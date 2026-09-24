/**
 * 用真 Chrome 走一遍 M1 的验收路径：
 * 注册 → 建档四步 → 头像 + 照片 → status 翻成 active（第 1 张照片落库时就翻）。
 *
 * 照片传 3 张不是为了过门槛（门槛是 1 张，脚本里也断言了这一点），
 * 而是为了让下面的拖拽排序有得拖 —— 排序至少要 2 张才测得出东西。
 *
 * 这不是单元测试，是拿来验「前端到底能不能把这条链路走通」的 ——
 * tsc 能查类型，查不出「后端返回的是 {photos:[]} 而不是数组」这类事。
 *
 * 前置：本地栈起着，且 vite dev server 在跑（默认 5173）。
 * 跑法：node web/scripts/e2e-onboarding.mjs
 *
 * 控制台错误、未捕获异常、失败的请求全部收集起来，最后一起报，
 * 有任何一条退出码就是 1。
 */
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import puppeteer from 'puppeteer-core'

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = resolve(HERE, '../..')

/** 三个环境相关的路径都允许覆盖，默认值是本机开发的那一套。 */
const BASE = process.env.E2E_BASE ?? 'http://localhost:5173'
const CHROME =
  process.env.E2E_CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const OUT = process.env.E2E_OUT ?? '/tmp/tidings-shots'

/** 头像用它：这是一张真人脸，能过后端的 pigo 检测。 */
const FACE =
  process.env.E2E_FACE ?? resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')

/** 相册用图：由头像那张 fixture 缩放而来。相册不做人脸检测，尺寸不同即可区分。 */
const PHOTOS = [1, 2, 3].map((i) => resolve(OUT, `photo${i}.jpg`))

mkdirSync(OUT, { recursive: true })

/**
 * 清掉注册限流计数。
 *
 * 不清的话这个脚本一天只能跑通头几次，之后卡在注册那一步报 429 ——
 * 而它每跑一次就注册一个新账号。e2e-settings.mjs 早就有这一段（还多了个
 * E2E_CLEAR_LIMIT 的口子），这边一直没有，于是「重跑一遍」变成了要先手工
 * 去 redis 里删 key。两份保持一致，别只改一处。
 */
function clearRegisterLimit() {
  const custom = process.env.E2E_CLEAR_LIMIT
  if (custom === 'skip') return
  if (custom) {
    execFileSync('bash', ['-c', custom], { stdio: 'inherit' })
    return
  }
  execFileSync('docker', [
    'compose',
    '-f',
    resolve(REPO, 'deploy/docker-compose.yml'),
    'exec',
    '-T',
    'redis',
    'redis-cli',
    'EVAL',
    "for _,k in ipairs(redis.call('keys',ARGV[1])) do redis.call('del',k) end return 1",
    '0',
    'auth:reg:ip:*',
  ])
}

/**
 * 直接对本地库跑一条 SQL。
 *
 * 用来造「入池门槛后来加过项」的老账号 —— 那条路径走接口造不出来（现在的
 * 必填校验不会再让人以缺项的状态进池），可它恰恰是加门槛时最容易出事的地方：
 * status 是 active、missing_required 却不为空。生产上加这次门槛时就有 121 个
 * 账号是这个状态，所以它得有回归网，不能只靠推理。
 */
function psql(sql) {
  return execFileSync(
    'docker',
    [
      'compose',
      '-f',
      resolve(REPO, 'deploy/docker-compose.yml'),
      'exec',
      '-T',
      'postgres',
      'psql',
      '-U',
      'tidings',
      '-d',
      'tidings',
      '-tAc',
      sql,
    ],
    { encoding: 'utf8' },
  ).trim()
}

// 相册图按需生成，不让脚本依赖任何手工准备的文件。
// 用 sips（macOS 自带）缩放同一张 fixture —— 跟默认的 Chrome 路径一样，
// 这个脚本本来就只在 macOS 上跑。
if (!existsSync(PHOTOS[0])) {
  const widths = [900, 780, 1020]
  PHOTOS.forEach((out, i) => {
    execFileSync('sips', ['-Z', String(widths[i]), FACE, '--out', out], { stdio: 'ignore' })
  })
}

const email = `e2e-${Date.now()}@example.com`
const password = 'tidings-e2e-2026'

const problems = []
const steps = []

clearRegisterLimit()

function log(ok, msg) {
  steps.push(`${ok ? '✓' : '✗'} ${msg}`)
  console.log(`${ok ? '✓' : '✗'} ${msg}`)
}

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: true,
  args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
  defaultViewport: { width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
})

try {
  const page = await browser.newPage()

  page.on('pageerror', (e) => problems.push(`[未捕获异常] ${e.message}`))
  page.on('console', (m) => {
    // 带上来源 URL：「Failed to load resource: 401」这种消息不带地址，
    // 报出来等于没报 —— 分不清是哪一个接口
    const from = m.location()?.url
    const where = from ? ` (${from})` : ''
    if (m.type() === 'error') problems.push(`[console.error] ${m.text()}${where}`)
  })
  page.on('requestfailed', (r) => {
    const t = r.failure()?.errorText ?? ''
    // 导航被后续跳转取消是正常的，不算问题
    if (t.includes('ERR_ABORTED')) return
    problems.push(`[请求失败] ${r.url()} ${t}`)
  })

  const shot = async (name) => {
    await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: true })
  }

  const waitText = (text, timeout = 20000) =>
    page.waitForFunction((t) => document.body.innerText.includes(t), { timeout }, text)

  // 下面等步骤切换时，等的都是该步独有的那句提示，不是「基本」「外形」这种标题词。
  // 标题词只有一两个字，别处很容易撞上，用它等会提前命中 —— 脚本于是停在上一步
  // 却以为已经走过去了，表现是一个没头没脑的 `Cannot read properties of null
  // (reading 'uploadFile')`。别把这些串换回步骤标题。

  const clickText = async (selector, text) => {
    const h = await page.evaluateHandle(
      (sel, t) => [...document.querySelectorAll(sel)].find((el) => el.textContent.trim() === t) ?? null,
      selector,
      text,
    )
    const el = h.asElement()
    if (!el) throw new Error(`找不到文本为「${text}」的 ${selector}`)

    // 先挪到视口中间再点。底部 tab 固定占着视口最下面 56px，
    // 而 puppeteer 默认只把元素滚到「刚好露出来」—— 那种位置下元素的
    // 中点正好落在 tab 栏底下，点下去点到的是 tab，页面会跳到别处。
    // 表现是「点了退出登录，结果回到了首页」，很容易误判成产品坏了。
    //
    // 只滚一次还不够。滚完到点下去之间页面可能还在长：/me 上的
    // 「当前环境下通知服务不可用」是异步渲染的，它插在退出按钮上方，
    // 一出现就把按钮往下顶 ~50px，点下去正好落在刚顶下来的那段文字上。
    // 同一处时好时坏，就是这么来的（页面高度两次跑下来差了 80px）。
    // 所以滚完等盒子稳定、并确认这个点上真的是它，不是就重来。
    for (let i = 0; i < 10; i++) {
      const before = await el.evaluate((e) => {
        e.scrollIntoView({ block: 'center' })
        const r = e.getBoundingClientRect()
        return { top: r.top, left: r.left }
      })
      await new Promise((r) => setTimeout(r, 120))
      const ok = await el.evaluate((e, prev) => {
        const r = e.getBoundingClientRect()
        // 还在动：等它停下来再说
        if (Math.abs(r.top - prev.top) > 0.5 || Math.abs(r.left - prev.left) > 0.5) return false
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
        return hit === e || e.contains(hit)
      }, before)
      if (ok) {
        await el.click()
        await h.dispose()
        return
      }
    }
    await h.dispose()
    throw new Error(`点不到「${text}」：位置一直在动，或者被别的元素盖着`)
  }

  // ---------- 底部弹层与滚轮 ----------
  //
  // 出生日期 / 所在城市 / 家乡是「一个字段 + 底部弹层 + 滚轮」。弹层里每一列
  // 是 role=listbox 的滚动容器，每个选项是 role=option 且带 data-value
  // （和原来 <option value> 同义，不是给测试专用的假属性）。page.select()
  // 对它们完全无效，下面这几个助手是替代品。

  /**
   * 打开弹层。
   *
   * 直接派发 click 而不是按鼠标坐标点：底部 tab 固定占着视口最下面 56px，
   * 按坐标点会点到 tab 上（clickText 那段注释里写过同一件事）。这一层被测的
   * 是弹层本身，点开它这一步不该引入坐标的偶然性。
   */
  async function openSheet(triggerId, title) {
    await page.$eval(`#${triggerId}`, (el) => {
      el.scrollIntoView({ block: 'center' })
      el.click()
    })
    await page.waitForFunction(
      (t) => !!document.querySelector(`[role=dialog][aria-label="${t}"]`),
      { timeout: 10000 },
      title,
    )
  }

  /** 弹层头部右边那颗「完成」。 */
  async function confirmSheet(title) {
    await page.evaluate((t) => {
      const box = document.querySelector(`[role=dialog][aria-label="${t}"]`)
      ;[...box.querySelectorAll('button')].find((b) => b.textContent.trim() === '完成').click()
    }, title)
    await page.waitForFunction(() => !document.querySelector('[role=dialog]'), { timeout: 5000 })
  }

  /** 弹层上方那行实时回显（「1995 年 8 月 20 日 · 31 岁」/「浙江 · 杭州」）。 */
  const sheetReadout = () => page.$eval('[role=dialog] p', (el) => el.textContent)

  /** 某一列的全部选项值。 */
  const wheelValues = (colId) =>
    page.$$eval(`#${colId} [role=option]`, (els) => els.map((e) => e.dataset.value))

  /** 某一列当前停在哪个值上。 */
  const wheelSelected = (colId) =>
    page.$eval(`#${colId} [role=option][aria-selected=true]`, (el) => el.dataset.value)

  /**
   * 把某一列滚到指定值。
   *
   * 滚轮是 scroll-snap 的，停在第 i 项 ⟺ scrollTop = i × 行高（首尾各留了
   * 两行 padding，所以第 0 项也能停在正中），程序化滚动因此是确定性的，
   * 不需要模拟手势 —— 何况 e2e 的 click 只发鼠标事件，惯性滚动模拟不出来。
   *
   * 行高从 DOM 读、不写死 40：写死了改一次 CSS 就得回来改这里。
   *
   * 等的是 aria-selected 挪过去，不是固定 sleep：组件要把滚动收敛成状态得
   * 过 120ms 防抖，写死的窗口在慢机器上会变成偶发失败。
   */
  async function setWheel(colId, value) {
    const want = String(value)
    await page.evaluate(
      (id, v) => {
        const col = document.querySelector(`#${id}`)
        const items = [...col.querySelectorAll('[role=option]')]
        const i = items.findIndex((el) => el.dataset.value === v)
        if (i < 0) throw new Error(`#${id} 里没有 ${v}：${items.map((e) => e.dataset.value)}`)
        col.scrollTop = i * items[0].offsetHeight
      },
      colId,
      want,
    )
    await page.waitForFunction(
      (id, v) =>
        document.querySelector(`#${id} [role=option][aria-selected=true]`)?.dataset.value === v,
      { timeout: 5000 },
      colId,
      want,
    )
  }

  /** 直接问后端要照片顺序，不信页面上的样子。 */
  const apiPhotoIds = () =>
    page.evaluate(async () => {
      const r = await fetch('/api/v1/me/photos', {
        headers: { Authorization: `Bearer ${localStorage.getItem('tidings.access')}` },
      })
      return ((await r.json()).data.photos ?? []).map((p) => p.id)
    })

  /**
   * 直接问后端要完整资料。状态与缺失项一律以服务端为准，不看页面上的字。
   *
   * 走 /me/profile 而不是 /me：后者是给前端 rootLoader 用的会话视图，
   * 只有 status / next_step / email，**没有 missing_required**。
   */
  const apiMe = () =>
    page.evaluate(async () => {
      const r = await fetch('/api/v1/me/profile', {
        headers: { Authorization: `Bearer ${localStorage.getItem('tidings.access')}` },
      })
      return (await r.json()).data
    })

  // ---------- 登录页 ----------
  await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' })
  await waitText('我们替你留意')
  log(true, '登录页渲染正常')
  await shot('01-login')

  await clickText('[role=tab]', '注册')
  await waitText('确认密码')

  await page.type('#email', email)
  await page.type('#password', password)
  await page.type('#confirm', password)
  await shot('02-register-filled')

  await page.click('button[type=submit]')
  await page.waitForFunction(() => location.pathname === '/onboarding', { timeout: 20000 })
  log(true, `注册成功，落到建档向导（${email}）`)

  // ---------- 第 1 步：基本 ----------
  await waitText('基本')
  await page.type('#nickname', '林小满')
  await clickText('[role=radio]', '女')
  // 出生日期是**一个**字段：点开是底部弹层，里面是年 / 月 / 日三列滚轮。
  // 从前是三个并排的原生 select —— 同一个问题摊成三个控件，得依次点开三次
  // 系统滚轮。三级联动本身没丢，只是搬进了弹层（1995 年 2 月 28 天、
  // 1996 年 2 月 29 天）。它不是把「1995 年 8 月 20 日」铺成一长条 ——
  // 那是 43 年 × 12 月 × 31 日。
  //
  // 年份由当前年份算，不写死：写死的脚本过一年就烂。
  const now = new Date()
  const maxYear = now.getFullYear() - 18
  const minYear = now.getFullYear() - 60
  // 空着打开时年列落在今年往前 28 年（BirthDatePicker 的 DEFAULT_AGE）
  const defaultYear = now.getFullYear() - 28
  const TITLE_BIRTH = '选择出生日期'

  await openSheet('birth_date', TITLE_BIRTH)

  const years = await wheelValues('birth_date_year')
  log(
    years.length === 43 && years[0] === String(maxYear) && years.at(-1) === String(minYear),
    `出生年：${years.length} 项，${years[0]} 倒序到 ${years.at(-1)}`,
  )

  // 旧版这里是「年没选：月与日都是 disabled，月的列表是空的」—— 那个状态随着
  // 原生三级联动一起没了，也正是用户嫌烦的东西。现在空字段打开时三列都有值，
  // 头部写着这一屏要提交的是什么。
  const birthOpened = await sheetReadout()
  log(
    (await wheelValues('birth_date_month')).length === 12 &&
      (await wheelValues('birth_date_day')).length === 31 &&
      birthOpened.startsWith(`${defaultYear} 年 1 月 1 日`),
    `空字段打开：三列都有值（12 月 / 31 日），头部显示「${birthOpened}」`,
  )

  // 滚轮是自绘的，一个滚动容器对键盘用户本来完全不可达 —— role=listbox +
  // 方向键是补回来的。它换掉的原生 select 本来是可达的，这条最容易悄悄退化。
  await page.$eval('#birth_date_year', (el) => el.focus())
  await page.keyboard.press('ArrowDown')
  await page.waitForFunction(
    (before) =>
      document.querySelector('#birth_date_year [role=option][aria-selected=true]')?.dataset
        .value !== before,
    { timeout: 5000 },
    String(defaultYear),
  )
  log(true, `方向键：年列 ${defaultYear} → ${await wheelSelected('birth_date_year')}`)

  // 18 岁那一年只列到当前月，否则会排出 17 岁的组合，要提交才知道不行
  await setWheel('birth_date_year', maxYear)
  log(
    (await wheelValues('birth_date_month')).length === now.getMonth() + 1,
    `刚满 18 岁那一年只列到 ${now.getMonth() + 1} 月`,
  )

  await setWheel('birth_date_year', 1995)
  log((await wheelValues('birth_date_month')).length === 12, '换成 1995 年：12 个月')

  // 闰年要在界面上看得见，这是三级联动与「一个长列表」的实际差别
  await setWheel('birth_date_month', 2)
  log((await wheelValues('birth_date_day')).length === 28, '1995 年 2 月：日 28 项')

  await setWheel('birth_date_year', 1996)
  log(
    (await wheelSelected('birth_date_month')) === '2' &&
      (await wheelValues('birth_date_day')).length === 29,
    '换到 1996 年 2 月：月份保留，日变成 29 项（闰年）',
  )

  await setWheel('birth_date_month', 4)
  log((await wheelValues('birth_date_day')).length === 30, '1996 年 4 月：日 30 项')

  await setWheel('birth_date_year', 1995)
  await setWheel('birth_date_month', 8)
  await setWheel('birth_date_day', 20)
  await confirmSheet(TITLE_BIRTH)
  const birthHint = await page.$eval('#birth_date-hint', (el) => el.textContent)
  log(birthHint === '1995 年 8 月 20 日，你现在 31 岁。', `出生日期提示：${birthHint}`)

  // 所在城市：同一个「一个字段 + 弹层」，弹层里是一屏两列，左省右市。
  const TITLE_CITY = '选择所在城市'

  await openSheet('city_code', TITLE_CITY)
  const cityOpened = await sheetReadout()
  log(
    (await wheelValues('city_code_city')).length === 1 && cityOpened === '北京 · 北京',
    `空字段打开：省落在第一项，头部显示「${cityOpened}」`,
  )

  // 点「完成」才存。滚了但不确认（Escape；「取消」和点遮罩走的是同一条路），
  // 字段必须原封不动 —— 这是「滚轮只改草稿」这个契约的机器证明。
  await setWheel('city_code_province', 320000)
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('[role=dialog]'), { timeout: 5000 })
  const afterEscape = await page.$eval('#city_code', (el) => el.textContent.trim())
  log(afterEscape === '请选择', `滚了但不点「完成」：Escape 丢弃草稿，字段仍是「${afterEscape}」`)

  await openSheet('city_code', TITLE_CITY)
  // 换到一个多市的省，市列整列重建并落回第一项。旧版这里是**清空**
  // （市下拉变回「市」）—— 滚轮里没有「空」这一格，落到第一项是它唯一说得通
  // 的对应，而且上面的回显会写出「浙江 · 杭州」，用户看得见自己选到哪儿了。
  await setWheel('city_code_province', 330000)
  const zjCities = await wheelValues('city_code_city')
  log(
    zjCities.length === 11 && (await sheetReadout()) === '浙江 · 杭州',
    `换到浙江：市列重建为 ${zjCities.length} 个市，落回「${await sheetReadout()}」`,
  )

  await setWheel('city_code_city', 330200)
  log((await sheetReadout()) === '浙江 · 宁波', `选中宁波：${await sheetReadout()}`)

  // 只有一个市的省（四个直辖市 + 港澳台）—— 对上海用户来说「市」这一级
  // 没有决策含量，滚轮不隐藏，但答案已经在那儿了
  await setWheel('city_code_province', 310000)
  log(
    (await wheelValues('city_code_city')).length === 1 &&
      (await sheetReadout()) === '上海 · 上海',
    '换到上海：市列只剩 1 项，就是上海本身',
  )

  await confirmSheet(TITLE_CITY)
  const cityText = await page.$eval('#city_code', (el) => el.textContent.trim())
  log(cityText === '上海', `点「完成」之后字段显示「${cityText}」`)

  await waitText('你现在 31 岁')
  log(true, '第一步填好，年龄回显正确')
  await shot('03-onboarding-basic')

  await clickText('button', '下一步')
  await waitText('让别人能判断要不要认识你')
  log(true, '第一步已保存并进入第二步')

  // ---------- 第 2 步：外形 ----------
  await page.select('#height_cm', '165')
  await page.select('#weight_kg', '52')
  await shot('04-onboarding-figure')

  await clickText('button', '下一步')
  await waitText('学历是硬条件过滤里最常用的两项之一')
  log(true, '第二步已保存并进入学历步骤')

  // ---------- 第 3 步：学历 ----------
  await clickText('[role=radio]', '硕士')

  // 毕业院校：输入的是查询词，只能从列表里选一个。这里刻意用别名「复旦」
  // 而不是全称 —— 人就是这么打字的，联想查不到别名时这个框等于没用。
  await page.type('#school_name', '复旦')
  await page.waitForFunction(
    () => document.querySelector('#school_name-list [role=option]')?.textContent?.includes('复旦'),
    { timeout: 15000 },
  )
  await page.click('#school_name-list [role=option]')
  const schoolPicked = await page.$eval('#school_name', (el) => el.value)
  log(schoolPicked === '复旦大学', `院校联想：输入「复旦」选中了 ${schoolPicked}`)

  await shot('04b-onboarding-education')
  await clickText('button', '下一步')
  await waitText('我们会自动检查')
  log(true, '第三步已保存并进入照片步骤')

  // ---------- 第 4 步：照片 ----------
  const avatarInput = await page.$('input[aria-label="选择头像"]')
  await avatarInput.uploadFile(FACE)
  await waitText('头像已就位', 30000)
  log(true, '头像通过正脸检测并已生效')

  // 「1 张就够」的机器证明，两条缺一不可：
  //
  // 只测第 1 张上传后翻 active 是不够的 —— 把整个照片检查删掉（0 张也放行）
  // 也能过。所以上传第 1 张**之前**先钉一次反面：0 张时还卡在 onboarding、
  // missing_required 里得有 photos。两条合起来才是「门槛恰好是 1」。
  const beforePhoto = await apiMe()
  log(
    beforePhoto.status === 'onboarding' && beforePhoto.missing_required.includes('photos'),
    `0 张照片时仍卡在建档（status=${beforePhoto.status}，缺 ${beforePhoto.missing_required.join('、')}）`,
  )

  for (const [i, p] of PHOTOS.entries()) {
    const input = await page.$('input[aria-label="添加照片"]')
    await input.uploadFile(p)
    await waitText(`已选 ${i + 1} 张`, 30000)

    if (i === 0) {
      // 必须在传第 2 张之前问：再往后问就分不清是第几张翻的牌了
      const afterFirst = await apiMe()
      log(
        afterFirst.status === 'active' && !afterFirst.missing_required.includes('photos'),
        `第 1 张落库就入池（status=${afterFirst.status}，缺 ${afterFirst.missing_required.join('、') || '无'}）`,
      )
    }
  }
  // 传 3 张不是为了过门槛，是为了让下面的拖拽排序有得拖
  log(true, '3 张照片全部上传完成')
  await shot('05-onboarding-photos')

  // ---------- 拖拽排序 ----------
  //
  // 把第一张拖到第三张的位置，然后问后端要顺序 —— 只看页面看不出来
  // 排序到底存没存进去（本地顺序在请求失败时也会先动）。
  const idsBefore = await apiPhotoIds()
  const cells = await page.$$('[aria-roledescription="sortable"]')
  if (cells.length !== 3) throw new Error(`可拖拽的格子有 ${cells.length} 个，应该是 3 个`)

  const from = await cells[0].boundingBox()
  const to = await cells[2].boundingBox()
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  // 先挪过 PointerSensor 的 8px 阈值，否则这一下只会被当成点击
  await page.mouse.move(from.x + from.width / 2 + 30, from.y + from.height / 2, { steps: 5 })
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 })
  await page.mouse.up()

  let idsAfter = idsBefore
  for (let i = 0; i < 40; i++) {
    idsAfter = await apiPhotoIds()
    if (String(idsAfter) !== String(idsBefore)) break
    await new Promise((r) => setTimeout(r, 250))
  }
  const expected = [idsBefore[1], idsBefore[2], idsBefore[0]]
  log(
    String(idsAfter) === String(expected),
    `拖拽后服务端顺序 ${JSON.stringify(idsBefore)} → ${JSON.stringify(idsAfter)}`,
  )

  // 封面标记要跟着走 —— 它代表 position = 0
  const coverOnFirst = await page.evaluate(() => {
    const cell = document.querySelector('[aria-roledescription="sortable"]')
    return cell?.textContent?.includes('封面') ?? false
  })
  log(coverOnFirst, '封面标记跟着排到了第一位')
  await shot('05b-photos-reordered')

  await clickText('button', '下一步')
  await waitText('也可以留到以后再说')
  log(true, '照片满足门槛，可以继续')

  // 进池状态在第 1 张落库时就已经翻了（上面已断言），这里确认它没被后面的操作带歪
  const status = await apiMe()
  log(status.status === 'active', `走完照片步后 /me 的 status = ${status.status}`)
  log(status.next_step === 'home', `/me 的 next_step = ${status.next_step}`)

  // ---------- 第 4 步：补充 ----------
  // 家乡和所在城市是同一个控件，只是 idBase 和标题不同
  const TITLE_HOMETOWN = '选择家乡'
  await openSheet('hometown_code', TITLE_HOMETOWN)
  await setWheel('hometown_code_province', 320000)
  await setWheel('hometown_code_city', 320100)
  await confirmSheet(TITLE_HOMETOWN)
  const hometown = await page.$eval('#hometown_code', (el) => el.textContent.trim())
  log(hometown === '南京', `家乡两级滚轮：江苏 → ${hometown}`)

  await page.type('#occupation', '产品经理')
  await page.type('#hobbies', '摄影、徒步、做饭')
  await clickText('[role=radio]', '50–100 万')
  await clickText('[role=radio]', '早睡早起')
  await shot('06-onboarding-more')

  await clickText('button', '完成，去首页')
  await page.waitForFunction(() => location.pathname === '/', { timeout: 20000 })
  await waitText('暂时没有新的引荐')
  log(true, '向导完成，落到首页空状态')
  await shot('07-home')

  // ---------- 回填检查（19.6 第一个洞） ----------
  await page.goto(`${BASE}/me/edit`, { waitUntil: 'domcontentloaded' })
  await waitText('编辑资料')
  // 三个复合控件现在各是一个字段，回填就看触发器上那行字。
  //
  // **不为了测试往触发器上挂 data-value** —— 用户看得见的那行文本就是断言；
  // 码值由下面那段 /api/v1/me/profile 的服务端真值兜着。复合控件最容易只
  // 回填一半（年月回来了、日没回来），而半截的回填在这行字上看得一清二楚。
  const refilled = await page.evaluate(() => {
    const t = (id) => document.querySelector(id)?.textContent?.trim()
    const v = (id) => document.querySelector(id)?.value
    return {
      nickname: v('#nickname'),
      birth: t('#birth_date'),
      height: v('#height_cm'),
      // 体重也是原生下拉，回显是它唯一可能出错的地方（选项表与后端范围对不上
      // 就会静默落到第一项），所以和身高一样按 .value 读
      weight: v('#weight_kg'),
      city: t('#city_code'),
      hometown: t('#hometown_code'),
      occupation: v('#occupation'),
      school: v('#school_name'),
      gender: [...document.querySelectorAll('[role=radio][aria-checked=true]')].map((e) => e.textContent),
    }
  })
  log(
    refilled.nickname === '林小满' &&
      refilled.birth === '1995 年 8 月 20 日' &&
      refilled.height === '165' &&
      refilled.weight === '52' &&
      refilled.city === '上海' &&
      refilled.hometown === '南京' &&
      refilled.occupation === '产品经理' &&
      refilled.school === '复旦大学' &&
      refilled.gender.includes('女'),
    `编辑页回填正确：${JSON.stringify(refilled)}`,
  )

  // 日的真值在后端。库里存的是 birth_ym(YYYYMM) + birth_day，界面回填对了
  // 不等于存对了 —— 这两个字段是分开提交的，漏一个只会表现为「界面上有、
  // 刷新后没了」。
  const savedBirth = await page.evaluate(async () => {
    const r = await fetch('/api/v1/me/profile', {
      headers: { Authorization: `Bearer ${localStorage.getItem('tidings.access')}` },
    })
    const d = (await r.json()).data
    return { ym: d.birth_ym, day: d.birth_day, city: d.city_code, hometown: d.hometown_code }
  })
  log(
    savedBirth.ym === 199508 &&
      savedBirth.day === 20 &&
      savedBirth.city === 310000 &&
      savedBirth.hometown === 320100,
    `服务端存下了：birth_ym=${savedBirth.ym} birth_day=${savedBirth.day} ` +
      `city=${savedBirth.city} hometown=${savedBirth.hometown}`,
  )
  await shot('08-me-edit')

  await page.goto(`${BASE}/me`, { waitUntil: 'domcontentloaded' })
  // 完成度不再呈现给用户了，这一页现在只说进没进池子
  await waitText('入池条件已满足')
  const inPool = await page.evaluate(() =>
    document.body.innerText.match(/入池条件已满足[\s\S]{0,24}/)?.[0],
  )
  log(true, `我的页：${inPool?.replace(/\n/g, ' ')}`)
  await shot('09-me')

  // ---------- 门槛加过项之后的老账号 ----------
  //
  // 加必填项只拦新档案：服务端只在 status = onboarding 时翻牌（profile.go 的
  // applyProfileState），已经入池的人不会被踢回来 —— 那会连同已有的引荐和
  // 会话一起失效。于是「status 是 active、missing_required 却不为空」这个
  // 组合真实存在，而这一页必须仍然说他已入池。
  //
  // 拿 missing 当入池判据就错了：那批人什么都没做，却会被告知自己不在池子里。
  // 直接改库造出这个状态 —— 走接口造不出来，因为现在的校验不会让人以缺项的
  // 状态进池，而正是「造不出来的状态」最容易在改动里被漏掉。
  const uid = `(select id from users where email = '${email}')`
  psql(`update profiles set school_name = '' where user_id = ${uid}`)
  await page.goto(`${BASE}/me`, { waitUntil: 'domcontentloaded' })
  await waitText('去补全')
  const stillIn = await page.evaluate(() =>
    document.body.innerText.match(/入池条件已满足[\s\S]{0,16}/)?.[0],
  )
  log(
    Boolean(stillIn),
    `缺了新门槛项的老账号仍显示已入池，同时给出补全入口：${stillIn?.replace(/\n/g, ' ')}`,
  )
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  await waitText('去补全')
  log(true, '首页空状态同样给出补全入口')
  psql(`update profiles set school_name = '复旦大学' where user_id = ${uid}`)

  // ---------- 路由守卫 ----------
  await page.goto(`${BASE}/onboarding`, { waitUntil: 'domcontentloaded' })
  await page.waitForFunction(() => location.pathname === '/', { timeout: 15000 })
  log(true, '已建完档的人访问 /onboarding 会被弹回首页')

  // ---------- 退出 ----------
  await page.goto(`${BASE}/me`, { waitUntil: 'domcontentloaded' })
  await waitText('退出登录')
  await clickText('button', '退出登录')
  await page.waitForFunction(() => location.pathname === '/login', { timeout: 15000 })
  log(true, '退出登录回到登录页')


  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  await page.waitForFunction(() => location.pathname === '/login', { timeout: 15000 })
  log(true, '未登录访问首页会被拦到登录页')

  await page.goto(`${BASE}/nope`, { waitUntil: 'domcontentloaded' })
  await waitText('这一页不存在')
  await shot('10-404')
  log(true, '404 页面正常')

} catch (err) {
  problems.push(`[脚本中断] ${err.message}`)
  log(false, `中断：${err.message}`)
  try {
    const pages = await browser.pages()
    await pages.at(-1).screenshot({ path: `${OUT}/99-failure.png`, fullPage: true })
  } catch {}
} finally {
  await browser.close()
}

console.log('\n===== 控制台 / 网络问题 =====')
if (problems.length === 0) console.log('（无）')
else problems.forEach((p) => console.log(p))
console.log(`\n通过 ${steps.filter((s) => s.startsWith('✓')).length} / ${steps.length}`)
process.exit(problems.length === 0 && steps.every((s) => s.startsWith('✓')) ? 0 : 1)

/**
 * 用真 Chrome 走一遍 M1 的验收路径：
 * 注册 → 建档四步 → 头像 + 3 张照片 → status 翻成 active。
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

  // 下面等步骤切换时，等的都是该步独有的那句提示，不是「外形」「照片」这种标题词。
  // 标题词在向导顶部的「还差：…」那一行里也会出现（还差：昵称、性别、…、照片（至少 3 张）），
  // 用它等会在第一步就立刻命中，脚本于是停在第一步却以为已经到了照片步 ——
  // 表现是一个没头没脑的 `Cannot read properties of null (reading 'uploadFile')`。
  // 别把这些串换回步骤标题。

  const clickText = async (selector, text) => {
    const h = await page.evaluateHandle(
      (sel, t) => [...document.querySelectorAll(sel)].find((el) => el.textContent.trim() === t) ?? null,
      selector,
      text,
    )
    const el = h.asElement()
    if (!el) throw new Error(`找不到文本为「${text}」的 ${selector}`)
    await el.click()
    await h.dispose()
  }

  /** 直接问后端要照片顺序，不信页面上的样子。 */
  const apiPhotoIds = () =>
    page.evaluate(async () => {
      const r = await fetch('/api/v1/me/photos', {
        headers: { Authorization: `Bearer ${localStorage.getItem('tidings.access')}` },
      })
      return ((await r.json()).data.photos ?? []).map((p) => p.id)
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
  await page.select('#birth_year', '1995')
  await page.select('#birth_month', '8')
  await page.select('#city_code', '310000')
  await waitText('你现在 31 岁')
  log(true, '第一步填好，年龄回显正确')
  await shot('03-onboarding-basic')

  await clickText('button', '下一步')
  await waitText('让别人能判断要不要认识你')
  log(true, '第一步已保存并进入第二步')

  // ---------- 第 2 步：外形 ----------
  await page.select('#height_cm', '165')
  await clickText('[role=radio]', '硕士')
  await shot('04-onboarding-figure')
  await clickText('button', '下一步')
  await waitText('我们会自动检查')
  log(true, '第二步已保存并进入照片步骤')

  // ---------- 第 3 步：照片 ----------
  const avatarInput = await page.$('input[aria-label="选择头像"]')
  await avatarInput.uploadFile(FACE)
  await waitText('头像已就位', 30000)
  log(true, '头像通过正脸检测并已生效')

  for (const [i, p] of PHOTOS.entries()) {
    const input = await page.$('input[aria-label="添加照片"]')
    await input.uploadFile(p)
    await waitText(`已选 ${i + 1} 张`, 30000)
  }
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

  // 进池状态应该在照片够数的那一刻就已经翻了
  const status = await page.evaluate(async () => {
    const r = await fetch('/api/v1/me', {
      headers: { Authorization: `Bearer ${localStorage.getItem('tidings.access')}` },
    })
    return (await r.json()).data
  })
  log(status.status === 'active', `照片够数后 /me 的 status = ${status.status}`)
  log(status.next_step === 'home', `/me 的 next_step = ${status.next_step}`)

  // ---------- 第 4 步：补充 ----------
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
  const refilled = await page.evaluate(() => ({
    nickname: document.querySelector('#nickname')?.value,
    year: document.querySelector('#birth_year')?.value,
    height: document.querySelector('#height_cm')?.value,
    city: document.querySelector('#city_code')?.value,
    occupation: document.querySelector('#occupation')?.value,
    gender: [...document.querySelectorAll('[role=radio][aria-checked=true]')].map((e) => e.textContent),
  }))
  log(
    refilled.nickname === '林小满' &&
      refilled.year === '1995' &&
      refilled.height === '165' &&
      refilled.city === '310000' &&
      refilled.occupation === '产品经理' &&
      refilled.gender.includes('女'),
    `编辑页回填正确：${JSON.stringify(refilled)}`,
  )
  await shot('08-me-edit')

  await page.goto(`${BASE}/me`, { waitUntil: 'domcontentloaded' })
  await waitText('档案完成度')
  const completeness = await page.evaluate(() => document.body.innerText.match(/档案完成度[\s\S]{0,12}/)?.[0])
  log(true, `我的页：${completeness?.replace(/\n/g, ' ')}`)
  await shot('09-me')

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

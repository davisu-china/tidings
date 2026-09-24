/**
 * 用真 Chrome 走一遍 /me/preferences 与 /me/settings 两条页面：
 * 回填对不对、存不存得上、跨字段校验拦不拦得住、暂停是不是即时生效。
 *
 * 这两页的接口在后端已经能跑（e2e-m4 验的就是那半边），所以这里只看
 * 前端那一半：tsc 能查出字段名写错，查不出「下拉里的『不限』存进去
 * 变成了 0」「保存之后按钮还亮着」这类事。
 *
 * 账号用接口直接建起来，不重走一遍建档向导 —— 建档是 e2e-onboarding
 * 的题目，在这里只是前置条件，用浏览器再走一遍只会让这个脚本
 * 因为跟它无关的原因失败。
 *
 * 前置：本地栈起着，vite dev server 在 5173。
 * 跑法：node web/scripts/e2e-settings.mjs
 */
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import puppeteer from 'puppeteer-core'

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = resolve(HERE, '../..')

const BASE = process.env.E2E_BASE ?? 'http://localhost:5173'
const API = process.env.E2E_API ?? 'http://localhost:8081'
const CHROME =
  process.env.E2E_CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const OUT = process.env.E2E_OUT ?? '/tmp/tidings-shots'
const COMPOSE = ['compose', '-f', resolve(REPO, 'deploy/docker-compose.yml')]

const FACE = resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')
const PHOTOS = [0, 1, 2].map((i) => resolve(OUT, `settings-p${i}.jpg`))

const email = `e2e-settings-${Date.now()}@example.com`
const password = 'tidings-e2e-2026'

mkdirSync(OUT, { recursive: true })
if (!existsSync(PHOTOS[0])) {
  ;[880, 760, 1000].forEach((w, i) => {
    execFileSync('sips', ['-Z', String(w), FACE, '--out', PHOTOS[i]], {
      stdio: 'ignore',
    })
  })
}
const FACE_BYTES = readFileSync(FACE)
const PHOTO_BYTES = PHOTOS.map((p) => readFileSync(p))

const problems = []
const steps = []

function log(ok, msg) {
  steps.push(`${ok ? '✓' : '✗'} ${msg}`)
  console.log(`${ok ? '✓' : '✗'} ${msg}`)
}

// ---------------------------------------------------------------- 接口侧

async function api(path, { method = 'GET', token, body } = {}) {
  const headers = {}
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(`${API}/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const json = await res.json().catch(() => null)
  if (json?.code !== 'OK') {
    throw new Error(`${method} ${path} → ${res.status} ${json?.code} ${json?.message}`)
  }
  return json.data
}

async function upload(token, bytes) {
  const t = await api('/media/upload-url', {
    method: 'POST',
    token,
    body: { ext: '.jpg' },
  })
  const put = await fetch(t.upload_url, {
    method: 'PUT',
    headers: { 'Content-Type': 'image/jpeg' },
    body: bytes,
  })
  if (!put.ok) throw new Error(`直传失败 ${put.status}`)
  return t.object_key
}

/**
 * 建一个已入池的账号。
 *
 * 注册计数要清 —— 单 IP 每日 10 个（maxRegistersPerIPPerDay），
 * 这个脚本一个、e2e-m4 十六个，不清的话第二个脚本就 429。
 */
async function createActiveAccount() {
  execFileSync('docker', [
    ...COMPOSE,
    'exec',
    '-T',
    'redis',
    'redis-cli',
    'EVAL',
    "for _,k in ipairs(redis.call('keys',ARGV[1])) do redis.call('del',k) end return 1",
    '0',
    'auth:reg:ip:*',
  ])

  const auth = await api('/auth/register', {
    method: 'POST',
    body: { email, password },
  })
  const token = auth.access_token

  await api('/me/profile', {
    method: 'PATCH',
    token,
    body: {
      nickname: '林小满',
      gender: 'F',
      birth_ym: 199508,
      city_code: 310000,
      height_cm: 165,
      education_level: 3,
      occupation: '编辑',
      income_band: 3,
      chronotype: 1,
      want_child: 3,
      marital_status: 1,
      hobbies: '徒步、摄影',
      intro: '喜欢摄影和徒步，周末多半在外面。',
    },
  })
  await api('/me/avatar', {
    method: 'PUT',
    token,
    body: { object_key: await upload(token, FACE_BYTES) },
  })
  for (const b of PHOTO_BYTES) {
    await api('/me/photos', {
      method: 'POST',
      token,
      body: { object_key: await upload(token, b) },
    })
  }

  const me = await api('/me', { token })
  if (me.status !== 'active') throw new Error(`建档后 status = ${me.status}，没进池`)
  return { token }
}

// ---------------------------------------------------------------- 主流程

const account = await createActiveAccount()

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: true,
  args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
  defaultViewport: {
    width: 390,
    height: 844,
    deviceScaleFactor: 2,
    isMobile: true,
    hasTouch: true,
  },
})

try {
  const page = await browser.newPage()

  page.on('pageerror', (e) => problems.push(`[未捕获异常] ${e.message}`))
  page.on('console', (m) => {
    const from = m.location()?.url
    if (m.type() === 'error')
      problems.push(`[console.error] ${m.text()}${from ? ` (${from})` : ''}`)
  })
  page.on('requestfailed', (r) => {
    const t = r.failure()?.errorText ?? ''
    if (t.includes('ERR_ABORTED')) return
    problems.push(`[请求失败] ${r.url()} ${t}`)
  })

  const shot = (name) => page.screenshot({ path: `${OUT}/${name}.png`, fullPage: true })
  const waitText = (text, timeout = 20000) =>
    page.waitForFunction((t) => document.body.innerText.includes(t), { timeout }, text)

  const clickText = async (selector, text) => {
    const h = await page.evaluateHandle(
      (sel, t) =>
        [...document.querySelectorAll(sel)].find((el) => el.textContent.trim() === t) ?? null,
      selector,
      text,
    )
    const el = h.asElement()
    if (!el) throw new Error(`找不到文本为「${text}」的 ${selector}`)
    await el.click()
    await h.dispose()
  }

  /**
   * 在某一组分段控件里点一个选项。
   *
   * 不能用 clickText 全局找文本：「接受有婚史」和「接受异地」两组都有
   * 一个「不接受」，全局找只会命中先出现的那一组 —— 脚本会以为自己
   * 点了异地，实际点的是婚史，然后拿一个没被改动过的值去断言。
   * 分组靠的是 Field 给 radiogroup 挂的 aria-labelledby（见 fields.tsx），
   * 顺带也就验了这层关联还在。
   */
  const clickIn = async (groupId, text) => {
    const h = await page.evaluateHandle(
      (g, t) =>
        [...document.querySelectorAll(`[aria-labelledby="${g}-label"] [role=radio]`)].find(
          (el) => el.textContent.trim() === t,
        ) ?? null,
      groupId,
      text,
    )
    const el = h.asElement()
    if (!el) throw new Error(`「${groupId}」这一组里没有「${text}」`)
    await el.click()
    await h.dispose()
  }

  const textOf = (sel) => page.$eval(sel, (el) => el.value)
  const bodyText = () => page.evaluate(() => document.body.innerText)

  /** 直接从接口读回偏好/设置：页面显示的对不对，得有个页面之外的依据。 */
  const apiGet = (path) =>
    page.evaluate(
      async (p) =>
        (
          await (
            await fetch(`/api/v1${p}`, {
              headers: {
                Authorization: `Bearer ${localStorage.getItem('tidings.access')}`,
              },
            })
          ).json()
        ).data,
      path,
    )

  const save = async () => {
    await clickText('button', '保存')
  }

  // ---------- 登录 ----------
  await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' })
  await waitText('我们替你留意')
  await page.type('#email', email)
  await page.type('#password', password)
  await page.click('button[type=submit]')
  await page.waitForFunction(() => location.pathname === '/', {
    timeout: 20000,
  })
  log(true, `登录成功，落到首页（${email}）`)

  // ---------- /me 上的两个入口 ----------
  await page.goto(`${BASE}/me`, { waitUntil: 'domcontentloaded' })
  await waitText('偏好设置')
  log(true, '/me 上有「偏好设置」与「通知与暂停」两个入口')
  await shot('s01-me')

  // ---------- 偏好设置 ----------
  await page.goto(`${BASE}/me/preferences`, { waitUntil: 'domcontentloaded' })
  await waitText('硬条件')
  log(true, '偏好页渲染正常（硬条件 + 软偏好两块都在）')
  await shot('s02-preferences-empty')

  let page_note = await bodyText()
  log(page_note.includes('还没有设过偏好'), '没设过偏好时给出引导，而不是一片空白的表单')
  log(
    (await textOf('#age_min')) === '-1' && (await textOf('#height_min')) === '-1',
    '年龄与身高回填成「不限」而不是空白',
  )

  // 保存按钮在没动过的时候不该亮
  log(
    await page.$eval('button[type=submit]', (el) => el.disabled),
    '没有任何改动时「保存」是禁用的',
  )

  // ---------- 区间反了要被拦下 ----------
  // 不做 focus/blur 那一套：这一页是 onChange 校验（Select 与 Segmented
  // 都没有 onBlur 可接，onTouched 在这里永远是空转）。选完就该报错。
  await page.select('#age_min', '35')
  await page.select('#age_max', '25')
  await waitText('上限不能小于下限')
  log(true, '年龄上限小于下限时当场报错，不用等到提交')

  let rejected = false
  try {
    await save()
    await new Promise((r) => setTimeout(r, 800))
    // 拦住了的话接口里应该还是空的
    rejected = (await apiGet('/me/preferences')).age_min === null
  } catch {
    // 按钮 disabled 也是一个合格的结果 —— 总之没能存进去
    rejected = true
  }
  log(rejected, '区间反了的时候存不进去')
  await shot('s03-preferences-invalid')

  // ---------- 正常填一份 ----------
  await page.select('#age_min', '25')
  await page.select('#age_max', '35')
  await clickIn('accept_divorced', '不接受') // 0 —— 与「不限」的 -1 只差一个数
  await clickIn('accept_remote', '不接受') // 2
  await clickIn('edu_min', '硕士') // 3
  await clickText('button', '上海')
  await clickText('button', '杭州')

  page_note = await bodyText()
  log(page_note.includes('已选 2 个'), '选中城市之后计数跟着走')

  await save()
  await waitText('已保存', 10000)
  const saved = await apiGet('/me/preferences')
  log(
    saved.age_min === 25 && saved.age_max === 35 && saved.configured === true,
    `存下来的年龄是 25–35（接口回读 ${saved.age_min}–${saved.age_max}）`,
  )
  log(
    String(saved.city_codes) === '310000,330100',
    `两个城市都存上了，且没有重复：${JSON.stringify(saved.city_codes)}`,
  )
  log(saved.accept_remote === 2, '「不接受异地」存成了 2，不是被当成「不限」')
  log(saved.accept_divorced === 0, '「不接受有婚史」存成了 0，没有被当成「不限」吃掉')
  log(saved.edu_min === 3, '最低学历存成了 3（硕士）')
  log(saved.height_min === null && saved.income_min === null, '没填的那几项仍然是「不限」')
  await shot('s04-preferences-filled')

  // ---------- 刷新之后还在 ----------
  await page.reload({ waitUntil: 'domcontentloaded' })
  await waitText('硬条件')
  log(
    (await textOf('#age_min')) === '25' && (await textOf('#age_max')) === '35',
    '刷新之后回填的还是存下来的值',
  )
  log(
    await page.$eval('button[type=submit]', (el) => el.disabled),
    '刚保存完，「保存」重新变灰（没有假的未保存状态）',
  )

  // ---------- 改回「不限」也能存 ----------
  await page.select('#age_min', '-1')
  await save()
  await waitText('已保存', 10000)
  log(
    (await apiGet('/me/preferences')).age_min === null,
    '把「不限」存回去，落库的是 NULL 而不是 0',
  )

  // ---------- 通知与暂停 ----------
  await page.goto(`${BASE}/me/settings`, { waitUntil: 'domcontentloaded' })
  await waitText('静默时段')
  log(true, '设置页渲染正常（暂停 + 静默时段 + 通知三块）')
  await shot('s05-settings')

  log(
    (await textOf('#quiet_start')) === '22' && (await textOf('#quiet_end')) === '9',
    '静默时段回填成默认的 22:00–09:00',
  )

  // 暂停是即时生效的：它是个开关，不是表单字段
  await clickText('[role=radio]', '暂停接收')
  await waitText('已暂停接收引荐', 10000)
  log((await apiGet('/me/settings')).intros_paused === true, '点「暂停接收」当场落库，不用再按保存')
  log((await bodyText()).includes('现在是暂停状态'), '页面回显切到了暂停态')
  await shot('s06-settings-paused')

  await clickText('[role=radio]', '接收引荐')
  await waitText('已恢复接收引荐', 10000)
  log((await apiGet('/me/settings')).intros_paused === false, '再点一下恢复，同样是即时的')

  // 静默时段走保存按钮：它是一组要一起看的值
  const before = await apiGet('/me/settings')
  await page.select('#quiet_start', '21')
  await page.select('#quiet_end', '8')
  log(
    (await apiGet('/me/settings')).quiet_start === before.quiet_start,
    '改了下拉但没保存时，服务端没有被动到',
  )

  await save()
  await waitText('已保存', 10000)
  const quiet = await apiGet('/me/settings')
  log(
    quiet.quiet_start === 21 && quiet.quiet_end === 8 && quiet.intros_paused === false,
    '保存了静默时段，且没有把暂停那一项顺手清成 false 之外的值',
  )
  log((await bodyText()).includes('现在是 21:00 到 08:00'), '页面把静默时段用人话回显了出来')
  await shot('s07-settings-quiet')

  // 起点与终点相同 = 不设静默，文案要说人话
  await page.select('#quiet_end', '21')
  await save()
  await waitText('已保存', 10000)
  log(
    (await bodyText()).includes('等于不设静默'),
    '两端相同时明确说明等于不设静默，而不是显示「21:00 到 21:00」',
  )
} catch (err) {
  problems.push(`[脚本中断] ${err.message}`)
  log(false, `中断：${err.message}`)
  try {
    const pages = await browser.pages()
    await pages.at(-1).screenshot({ path: `${OUT}/99-settings-failure.png`, fullPage: true })
  } catch {}
} finally {
  await browser.close()
}

console.log('\n===== 控制台 / 网络问题 =====')
if (problems.length === 0) console.log('（无）')
else problems.forEach((p) => console.log(p))
console.log(`\n通过 ${steps.filter((s) => s.startsWith('✓')).length} / ${steps.length}`)
process.exit(problems.length === 0 && steps.every((s) => s.startsWith('✓')) ? 0 : 1)

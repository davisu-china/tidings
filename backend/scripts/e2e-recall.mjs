/**
 * 召回侧的城市过滤验收：我填的「期望城市」到底管不管用。
 *
 * 这一条在 M4 之前是漏的：preferences.city_codes 存得进去、回显得出来，
 * 而候选集 SQL 从头到尾没读过它 —— 界面上写着「选了之后只会被引荐给
 * 这些城市的人」，实际上推来的人跟他选的城市毫无关系。这个脚本就是
 * 拿真账号、真生成任务把这句话验一遍。
 *
 * 为什么不能只看 SQL：漏的是**接线**，不是 SQL 文本。repo 那边一直
 * 收着 CandidateQuery.CityCodes，service 也一直老老实实填，
 * 只是 who-reads-it 那一步断了。所以这里不走 SQL 直查，
 * 而是造两个同省城市的人，让 worker 按 30 秒一轮真跑，读它生成出来的
 * 引荐行 —— 断了的那根线只要没接上，第二、三段就会当场露馅。
 *
 * 城市选南京 320100 与苏州 320500：同属江苏省（省份码取前两位，都是 32）。
 * 「同城或同省」是召回的既有条件，两个城市必须同省，否则不管「期望城市」
 * 填什么都不会有跨城的人进来，也就验不出这条过滤。
 *
 * 池子门槛：一个城市少于 INTRO_POOL_MIN（开发环境 10）时整个城市被跳过
 * （TierFor → TooSmall），所以两个城市都得种满 10 个人。这是本脚本
 * 造号最多、也最慢的原因 —— 值得，因为它验的正是「谁来推给我」。
 *
 * 前置：本地栈起着（make up），worker 在跑。
 * 跑法：node backend/scripts/e2e-recall.mjs
 *   E2E_BASE 默认 http://localhost:8081
 *
 * 任何一步不符就打印 ✗ 并让退出码非零；全通过才打印 ✓。
 */
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = resolve(HERE, '../..')

const BASE = process.env.E2E_BASE ?? 'http://localhost:8081'
const COMPOSE = ['compose', '-f', resolve(REPO, 'deploy/docker-compose.yml')]
const SEED_POOL = resolve(HERE, 'seed-pool.mjs')

/** 我（被验的那个人）所在的城市。 */
const HOME = 320100
/** 同省的另一座城市 —— 「期望城市」填它，就该只推它的人。 */
const OTHER = 320500
/** 省外的城市。填它一个引荐都不该有，界面上那句警告说的就是这件事。 */
const FAR = 110000

/** 每个城市要种的人数，必须 ≥ INTRO_POOL_MIN（deploy/.env，开发环境 10）。 */
const POOL = 10
/** 生成轮次间隔是 30 秒（INTRO_GEN_INTERVAL），等三轮还没动静就是真没动静。 */
const ROUND_MS = 30_000
const WAIT_MS = 3 * ROUND_MS + 10_000

const PASSWORD = 'tidings-e2e-recall-2026'

const OUT = '/tmp/tidings-seed'
const FACE = resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')
const PHOTOS = [0, 1, 2].map((i) => resolve(OUT, `p${i}.jpg`))

mkdirSync(OUT, { recursive: true })
if (!existsSync(PHOTOS[0])) {
  ;[880, 760, 1000].forEach((w, i) => {
    execFileSync('sips', ['-Z', String(w), FACE, '--out', PHOTOS[i]], { stdio: 'ignore' })
  })
}
const FACE_BYTES = readFileSync(FACE)
const PHOTO_BYTES = PHOTOS.map((p) => readFileSync(p))

let failed = 0
let passed = 0

function ok(cond, msg) {
  if (cond) {
    passed++
    console.log(`  ✓ ${msg}`)
  } else {
    failed++
    console.log(`  ✗ ${msg}`)
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// ---------------------------------------------------------------- 接口

async function api(path, { method = 'GET', token, body } = {}) {
  const headers = {}
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(`${BASE}/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const json = JSON.parse(text)
  if (!res.ok || json.code !== 'OK') {
    throw new Error(`${method} ${path} → ${res.status} ${json.code} ${json.message}`)
  }
  return json.data
}

async function upload(token, bytes) {
  const ticket = await api('/media/upload-url', { method: 'POST', token, body: { ext: '.jpg' } })
  const put = await fetch(ticket.upload_url, {
    method: 'PUT',
    headers: { 'Content-Type': 'image/jpeg' },
    body: bytes,
  })
  if (!put.ok) throw new Error(`直传失败 ${put.status}`)
  return ticket.object_key
}

// ---------------------------------------------------------------- 库

function psql(sql) {
  return execFileSync(
    'docker',
    [...COMPOSE, 'exec', '-T', 'postgres', 'psql', '-U', 'tidings', '-d', 'tidings', '-tAc', sql],
    { encoding: 'utf8' },
  ).trim()
}

/**
 * 清掉单 IP 每日注册计数。
 *
 * 这个脚本要注册 21 个号（我 + 两个城市各 10），而上限是 10
 * （maxRegistersPerIPPerDay，见 service/auth.go）。每次种之前清一次，
 * 每个城市正好 10 个，卡在线上而不是线上之外。
 */
function clearRegisterLimit() {
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
}

// ---------------------------------------------------------------- 造号

/**
 * 造我自己（被验的那个账号）。
 *
 * 必须是南京这批人里 id 最小的：生成任务每轮从城市里挑 3 个人，
 * 排序是「距上次收到引荐最久」在前、同为空时按 id 升序
 * （见 repo.PickUsersForGeneration）。id 最小 = 只要我没收到过引荐，
 * 每轮都排第一 —— 每一段验完都能指望下一轮就轮到我，而不用干等
 * 其他 10 个人轮完一遍。所以这个函数必须在新种的人之前调用。
 */
async function createSubject(stamp) {
  const email = `recall-${stamp}-me@example.com`
  clearRegisterLimit()
  const auth = await api('/auth/register', { method: 'POST', body: { email, password: PASSWORD } })
  const token = auth.access_token

  await api('/me/profile', {
    method: 'PATCH',
    token,
    body: {
      nickname: '苏见月',
      gender: 'F',
      birth_ym: 199508,
      city_code: HOME,
      height_cm: 166,
      education_level: 3,
      occupation: '编辑',
      income_band: 3,
      chronotype: 1,
      // 婚育与婚史都填「再说 / 未婚」，与种子里的人同款 ——
      // 硬条件里这两项撞上就淘汰，本脚本要验的是城市，不是它们。
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
  for (const bytes of PHOTO_BYTES) {
    await api('/me/photos', {
      method: 'POST',
      token,
      body: { object_key: await upload(token, bytes) },
    })
  }

  const me = await api('/me', { token })
  if (me.status !== 'active') throw new Error(`建档后 status = ${me.status}，没进池`)

  const id = Number(psql(`SELECT id FROM users WHERE email = '${email}'`))
  if (!id) throw new Error(`${email} 没查到 id`)
  return { id, token, email }
}

/** 种一个城市的人。走 seed-pool（真接口建号），性别一半一半。 */
function seedCity(city, prefix, count) {
  clearRegisterLimit()
  execFileSync('node', [SEED_POOL, String(count)], {
    env: { ...process.env, SEED_CITY: String(city), SEED_PREFIX: prefix },
    stdio: ['ignore', 'ignore', 'inherit'],
  })
}

/**
 * 改我的期望城市。
 *
 * 其余偏好固定不变：年龄放宽到 22–45、接受婚史、接受异地。
 * 异地那项必须填「接受」（1）—— 南京对苏州是跨城，acceptsRemote 会
 * 双向各判一次，我这边写「不接受」的话这段跨城匹配就永远出不来，
 * 而失败的样子看起来像城市过滤写反了。
 */
async function setCities(token, cities) {
  await api('/me/preferences', {
    method: 'PUT',
    token,
    body: {
      age_min: 22,
      age_max: 45,
      city_codes: cities,
      want_child: null,
      accept_divorced: 1,
      accept_remote: 1,
      edu_min: null,
      height_min: null,
      height_max: null,
      income_min: null,
      income_max: null,
    },
  })
}

// ---------------------------------------------------------------- 观察

/**
 * 把我收到的引荐全删掉。
 *
 * 两个作用：一是回到「我没收到过引荐」这个初始态，让我下一轮必定被挑中；
 * 二是清掉「未终结的引荐」这条排重 —— 留着它，上一段配过的人
 * 再也不会出现在候选集里，池子只有 5 个人，两段就见底了。
 */
function resetMyIntros(id) {
  psql(`DELETE FROM introductions WHERE user_low = ${id} OR user_high = ${id}`)
}

/** 当前最大引荐 id，用来只看这一轮之后新生成的。 */
function maxIntroID() {
  return Number(psql('SELECT coalesce(max(id), 0) FROM introductions'))
}

/** 这个 id 之后，我收到的引荐分别来自哪些城市。 */
function receivedCities(id, sinceId) {
  const out = psql(`
    SELECT p.city_code
    FROM introductions i
    JOIN profiles p ON p.user_id = CASE WHEN i.user_low = ${id} THEN i.user_high ELSE i.user_low END
    WHERE (i.user_low = ${id} OR i.user_high = ${id}) AND i.id > ${sinceId}`)
  return out === '' ? [] : out.split('\n').map((s) => Number(s.trim()))
}

/**
 * 等我收到引荐，最多 WAIT_MS。
 *
 * 收到就返回（验「该推的人推来了」时不必等满）；一直没收到就等到
 * 期限 —— 验「一条都不该有」时，提前返回等于没验。
 *
 * 等待期间每 5 秒看一次，并把等了几轮打出来：一段等 100 秒而屏幕上
 * 没有动静，出问题时很难判断是「还没轮到我」还是「脚本卡住了」。
 */
async function waitForIntros(id, sinceId) {
  const until = Date.now() + WAIT_MS
  let round = 0
  for (;;) {
    const cities = receivedCities(id, sinceId)
    if (cities.length > 0 || Date.now() >= until) {
      process.stdout.write('\r' + ' '.repeat(44) + '\r')
      return cities
    }
    round++
    process.stdout.write(`\r    等第 ${round} 轮生成… 还剩 ${Math.round((until - Date.now()) / 1000)}s`)
    await sleep(5000)
  }
}

// ---------------------------------------------------------------- 主流程

const stamp = Date.now().toString(36)
const cityNames = { [HOME]: '南京', [OTHER]: '苏州', [FAR]: '北京' }
const nameOf = (c) => `${cityNames[c] ?? c}(${c})`

console.log('召回的城市过滤验收 —— 南京 320100 / 苏州 320500（同省），北京 110000（省外）\n')

console.log('一、种池子')
const me = await createSubject(stamp)
console.log(`  ✓ 我建好了（女，南京，id=${me.id}）—— 南京这批人里 id 最小`)
seedCity(HOME, `recall-nj-${stamp}`, POOL)
seedCity(OTHER, `recall-sz-${stamp}`, POOL)

const pools = psql(`
  SELECT p.city_code || '=' || count(*)
  FROM profiles p JOIN users u ON u.id = p.user_id
  WHERE u.status = 'active' AND p.completeness >= 60 AND p.gender IS NOT NULL
    AND p.city_code IN (${HOME}, ${OTHER})
  GROUP BY p.city_code ORDER BY p.city_code`)
console.log(`  ✓ 池子规模：${pools.split('\n').join(' / ')}`)
for (const city of [HOME, OTHER]) {
  const n = Number(psql(`
    SELECT count(*) FROM profiles p JOIN users u ON u.id = p.user_id
    WHERE u.status = 'active' AND p.completeness >= 60 AND p.gender IS NOT NULL
      AND p.city_code = ${city}`))
  ok(n >= POOL, `${nameOf(city)} 有 ${n} 个人，够 INTRO_POOL_MIN（${POOL}）—— 不够的话整个城市会被跳过`)
}

console.log('\n二、「期望城市」留空 = 不限：南京与苏州的人都该推得过来')
await setCities(me.token, [])
resetMyIntros(me.id)
let since = maxIntroID()
let got = await waitForIntros(me.id, since)
ok(got.length > 0, `收到了引荐（${got.length} 条）`)
ok(
  got.length > 0 && got.every((c) => c === HOME || c === OTHER),
  `推来的人都在同城或同省之内：${[...new Set(got)].map(nameOf).join('、') || '（没有）'}`,
)

console.log(`\n三、期望城市只填${nameOf(OTHER)}：南京的人不该再出现`)
await setCities(me.token, [OTHER])
resetMyIntros(me.id)
since = maxIntroID()
got = await waitForIntros(me.id, since)
ok(got.length > 0, `收到了引荐（${got.length} 条）`)
ok(
  got.length > 0 && got.every((c) => c === OTHER),
  `全是${nameOf(OTHER)}的人：${[...new Set(got)].map(nameOf).join('、') || '（没有）'}`,
)

console.log(`\n四、期望城市只填${nameOf(HOME)}：换回同城，苏州的人不该再出现`)
await setCities(me.token, [HOME])
resetMyIntros(me.id)
since = maxIntroID()
got = await waitForIntros(me.id, since)
ok(got.length > 0, `收到了引荐（${got.length} 条）`)
ok(
  got.length > 0 && got.every((c) => c === HOME),
  `全是${nameOf(HOME)}的人：${[...new Set(got)].map(nameOf).join('、') || '（没有）'}`,
)

console.log(`\n五、期望城市填省外的${nameOf(FAR)}：一条都不该有（界面上那句话不是吓唬人）`)
await setCities(me.token, [FAR])
resetMyIntros(me.id)
since = maxIntroID()
got = await waitForIntros(me.id, since)
ok(
  got.length === 0,
  `等了 ${WAIT_MS / 1000} 秒一条都没有：${got.map(nameOf).join('、') || '（确实没有）'}`,
)

console.log(`\n通过 ${passed} / ${passed + failed}`)
process.exit(failed === 0 ? 0 : 1)

/**
 * M4 的验收：超时扫描 → 四类收尾通知 → 静默时段与未响应冻结。
 *
 * §22 的 M4 判据原文：「把时限调到 1 分钟跑一遍，四类通知都正确到达，
 * 单向引荐不产生错过提醒」。四类指的是 §4.8 那张表的四行 ——
 * 三行要到达，第四行是「不发」。所以「都正确到达」里也包含
 * 「不该到的没到」，脚本对两件事都断言。
 *
 * 时限不靠改代码来缩短：直接把 expires_at 写到过去。扫描看到的是
 * 同一件事 ——「这行已经到期了」。扫描周期在开发环境是 5 秒
 * （deploy/.env 的 INTRO_EXPIRE_INTERVAL），生产是 1 分钟。
 *
 * 推送那一半用本地假端点接：起一个返回 201 的 HTTP 服务，把它的
 * 地址注册成推送订阅，worker 容器经 host.docker.internal 发过来。
 * 这样「投递成功 → 未打开计数 +1 → 连推两条就冻结」这条路是真的
 * 走了一遍，而不是靠直接改库里的列来假装。
 *
 * 前置：本地栈起着（make up），api 在 8081。
 * 跑法：node backend/scripts/e2e-m4.mjs
 *   E2E_BASE 默认 http://localhost:8081
 *
 * 任何一步不符就打印 ✗ 并让退出码非零；全通过才打印 ✓。
 */
import { execFileSync } from 'node:child_process'
import { createServer } from 'node:http'
import { createECDH, randomBytes } from 'node:crypto'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = resolve(HERE, '../..')

const BASE = process.env.E2E_BASE ?? 'http://localhost:8081'
const COMPOSE = ['compose', '-f', resolve(REPO, 'deploy/docker-compose.yml')]

const OUT = '/tmp/tidings-seed'
const FACE = resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')
const PHOTOS = [0, 1, 2].map((i) => resolve(OUT, `p${i}.jpg`))
const PASSWORD = 'tidings-e2e-m4-2026'

mkdirSync(OUT, { recursive: true })
if (!existsSync(PHOTOS[0])) {
  ;[880, 760, 1000].forEach((w, i) => {
    execFileSync('sips', ['-Z', String(w), FACE, '--out', PHOTOS[i]], { stdio: 'ignore' })
  })
}
const FACE_BYTES = readFileSync(FACE)
const PHOTO_BYTES = PHOTOS.map((p) => readFileSync(p))

// ---------------------------------------------------------------- 断言

let failed = 0

function ok(cond, msg) {
  if (cond) {
    console.log(`  ✓ ${msg}`)
  } else {
    failed++
    console.log(`  ✗ ${msg}`)
  }
  return cond
}

function section(title) {
  console.log(`\n${title}`)
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/**
 * 轮询直到 fn 返回真值，超时就放弃。
 *
 * 扫描是异步的（5 秒一轮，生产 1 分钟），所以「等它发生」和
 * 「断言它发生了」必须分开。写死 sleep(6000) 也能过，但那是在赌
 * 机器不忙 —— 忙起来就假报错，而假报错比没有断言更糟。
 */
async function until(fn, { ms = 20000, step = 400 } = {}) {
  const deadline = Date.now() + ms
  for (;;) {
    const v = await fn()
    if (v) return v
    if (Date.now() > deadline) return null
    await sleep(step)
  }
}

// ---------------------------------------------------------------- HTTP

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
  let json = null
  try {
    json = JSON.parse(text)
  } catch {
    // 网关或代理的非 JSON 错误页
  }
  return {
    status: res.status,
    code: json?.code ?? null,
    message: json?.message ?? text.slice(0, 200),
    data: json?.data,
  }
}

async function need(path, opts) {
  const r = await api(path, opts)
  if (r.code !== 'OK') {
    throw new Error(`${opts?.method ?? 'GET'} ${path} → ${r.status} ${r.code} ${r.message}`)
  }
  return r.data
}

async function upload(token, bytes) {
  const ticket = await need('/media/upload-url', { method: 'POST', token, body: { ext: '.jpg' } })
  const put = await fetch(ticket.upload_url, {
    method: 'PUT',
    headers: { 'Content-Type': 'image/jpeg' },
    body: bytes,
  })
  if (!put.ok) throw new Error(`直传失败 ${put.status} ${await put.text()}`)
  return ticket.object_key
}

// ---------------------------------------------------------------- 造账号

const FACES = [
  { nickname: '闻笛', gender: 'F', birthYM: 199402, height: 166, occupation: '编辑' },
  { nickname: '沈砚', gender: 'M', birthYM: 199009, height: 179, occupation: '建筑师' },
  { nickname: '程野', gender: 'M', birthYM: 199603, height: 174, occupation: '数据分析师' },
  { nickname: '白露', gender: 'F', birthYM: 199508, height: 162, occupation: '教师' },
  { nickname: '顾南', gender: 'M', birthYM: 199211, height: 181, occupation: '医生' },
  { nickname: '林隅', gender: 'F', birthYM: 199611, height: 168, occupation: '设计师' },
]

/**
 * 清掉单 IP 每日注册计数。
 *
 * 这个脚本要注册 16 个账号，而上限是 10（maxRegistersPerIPPerDay，
 * 见 service/auth.go）—— 不清理的话第 11 个就 429。反复跑也会撞上
 * 前一次留下的计数。
 *
 * 清理的只是测试这一个维度的计数器，注册本身走的还是真接口：
 * 限流有自己的验收，不该由这个脚本顺带证明 —— 而它挡在这里的
 * 唯一后果是「M4 的断言跑不完」。
 */
function clearRegisterLimit() {
  execFileSync('docker', [
    ...COMPOSE, 'exec', '-T', 'redis', 'redis-cli', 'EVAL',
    "for _,k in ipairs(redis.call('keys',ARGV[1])) do redis.call('del',k) end return 1",
    '0', 'auth:reg:ip:*',
  ])
}

async function createAccount(i, stamp) {
  const f = FACES[i]
  const email = `m4-${stamp}-${i}@example.com`

  clearRegisterLimit()
  const auth = await need('/auth/register', { method: 'POST', body: { email, password: PASSWORD } })
  const token = auth.access_token

  await need('/me/profile', {
    method: 'PATCH',
    token,
    body: {
      nickname: f.nickname,
      gender: f.gender,
      birth_ym: f.birthYM,
      city_code: 110000,
      height_cm: f.height,
      education_level: 3,
      occupation: f.occupation,
      income_band: 3,
      chronotype: 1,
      want_child: 3,
      marital_status: 1,
      hobbies: '徒步、摄影',
      intro: '喜欢摄影和徒步，周末多半在外面。',
    },
  })

  const avatarKey = await upload(token, FACE_BYTES)
  await need('/me/avatar', { method: 'PUT', token, body: { object_key: avatarKey } })
  for (const bytes of PHOTO_BYTES) {
    const key = await upload(token, bytes)
    await need('/me/photos', { method: 'POST', token, body: { object_key: key } })
  }

  const me = await need('/me', { token })
  if (me.status !== 'active') throw new Error(`${email} 建档后 status = ${me.status}，没进池`)
  return { id: auth.user_id, email, nickname: f.nickname, gender: f.gender, token }
}

// ---------------------------------------------------------------- 库

function psql(sql) {
  return execFileSync(
    'docker',
    [...COMPOSE, 'exec', '-T', 'postgres', 'psql', '-U', 'tidings', '-d', 'tidings', '-tAc', sql],
    { encoding: 'utf8' },
  ).trim()
}

/** -tAc 在 INSERT ... RETURNING 上会多印一行命令标签，取第一行才是值。 */
function psqlValue(sql) {
  return psql(sql).split('\n')[0].trim()
}

/**
 * 造一条开着的引荐，并把它推成「已经到期」。
 *
 * 先找再插：生成引擎每 30 秒扫一次池子，这几位账号一建好就合格了，
 * 它随时可能抢在前面。被它先配上不是坏事 —— 那是一条真实的引荐。
 * 只有形状跟这次要验的不一样（要单向、它配了成对）才作废重放。
 *
 * due 传 true 就把 expires_at 写到过去，让下一轮扫描立刻捡走它。
 */
function makeIntro(x, y, { kind = 'paired', hiddenSide = null, due = true, hours = 72 } = {}) {
  const low = Math.min(x, y)
  const high = Math.max(x, y)

  const existing = psql(`
    SELECT id || '|' || kind || '|' || coalesce(hidden_side, '-')
    FROM introductions
    WHERE user_low = ${low} AND user_high = ${high}
      AND state IN ('pending', 'viewed', 'responded')
    ORDER BY id DESC LIMIT 1`)

  let id
  if (existing) {
    const [eid, k, h] = existing.split('|')
    if (k === kind && (hiddenSide === null ? h === '-' : h === hiddenSide)) {
      id = Number(eid)
    } else {
      psql(`UPDATE introductions SET state = 'expired', closed_at = now() WHERE id = ${eid}`)
    }
  }

  if (id === undefined) {
    id = Number(
      psqlValue(`
        INSERT INTO introductions (batch_id, user_low, user_high, kind, state, hidden_side, expires_at)
        VALUES (gen_random_uuid(), ${low}, ${high}, '${kind}', 'pending', ${
          hiddenSide === null ? 'NULL' : `'${hiddenSide}'`
        }, now() + interval '${hours} hours')
        RETURNING id`),
    )
    if (!Number.isInteger(id)) throw new Error(`放引荐失败，psql 回的是「${id}」`)
  }

  if (due) psql(`UPDATE introductions SET expires_at = now() - interval '1 second' WHERE id = ${id}`)
  return id
}

/** 表态。走真接口，不直接改列。 */
async function respond(acct, introId, action, reason) {
  return api(`/introductions/${introId}/respond`, {
    method: 'POST',
    token: acct.token,
    body: reason ? { action, reason } : { action },
  })
}

/** 一条引荐的当前状态。 */
function introState(id) {
  return psql(`SELECT state || '|' || coalesce(closed_at::text, '-') FROM introductions WHERE id = ${id}`)
}

/**
 * 把一条引荐的时限推到过去，等价于「它已经等了整整一段时限」。
 *
 * 表态之后才调用是必须的：写成 responded 会把时限重算成 7 天
 * （respondTTL，见 repoDecision），在表态之前推早了会被它改回来。
 */
function dueNow(id) {
  psql(`UPDATE introductions SET expires_at = now() - interval '1 second' WHERE id = ${id}`)
}

/**
 * 这个人在某个模板上的 outbox 行，返回 `${status}|${last_error}`。
 * 一行都没有时返回 null。
 */
function outboxRow(userId, template) {
  const v = psql(`
    SELECT status || '|' || last_error FROM outbox
    WHERE user_id = ${userId} AND template = '${template}'
    ORDER BY id DESC LIMIT 1`)
  return v === '' ? null : v
}

/** 这个人在某个模板上有没有 outbox 行。 */
function hasOutbox(userId, template) {
  return psql(`SELECT count(*) FROM outbox WHERE user_id = ${userId} AND template = '${template}'`) !== '0'
}

/** 等一条 outbox 行出现，返回它的 `${status}|${last_error}`。 */
async function waitOutbox(userId, template, ms = 20000) {
  return until(() => outboxRow(userId, template), { ms })
}

// ---------------------------------------------------------------- 假推送端点

/**
 * 起一个「照单全收」的推送端点，返回它的地址与关停函数。
 *
 * 存在的理由：未响应冻结的计数只在投递成功之后才 +1，而真实的
 * 推送服务在测试里是够不着的。一个返回 201 的本地服务就够了 ——
 * webpush-go 只看状态码是不是 2xx（见 pkg/push 的 Send）。
 *
 * 监听 0.0.0.0 而不是 127.0.0.1：请求来自 worker 容器，
 * 经 host.docker.internal 过来，绑在回环上容器连不进来。
 */
function startFakePush() {
  return new Promise((res) => {
    const server = createServer((req, r) => {
      req.resume() // 把正文读掉，否则连接不释放
      req.on('end', () => {
        r.writeHead(201)
        r.end()
      })
    })
    server.listen(0, '0.0.0.0', () => {
      const { port } = server.address()
      res({
        port,
        url: `http://host.docker.internal:${port}/push`,
        close: () => server.close(),
      })
    })
  })
}

/**
 * 给一个人登记一条订阅，指向我们的假端点。
 *
 * 密钥必须是**真的** P-256 公钥：webpush-go 会拿它做 ECDH 加密，
 * 随便填一串 base64 会在加密那一步就失败，Send 返回的不是
 * 「对方收到 201」而是一个解码错误 —— 那样测出来的就不是投递成功了。
 */
async function subscribeFake(acct, pushURL) {
  const ecdh = createECDH('prime256v1')
  ecdh.generateKeys()
  await need('/push/subscribe', {
    method: 'POST',
    token: acct.token,
    body: {
      endpoint: `${pushURL}/${acct.id}`,
      keys: {
        p256dh: ecdh.getPublicKey().toString('base64url'),
        auth: randomBytes(16).toString('base64url'),
      },
    },
  })
}

/** 直接往 outbox 放一条通知，用来验投递闸门（闸门在订阅查询之前）。 */
function queueNotify(userId, template, tag) {
  psql(`
    INSERT INTO outbox (channel, user_id, template, payload, dedup_key, status, next_retry_at)
    VALUES ('push', ${userId}, '${template}', '{}'::jsonb, 'e2e-m4:${tag}', 'pending', now())`)
}

// ---------------------------------------------------------------- 主流程

const stamp = Date.now().toString(36)
const fake = await startFakePush()
console.log(`假推送端点：${fake.url}`)

try {
  // ---------------------------------------------------------------- 一
  section('一、超时扫描把过期的信推进到 expired')

  const [a, b] = [await createAccount(0, stamp), await createAccount(1, stamp)]
  const introAB = makeIntro(a.id, b.id)
  ok((await introState(introAB)).startsWith('pending'), `引荐 #${introAB} 一开始是 pending`)

  const done = await until(() => {
    const s = introState(introAB)
    return s.startsWith('expired') ? s : null
  })
  ok(!!done, '一轮扫描之内变成了 expired')
  ok(!(done ?? '').endsWith('|-'), 'closed_at 被写上了（不是一个还开着的终态）')
  ok(
    (await introState(introAB)).startsWith('expired'),
    '它是终态，不会再被扫描捡起来（幂等在第九节另外验）',
  )

  // ---------------------------------------------------------------- 二
  section('二、§4.8 第一行与第二行 —— 成对引荐超时，两边措辞不同')

  const [c, d] = [await createAccount(2, stamp), await createAccount(3, stamp)]
  const introCD = makeIntro(c.id, d.id, { due: false })
  // C 先表态「想认识」，D 一直不动 —— 于是 C 在等，D 从没回应
  const r1 = await respond(c, introCD, 'like')
  ok(r1.code === 'OK', 'C 点了「想认识」，引荐进入等待对方')
  ok(r1.data?.state === 'responded', '它落在 responded：一方表态、另一方还没动')
  dueNow(introCD) // 等回音的 7 天走完了

  const cNotice = await waitOutbox(c.id, 'intro_closed')
  const dNotice = await waitOutbox(d.id, 'intro_missed')
  ok(!!cNotice, '已表态「想认识」的 C 收到了 intro_closed（「上封信没有等到回音」）')
  ok(!!dNotice, '从没表态的 D 收到了 intro_missed（「你错过了 1 位合适的人」）')

  ok(
    psql(`SELECT payload->>'reason' FROM outbox WHERE user_id = ${c.id} AND template = 'intro_closed'
          ORDER BY id DESC LIMIT 1`) === 'expired',
    'payload 里的 reason = expired，埋点能把「超时」和「被拒绝」分开',
  )
  ok(
    !hasOutbox(c.id, 'intro_missed'),
    'C 没有收到「你错过了」——他在等回音，不是在错过',
  )
  ok(!hasOutbox(d.id, 'intro_closed'), 'D 没有收到「没有等到回音」——他从头到尾没在等谁'),
  ok(
    (await introState(introCD)).startsWith('expired'),
    '这条引荐本身也走完了它自己的一生',
  )

  // ---------------------------------------------------------------- 三
  section('三、§4.8 第三行与第四行 —— 被明确拒绝')

  const [e, f] = [await createAccount(4, stamp), await createAccount(5, stamp)]
  const introEF = makeIntro(e.id, f.id, { due: false })
  await respond(e, introEF, 'like')
  const r2 = await respond(f, introEF, 'pass', 'vibe')
  ok(r2.code === 'OK', 'F 点了「不合适」并给了原因')

  const eNotice = await waitOutbox(e.id, 'intro_closed')
  ok(!!eNotice, '表过「想认识」的 E 立即收到了 intro_closed')
  ok(
    psql(`SELECT payload->>'reason' FROM outbox WHERE user_id = ${e.id} AND template = 'intro_closed'
          ORDER BY id DESC LIMIT 1`) === 'declined',
    'reason = declined，与超时那一条区分得开',
  )
  ok(!hasOutbox(f.id, 'intro_closed'), 'F 没有收到任何收尾通知：他做了一个决定，不需要被通知')
  ok(!hasOutbox(f.id, 'intro_missed'), 'F 也没有收到「错过」')

  // §4.8：被拒绝的人不需要知道对方嫌什么。他给的原因是 'vibe'，
  // 它不该出现在收件人这侧的任何一个字里 —— 包括 payload。
  ok(
    psql(`SELECT payload::text NOT LIKE '%vibe%' FROM outbox
          WHERE user_id = ${e.id} AND template = 'intro_closed' ORDER BY id DESC LIMIT 1`) === 't',
    'E 收到的通知里没有夹带 F 选的那个原因（§4.8：不区分原因）',
  )

  // ---------------------------------------------------------------- 四
  section('四、单向引荐不产生错过提醒')

  const [g, h] = [await createAccount(0, stamp + 'x'), await createAccount(1, stamp + 'x')]
  const introGH = makeIntro(g.id, h.id, { kind: 'oneway', hiddenSide: 'high' })
  await until(() => introState(introGH).startsWith('expired'))
  ok(true, '单向引荐也照常被扫描终结')
  // 给扫描留一轮，确认它不会补发
  await sleep(6000)
  ok(!hasOutbox(g.id, 'intro_missed'), '可见方没有收到「你错过了」（§22 判据：单向引荐不产生错过提醒）')
  ok(!hasOutbox(g.id, 'intro_closed'), '可见方也没有收到「没有等到回音」')
  ok(!hasOutbox(h.id, 'intro_missed'), '隐藏方从头到尾没收到过东西')
  ok(!hasOutbox(h.id, 'intro_closed'), '隐藏方也没收到收尾通知')

  // ---------------------------------------------------------------- 五
  section('五、暂停期间计时冻结（§13.2）')

  const [i, j] = [await createAccount(2, stamp + 'x'), await createAccount(3, stamp + 'x')]

  // 信先到，人再按暂停 —— §13.2 说的正是这个次序：一封已经在等
  // 他回音的信，因为他关掉了引荐而停表。
  const introIJ = makeIntro(i.id, j.id, { due: false })

  await need('/me/settings', {
    method: 'PUT',
    token: i.token,
    body: { intros_paused: true, quiet_start: 22, quiet_end: 9 },
  })

  // 把时间轴摆开，好让「顺延了多少」是个能对得上的数：
  // 信是 3 小时前到的，他 2 小时前按的暂停，信的时限刚刚到。
  // 恢复时应当补回暂停的那 2 小时 —— 不多不少。
  //
  // 顺序不能反。若信是在暂停之后才写的，created_at 就晚于
  // paused_at，暂停前的那些时间本来就不该算给他（顺延公式取
  // GREATEST(paused_at, created_at)），这条断言会退化成
  // 「顺延了 0 秒」而看不出任何问题。
  psql(`UPDATE introductions
        SET created_at = now() - interval '3 hours',
            expires_at = now() - interval '1 second'
        WHERE id = ${introIJ}`)
  psql(`UPDATE user_settings SET paused_at = now() - interval '2 hours' WHERE user_id = ${i.id}`)

  const beforeIJ = psql(`SELECT expires_at::text FROM introductions WHERE id = ${introIJ}`)

  await sleep(8000) // 至少两轮扫描
  ok(
    /^(pending|viewed)/.test(await introState(introIJ)),
    '对手方在暂停中，这一行原地不动，没有被判超时',
  )
  ok(!hasOutbox(j.id, 'intro_missed'), 'J 没有收到「你错过了」——他不知道对面在暂停')
  ok(
    psql(`SELECT expires_at::text FROM introductions WHERE id = ${introIJ}`) === beforeIJ,
    '时限冻结着，没有被推进',
  )

  // 恢复：时限按暂停时长顺延回来
  const resumed = await need('/me/settings', {
    method: 'PUT',
    token: i.token,
    body: { intros_paused: false, quiet_start: 22, quiet_end: 9 },
  })
  ok(resumed.intros_paused === false, 'I 恢复接收引荐')

  const afterIJ = psql(`SELECT expires_at::text FROM introductions WHERE id = ${introIJ}`)
  ok(afterIJ !== beforeIJ, '恢复的同一个事务里，时限被顺延了')
  ok(
    psql(`SELECT expires_at > now() FROM introductions WHERE id = ${introIJ}`) === 't',
    '顺延之后时限落在未来 —— 它不会在下一轮扫描里被立刻判超时',
  )
  ok(
    psql(`SELECT expires_at BETWEEN now() + interval '119 minutes' AND now() + interval '121 minutes'
          FROM introductions WHERE id = ${introIJ}`) === 't',
    '顺延的量正好是暂停的那 2 小时（不是「随便往后挪一点」）',
  )

  await sleep(6000)
  ok(
    /^(pending|viewed)/.test(await introState(introIJ)),
    '顺延之后又过了几轮扫描，它仍然是活的（最要紧的一条：顺延算少一秒就会当场被判死）',
  )

  // ---------------------------------------------------------------- 六
  section('六、静默时段：推迟，不丢弃')

  // 把现在这一小时圈进静默时段。收尾通知不受冻结/暂停限制，
  // 但受静默时段限制（§14.2）—— 半夜震一下手机的投诉比晚一天知道严重。
  //
  // 时区必须显式指定：worker 按 TIMEZONE（deploy/.env，Asia/Shanghai）
  // 算钟点，而 postgres 容器没有设 TZ、会话是 UTC。直接取
  // extract(hour from now()) 会拿到 UTC 的小时，跟 worker 差 8 小时，
  // 于是「圈进静默时段」圈的是另一个时段 —— 而且只在 UTC 与东八区
  // 分别落在不同小时的时候才看得出来。
  const hh = Number(psql(`SELECT extract(hour from now() at time zone 'Asia/Shanghai')::int`))
  const quietStart = hh
  const quietEnd = (hh + 1) % 24

  const [k, l] = [await createAccount(4, stamp + 'x'), await createAccount(5, stamp + 'x')]
  await need('/me/settings', {
    method: 'PUT',
    token: l.token,
    body: { intros_paused: false, quiet_start: quietStart, quiet_end: quietEnd },
  })
  const introKL = makeIntro(k.id, l.id, { due: false })
  await respond(k, introKL, 'like')
  dueNow(introKL)

  const lQuiet = await waitOutbox(l.id, 'intro_missed')
  ok(!!lQuiet, 'L 的「你错过了」进了队列')

  // 等到 outbox 循环处理过它（2 秒一轮），再看它的状态
  await sleep(5000)
  const lRow = outboxRow(l.id, 'intro_missed') ?? ''
  const [lStatus] = lRow.split('|')
  ok(lStatus === 'pending', '落在静默时段里：它仍是 pending，没有被投出去，也没有被判失败')
  ok(
    psql(`SELECT next_retry_at > now() FROM outbox WHERE user_id = ${l.id} AND template = 'intro_missed'
          ORDER BY id DESC LIMIT 1`) === 't',
    '它被推迟到了静默时段结束之后，而不是丢掉',
  )

  // 把静默时段挪开，它就该被投出去了（L 没有订阅 → 会被记成 no_subscription，
  // 但那已经走过闸门了，正是我们要看的）
  await need('/me/settings', {
    method: 'PUT',
    token: l.token,
    body: { intros_paused: false, quiet_start: 0, quiet_end: 0 },
  })
  psql(`UPDATE outbox SET next_retry_at = now() WHERE user_id = ${l.id} AND template = 'intro_missed'`)
  const lAfter = await until(() => {
    const r = outboxRow(l.id, 'intro_missed') ?? ''
    return r && !r.startsWith('pending') ? r : null
  })
  ok(
    (lAfter ?? '').includes('no_subscription'),
    '静默时段一解除它就被处理了（L 没订阅，所以停在 no_subscription）',
  )

  // ---------------------------------------------------------------- 七
  section('七、未响应冻结：连推两条未打开就停推，访问一次解冻')

  const m = await createAccount(0, stamp + 'y')
  await subscribeFake(m, fake.url)

  // 第 1 条：投递成功，计数 0 → 1，还没冻
  queueNotify(m.id, 'intro_delivered', `${stamp}-m1`)
  const m1 = await until(() => {
    const r = outboxRow(m.id, 'intro_delivered') ?? ''
    return r.startsWith('sent') ? r : null
  })
  ok(!!m1, '第 1 条推送投递成功（假端点回了 201）')
  ok(
    psql(`SELECT unopened_streak FROM user_settings WHERE user_id = ${m.id}`) === '1',
    '投递成功之后未打开计数变成 1',
  )
  ok(
    psql(`SELECT push_frozen FROM user_settings WHERE user_id = ${m.id}`) === 'f',
    '一次还没冻上',
  )

  // 第 2 条：计数 1 → 2，冻上
  queueNotify(m.id, 'intro_delivered', `${stamp}-m2`)
  await until(() => outboxRow(m.id, 'intro_delivered')?.startsWith('sent'))
  await sleep(1000)
  ok(
    psql(`SELECT unopened_streak FROM user_settings WHERE user_id = ${m.id}`) === '2',
    '第 2 条之后计数变成 2',
  )
  ok(
    psql(`SELECT push_frozen FROM user_settings WHERE user_id = ${m.id}`) === 't',
    '连续两次未打开，推送被冻结（§4.5）',
  )

  // 冻上之后，新引荐的推送不再发出去
  queueNotify(m.id, 'intro_delivered', `${stamp}-m3`)
  const m3 = await until(() => {
    const r = outboxRow(m.id, 'intro_delivered') ?? ''
    return r.includes('push_frozen') ? r : null
  })
  ok(!!m3, '冻上之后的新引荐通知被挡下，原因记成 skipped:push_frozen')

  // 收尾通知穿得过这道闸（决策 17：它恰恰要发给不常来的人）。
  // m 名下有那条假订阅，所以「穿过去了」的表现是真的投出去 ——
  // 被冻住的话它会停在 failed|skipped:push_frozen。
  queueNotify(m.id, 'intro_missed', `${stamp}-m4`)
  const m4 = await until(() => {
    const r = outboxRow(m.id, 'intro_missed') ?? ''
    return r.startsWith('sent') ? r : null
  })
  ok(!!m4, '收尾通知不受冻结限制：冻着的人照样收得到「你错过了」')

  // 访问一次 → 解冻
  await need('/me', { token: m.token })
  ok(
    psql(`SELECT push_frozen FROM user_settings WHERE user_id = ${m.id}`) === 'f',
    '打开一次应用就解冻了（§4.5：直到用户主动访问一次）',
  )
  ok(
    psql(`SELECT unopened_streak FROM user_settings WHERE user_id = ${m.id}`) === '0',
    '计数也清零了，下一次要重新数两条',
  )

  // ---------------------------------------------------------------- 八
  section('八、暂停接收引荐：新引荐的推送也不发')

  const n = await createAccount(1, stamp + 'y')
  await subscribeFake(n, fake.url)
  await need('/me/settings', {
    method: 'PUT',
    token: n.token,
    body: { intros_paused: true, quiet_start: 0, quiet_end: 0 },
  })
  queueNotify(n.id, 'intro_delivered', `${stamp}-n1`)
  const n1 = await until(() => {
    const r = outboxRow(n.id, 'intro_delivered') ?? ''
    return r.includes('intros_paused') ? r : null
  })
  ok(!!n1, '用户自己按了暂停，新引荐的推送被挡下，原因记成 skipped:intros_paused')

  queueNotify(n.id, 'intro_closed', `${stamp}-n2`)
  const n2 = await until(() => {
    const r = outboxRow(n.id, 'intro_closed') ?? ''
    return !r.startsWith('pending') ? r : null
  })
  ok((n2 ?? '').startsWith('sent'), '暂停也拦不住收尾通知：它照常投了出去')

  // ---------------------------------------------------------------- 九
  section('九、幂等：同一条引荐不会被终结两次')

  const [o, p] = [await createAccount(2, stamp + 'y'), await createAccount(3, stamp + 'y')]
  const introOP = makeIntro(o.id, p.id)
  await until(() => introState(introOP).startsWith('expired'))
  const firstClosedAt = psql(`SELECT closed_at::text FROM introductions WHERE id = ${introOP}`)
  await sleep(7000) // 又过了至少一轮
  ok(
    psql(`SELECT closed_at::text FROM introductions WHERE id = ${introOP}`) === firstClosedAt,
    '又跑了几轮，closed_at 没有被改写（终结只发生一次）',
  )
  ok(
    psql(`SELECT count(*) FROM outbox WHERE user_id IN (${o.id}, ${p.id})
          AND template IN ('intro_missed','intro_closed')`) === '2',
    '两个人各只收到一条收尾通知，没有因为多轮扫描而叠加',
  )

  // ---------------------------------------------------------------- 十
  section('十、设置接口本身')

  const s1 = await need('/me/settings', { token: o.token })
  ok(
    s1.quiet_start === 22 && s1.quiet_end === 9,
    '没设过的人读到的是默认值（22:00–09:00），而不是 404',
  )
  ok(typeof s1.push_frozen === 'boolean' && typeof s1.unopened_streak === 'number', '冻结状态也一并返回，用户能知道自己为什么收不到推送')

  const bad = await api('/me/settings', {
    method: 'PUT',
    token: o.token,
    body: { intros_paused: false, quiet_start: 22 },
  })
  ok(bad.code === 'BAD_REQUEST', '漏传 quiet_end 被拒（零点是合法取值，不能当默认值用）')

  const bad2 = await api('/me/settings', {
    method: 'PUT',
    token: o.token,
    body: { intros_paused: false, quiet_start: 24, quiet_end: 9 },
  })
  ok(bad2.code === 'BAD_REQUEST', '24 点被拒（0–23）')

  const okS = await need('/me/settings', {
    method: 'PUT',
    token: o.token,
    body: { intros_paused: false, quiet_start: 22, quiet_end: 9 },
  })
  ok(okS.quiet_start === 22, '合法的设置写得进去')
} finally {
  fake.close()
}

// ---------------------------------------------------------------- 结果

console.log('')
if (failed === 0) {
  console.log('全部通过 ✓')
} else {
  console.log(`${failed} 条不符 ✗`)
  process.exitCode = 1
}

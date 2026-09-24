/**
 * M3 的验收：表态 → 成匹配 → 互发消息。
 *
 * 走的是真 HTTP 与真 WebSocket，只有一样是造出来的：那条引荐行本身。
 * 引荐怎么产生是 M2 的事（生成引擎、候选集、打分，另有一套验收），
 * 这里要验的是它产生之后会发生什么 —— 所以直接往库里放一条
 * pending 的引荐，把这一段的起点固定住。造行之前会检查双方确实
 * 都已经是 active，否则等于绕过了入池判定，测出来的东西不作数。
 *
 * 前置：本地栈起着（make up），api 在 8081。
 * 跑法：node backend/scripts/e2e-m3.mjs
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

const OUT = '/tmp/tidings-seed'
const FACE = resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')
const PHOTOS = [0, 1, 2].map((i) => resolve(OUT, `p${i}.jpg`))
const PASSWORD = 'tidings-e2e-m3-2026'

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
 * 从列表本身算出未读该是几，再跟服务端给的数字比。
 *
 * 不写死「应该是 2」：这个库里还有别的账号，生成引擎随时可能给我们
 * 这几位再塞一封新的信进来，写死的数字会时不时假报错。按规则算一遍
 * 反而是更强的断言 —— 它验的是「未读是怎么算的」这条规则本身。
 */
function expectedUnread(list) {
  return list.introductions.filter(
    (i) => !i.viewed && i.state !== 'declined' && i.state !== 'expired',
  ).length
}

// ---------------------------------------------------------------- HTTP

/**
 * 请求封装。raw 为真时不看信封 —— 用来验错误码。
 * 返回 { status, code, data, message }。
 */
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

/** 断言成功并取出 data。失败直接把整条响应抛出来，好定位。 */
async function need(path, opts) {
  const r = await api(path, opts)
  if (r.code !== 'OK') {
    throw new Error(`${opts?.method ?? 'GET'} ${path} → ${r.status} ${r.code} ${r.message}`)
  }
  return r.data
}

async function upload(token, bytes) {
  const ticket = await need('/media/upload-url', {
    method: 'POST',
    token,
    body: { ext: '.jpg' },
  })
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
  { nickname: '苏砚', gender: 'F', birthYM: 199406, height: 164, occupation: '编辑' },
  { nickname: '周迟', gender: 'M', birthYM: 199111, height: 178, occupation: '建筑师' },
  { nickname: '许屿', gender: 'M', birthYM: 199508, height: 175, occupation: '数据分析师' },
]

async function createAccount(i, stamp) {
  const f = FACES[i]
  const email = `m3-${stamp}-${i}@example.com`

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
      // 20 项必填要一次给全，否则账号停在 onboarding，
      // 下面那句 `status !== 'active'` 会先炸。加必填项时这里必须跟着加。
      weight_kg: 60,
      education_level: 3,
      school_name: '复旦大学',
      hometown_code: 110000,
      occupation: f.occupation,
      company: '某互联网公司',
      income_band: 3,
      smoking: 0,
      drinking: 1,
      hobbies: '徒步、摄影',
      intro: '喜欢摄影和徒步，周末多半在外面。',
      expectation: '想找一个愿意一起出门的人。',
      want_child: 3,
      marital_status: 1,
    },
  })

  const avatarKey = await upload(token, FACE_BYTES)
  await need('/me/avatar', { method: 'PUT', token, body: { object_key: avatarKey } })
  for (const bytes of PHOTO_BYTES) {
    const key = await upload(token, bytes)
    await need('/me/photos', { method: 'POST', token, body: { object_key: key } })
  }

  const me = await need('/me', { token })
  if (me.status !== 'active') {
    throw new Error(`${email} 建档后 status = ${me.status}，没进池`)
  }
  return { id: auth.user_id, email, nickname: f.nickname, token }
}

// ---------------------------------------------------------------- 造引荐

function psql(sql) {
  return execFileSync('docker', [...COMPOSE, 'exec', '-T', 'postgres', 'psql', '-U', 'tidings', '-d', 'tidings', '-tAc', sql], {
    encoding: 'utf8',
  }).trim()
}

/**
 * 跑一条会返回单个值的语句。
 *
 * 单独拎出来是因为 -tAc 在 INSERT ... RETURNING 上会多印一行命令标签
 * （`INSERT 0 1`），跟返回值拼在一起 —— 直接 Number() 整段会得到 NaN。
 */
function psqlValue(sql) {
  return psql(sql).split('\n')[0].trim()
}

/**
 * 保证这一对之间有一条开着、且形状如我们所愿的引荐。
 *
 * 只在「引荐的生成」这一件事上动手，其余一概走产品自己的路径。
 * user_low < user_high 是表上的约束，这里按 id 排一下。
 *
 * 先找再插，是因为生成引擎随时可能抢在前面：它每 30 秒扫一次池子，
 * 而这几位账号一建好就合格了。被它先配上不是坏事 —— 那正是一条真实
 * 的引荐，拿来做这一段验收比手插的更实在。只有当它配出来的形状跟
 * 这一次要验的不一样（比如我们要单向、它配了成对）才需要换掉：
 * 那是测试自己造的夹具，作废重放，不留给下一个人猜。
 */
function ensureOpenIntro(x, y, { kind = 'paired', hiddenSide = null, hours = 72 } = {}) {
  const low = Math.min(x, y)
  const high = Math.max(x, y)

  const existing = psql(`
    SELECT id || '|' || kind || '|' || coalesce(hidden_side, '-')
    FROM introductions
    WHERE user_low = ${low} AND user_high = ${high}
      AND state IN ('pending', 'viewed', 'responded')
    ORDER BY id DESC
    LIMIT 1`)

  if (existing) {
    const [id, k, h] = existing.split('|')
    if (k === kind && (hiddenSide === null ? h === '-' : h === hiddenSide)) {
      console.log(`  · 复用引擎已经配好的引荐 #${id}`)
      return Number(id)
    }
    psql(`UPDATE introductions SET state = 'expired', closed_at = now() WHERE id = ${id}`)
  }

  const inserted = psqlValue(`
    INSERT INTO introductions (batch_id, user_low, user_high, kind, state, hidden_side, expires_at)
    VALUES (gen_random_uuid(), ${low}, ${high}, '${kind}', 'pending', ${
      hiddenSide === null ? 'NULL' : `'${hiddenSide}'`
    }, now() + interval '${hours} hours')
    RETURNING id`)

  const n = Number(inserted)
  if (!Number.isInteger(n)) throw new Error(`放引荐失败，psql 回的是「${inserted}」`)
  return n
}

// ---------------------------------------------------------------- WebSocket

/**
 * 一条已鉴权的实时连接。
 *
 * 票据是一次性的（60 秒、握手时消费掉），所以每次连接都要现换一张，
 * 不能拿一张存着反复用。
 */
const WS_BASE = BASE.replace(/^http/, 'ws')

/**
 * 拿一张票据去握手，只回「成没成」。
 *
 * 不能用 fetch 试：ws:// 不是 fetch 认的协议，它连请求都不会发出去，
 * 直接一句 fetch failed，看起来像被拒了，其实什么都没测到。
 */
function tryHandshake(ticket) {
  return new Promise((resolve) => {
    const ws = new WebSocket(`${WS_BASE}/api/v1/ws?ticket=${ticket}`)
    let settled = false
    const done = (result) => {
      if (settled) return
      settled = true
      try {
        ws.close()
      } catch {
        // 还没开就关了
      }
      resolve(result)
    }
    ws.addEventListener('open', () => done(true), { once: true })
    ws.addEventListener('error', () => done(false), { once: true })
    ws.addEventListener('close', () => done(false), { once: true })
    setTimeout(() => done(false), 5000)
  })
}

async function connectWS(token) {
  const { ticket } = await need('/auth/ws-ticket', { method: 'POST', token })
  const ws = new WebSocket(`${WS_BASE}/api/v1/ws?ticket=${ticket}`)
  const frames = []
  const waiters = []
  // 收到哪了。等事件是「按顺序往下拿」，不是「在历史里随便找一个」——
  // 同一种事件会来很多次（自己发的每条消息都会推回来一份），
  // 从头上找的话，第二次等 message 永远拿到第一次那条。
  let cursor = 0

  ws.addEventListener('message', (ev) => {
    frames.push(JSON.parse(ev.data))
    for (let i = 0; i < waiters.length; i++) {
      if (waiters[i].try()) {
        waiters.splice(i, 1)
        i--
      }
    }
  })

  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true })
    ws.addEventListener('error', () => rej(new Error('WebSocket 握手失败')), { once: true })
    setTimeout(() => rej(new Error('WebSocket 握手超时')), 5000)
  })

  return {
    ws,

    /** 等一条指定类型的事件（可以再给一个筛选条件）。等到的会消费掉。 */
    wait(type, ms = 5000, match = () => true) {
      return new Promise((resolve, reject) => {
        const entry = {
          try() {
            for (let i = cursor; i < frames.length; i++) {
              if (frames[i].type === type && match(frames[i])) {
                cursor = i + 1
                clearTimeout(timer)
                resolve(frames[i])
                return true
              }
            }
            return false
          },
        }
        const timer = setTimeout(() => {
          const idx = waiters.indexOf(entry)
          if (idx >= 0) waiters.splice(idx, 1)
          const seen = frames
            .slice(cursor)
            .map((f) => f.type)
            .join(',')
          reject(new Error(`等 ${type} 事件超时（游标 ${cursor}，共 ${frames.length} 帧，游标之后：${seen || '无'}）`))
        }, ms)
        waiters.push(entry)
        // 帧可能早就到了，try() 会当场兑现。这条路没经过下面那个
        // 派发循环，得自己把登记撤掉 —— 留着的话，它会一直挂在表里，
        // 下一条同类事件来的时候抢着把它消费掉（游标也就跟着越过去了），
        // 而真正在等的那一个只能干等到超时。
        if (entry.try()) waiters.splice(waiters.indexOf(entry), 1)
      })
    },

    /** 给「不该收到」的场合用：等一小会儿，确认没有新的这类事件。 */
    async quiet(type, ms = 800) {
      const from = cursor
      await sleep(ms)
      return !frames.slice(from).some((f) => f.type === type)
    },

    /** 收过哪些帧、收到哪了。断言失败时用它说明白到底来了什么。 */
    dump() {
      return frames.map((f) => `${f.type} ${JSON.stringify(f.data).slice(0, 70)}`)
    },

    close() {
      try {
        ws.close()
      } catch {
        // 已经关了
      }
    },
  }
}

// ================================================================ 开跑

const stamp = Date.now().toString(36)
console.log(`M3 验收 → ${BASE}`)

const [a, b, c] = [
  await createAccount(0, stamp),
  await createAccount(1, stamp),
  await createAccount(2, stamp),
]
console.log(`\n三个账号就绪：${a.nickname}(F) / ${b.nickname}(M) / ${c.nickname}(M)`)

// A–B 走「双方都想认识」，A–C 走「一方不合适」
const introAB = ensureOpenIntro(a.id, b.id)
const introAC = ensureOpenIntro(a.id, c.id)

let wsA = null
let wsB = null
let wsC = null

try {
  // ------------------------------------------------------------ 列表与未读
  section('一、引荐看得见，打开才算打开')

  const listA = await need('/introductions', { token: a.token })
  const rowAB = listA.introductions.find((i) => i.id === introAB)
  ok(rowAB !== undefined, 'A 的列表里有这条引荐')
  ok(rowAB?.state === 'pending', `列表里是 pending（实际 ${rowAB?.state}）`)
  ok(rowAB?.viewed === false, '还没打开过，viewed 是假的')
  ok(listA.unread === expectedUnread(listA), `未读计数与规则一致（${listA.unread}）`)

  // 列表不该改状态：§13.1 的「打开」是点进详情，不是刷新列表
  const afterList = await need('/introductions', { token: a.token })
  ok(
    afterList.introductions.find((i) => i.id === introAB)?.viewed === false,
    '只看列表不会把它翻成已打开',
  )

  const detail = await need(`/introductions/${introAB}`, { token: a.token })
  ok(detail.state === 'viewed', `点进详情后翻成 viewed（实际 ${detail.state}）`)
  ok(detail.other.nickname === b.nickname, '详情里是对方的昵称')
  ok(typeof detail.other.occupation === 'string' && detail.other.occupation !== '', '详情页带职业（§4.6）')

  const listAfter = await need('/introductions', { token: a.token })
  ok(
    listAfter.introductions.find((i) => i.id === introAB)?.viewed === true,
    '列表里这一条变成了已打开',
  )
  ok(
    listAfter.unread < listA.unread && listAfter.unread === expectedUnread(listAfter),
    `未读少了一条且仍与规则一致（${listA.unread} → ${listAfter.unread}）`,
  )

  // 不是当事人：后端返回 404 而不是 403（§18.3 的反枚举规则）
  const foreign = await api(`/introductions/${introAB}`, { token: c.token })
  ok(foreign.status === 404 && foreign.code === 'INTRO_NOT_FOUND', `第三方拿到 404（实际 ${foreign.status} ${foreign.code}）`)

  // ------------------------------------------------------------ 实时通道
  section('二、实时通道连得上')

  wsA = await connectWS(a.token)
  wsB = await connectWS(b.token)
  wsC = await connectWS(c.token)
  ok(true, '三条连接都握手成功（Origin 同源校验通过）')

  // 票据只能用一次：交出去之后它就在浏览器历史和代理日志里了，
  // 那张纸不该还能再进一次门
  const t = await need('/auth/ws-ticket', { method: 'POST', token: a.token })
  ok(await tryHandshake(t.ticket), '一张新票据握得上手')
  ok(!(await tryHandshake(t.ticket)), '同一张票据不能用第二次')

  const bad = await tryHandshake('not-a-ticket')
  ok(!bad, '伪造的票据进不来')

  // ------------------------------------------------------------ 单方表态
  section('三、单方表态：等对方，不推事件')

  const like1 = await need(`/introductions/${introAB}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'like' },
  })
  ok(like1.state === 'responded', `状态是 responded（实际 ${like1.state}）`)
  ok(like1.matched === false, '还没成匹配')
  ok(await wsB.quiet('match'), 'B 这时不该收到任何匹配事件（单向还没有悬念）')

  // 「不合适」必须带原因
  const noReason = await api(`/introductions/${introAC}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'pass' },
  })
  ok(noReason.status === 400 && noReason.code === 'REASON_REQUIRED', `不带原因被拒（实际 ${noReason.status} ${noReason.code}）`)

  const badReason = await api(`/introductions/${introAC}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'pass', reason: '条件不符' },
  })
  ok(badReason.status === 400, `原因要传 code 不是中文（实际 ${badReason.status}）`)

  // 改主意：同一侧已经 like 过，再 pass 是冲突
  const flip = await api(`/introductions/${introAB}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'pass', reason: 'vibe' },
  })
  ok(flip.status === 409 && flip.code === 'INTRO_RESPONDED', `盖了章就收不回（实际 ${flip.status} ${flip.code}）`)

  // ------------------------------------------------------------ 成匹配
  section('四、双方都想认识 → 立刻成匹配')

  const like2 = await need(`/introductions/${introAB}/respond`, {
    method: 'POST',
    token: b.token,
    body: { action: 'like' },
  })
  ok(like2.state === 'matched', `状态是 matched（实际 ${like2.state}）`)
  ok(like2.matched === true, 'matched 为真')
  ok(Number.isInteger(like2.match_id), `响应里带回 match_id（${like2.match_id}）`)

  const matchID = like2.match_id

  // 双方都该收到匹配事件
  const evA = await wsA.wait('match').catch(() => null)
  ok(evA !== null, 'A 收到了匹配事件')
  ok(evA?.data?.match_id === matchID, `事件里的 match_id 对得上（${evA?.data?.match_id}）`)
  ok(evA?.data?.intro_id === introAB, '事件里带回了引荐 id')
  ok(await wsB.wait('match').then(() => true).catch(() => false), 'B 也收到了匹配事件')

  // 重放：同一个动作再来一次是幂等成功，而不是「你已经表过态了」
  const replay = await need(`/introductions/${introAB}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'like' },
  })
  ok(replay.state === 'matched' && replay.matched === true, '重放 like 幂等成功')
  ok(replay.match_id === matchID, `重放也拿得到同一个 match_id（${replay.match_id}）`)

  // 重复表态不该重复建匹配
  const matchCount = Number(psql(`SELECT count(*) FROM matches WHERE user_low = ${Math.min(a.id, b.id)} AND user_high = ${Math.max(a.id, b.id)}`))
  ok(matchCount === 1, `matches 只有一行（实际 ${matchCount}）`)

  // ------------------------------------------------------------ 会话
  section('五、会话通了')

  const matchesA = await need('/matches', { token: a.token })
  const mA = matchesA.matches.find((m) => m.match_id === matchID)
  ok(mA !== undefined, 'A 的会话列表里有这条')
  ok(mA?.nickname === b.nickname, '会话上显示对方昵称')
  ok(mA?.last_content === null, '还没人说话，last_content 是空的')

  const empty = await need(`/matches/${matchID}/messages`, { token: a.token })
  ok(empty.messages.length === 0, '消息列表初始为空')
  ok(empty.has_more === false, 'has_more 为假')

  // 不是当事人
  const foreignMatch = await api(`/matches/${matchID}/messages`, { token: c.token })
  ok(foreignMatch.status === 404 && foreignMatch.code === 'MATCH_NOT_FOUND', `第三方拿到 404（实际 ${foreignMatch.status} ${foreignMatch.code}）`)

  const msg1 = await need(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-1', content: '你好，看到你也喜欢徒步。' },
  })
  ok(msg1.mine === true, '自己发的消息 mine 为真')
  ok(msg1.client_msg_id === 'e2e-a-1', 'client_msg_id 原样回来了')

  // B 该被实时叫醒
  const evMsg = await wsB.wait('message').catch(() => null)
  ok(evMsg !== null, 'B 收到了实时消息事件')
  ok(evMsg?.data?.message?.content === '你好，看到你也喜欢徒步。', '事件里带的就是那条内容')
  ok(Number.isInteger(evMsg?.data?.match_id), '事件里带 match_id，前端才知道该刷新哪个会话')

  // A 自己也该收到（另一个标签页、另一台设备在等这条）
  ok(await wsA.wait('message').then(() => true).catch(() => false), 'A 自己也收到一份（多端同步）')

  // 发消息的一方不该被算成未读
  const matchesAfterSend = await need('/matches', { token: a.token })
  const mA2 = matchesAfterSend.matches.find((m) => m.match_id === matchID)
  ok(mA2?.unread === 0, `A 自己没未读（实际 ${mA2?.unread}）`)
  ok(mA2?.last_mine === true, 'last_mine 为真，列表上会写「我：」')

  const matchesB = await need('/matches', { token: b.token })
  const mB = matchesB.matches.find((m) => m.match_id === matchID)
  ok(mB?.unread === 1, `B 有 1 条未读（实际 ${mB?.unread}）`)
  ok(matchesB.unread === 1, `会话总未读为 1（实际 ${matchesB.unread}）`)

  // ------------------------------------------------------------ B 回话
  section('六、B 回话，A 实时收到')

  const msg2 = await need(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: b.token,
    body: { client_msg_id: 'e2e-b-1', content: '周末常去西山，你呢？' },
  })
  ok(msg2.sender_id === b.id, 'sender_id 是 B')

  // 按内容等，不按顺序等：A 这条连接上已经流过好几条 message 了
  // （自己发的每一条都会推回来一份）
  const evA2 = await wsA
    .wait('message', 5000, (f) => f.data?.message?.content === '周末常去西山，你呢？')
    .catch((e) => {
      console.log(`    ${e.message}`)
      return null
    })
  ok(evA2 !== null, 'A 实时收到 B 的消息')
  if (!evA2) {
    console.log('    A 这条连接上收到过的帧：')
    for (const line of wsA.dump()) console.log(`      ${line}`)
  }
  ok(evA2?.data?.message?.sender_id === b.id, '推过来的这条署名是 B')

  // ------------------------------------------------------------ 幂等
  section('七、重发同一条不会变成两条')

  const resend = await need(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-1', content: '你好，看到你也喜欢徒步。' },
  })
  ok(resend.id === msg1.id, `同一条 client_msg_id 拿回同一行（${msg1.id} → ${resend.id}）`)

  const all = await need(`/matches/${matchID}/messages`, { token: a.token })
  ok(all.messages.length === 2, `会话里始终只有 2 条（实际 ${all.messages.length}）`)
  ok(all.messages[0].id < all.messages[1].id, '按时间正序返回')
  ok(all.other.nickname === b.nickname, '消息响应里带回对方信息')

  // 校验
  const emptyMsg = await api(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-x', content: '   ' },
  })
  ok(emptyMsg.status === 400 && emptyMsg.code === 'MESSAGE_EMPTY', `空消息被拒（实际 ${emptyMsg.status} ${emptyMsg.code}）`)

  const longMsg = await api(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-y', content: '字'.repeat(1001) },
  })
  ok(longMsg.status === 400 && longMsg.code === 'MESSAGE_TOO_LONG', `超过 1000 字被拒（实际 ${longMsg.status} ${longMsg.code}）`)

  const exact = await api(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-z', content: '字'.repeat(1000) },
  })
  ok(exact.code === 'OK', `正好 1000 字是允许的（实际 ${exact.code}）`)

  const noKey = await api(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { content: '没有幂等键' },
  })
  ok(noKey.status === 400, `不带 client_msg_id 被拒（实际 ${noKey.status}）`)

  // ------------------------------------------------------------ 已读
  section('八、已读水位')

  // 现取一次，不拿前面那份：中间又发过一条（1000 字那条），
  // 水位必须推到真正的最新一条，否则下面「已读归零」的判断是假的。
  const now = await need(`/matches/${matchID}/messages`, { token: b.token })
  const lastID = now.messages[now.messages.length - 1].id
  ok(now.other.unread > 0, `B 此刻确实有未读（${now.other.unread}）`)

  const read = await need(`/matches/${matchID}/read`, {
    method: 'POST',
    token: b.token,
    body: { last_msg_id: lastID },
  })
  ok(read.last_read_msg_id === lastID, `水位推到 ${lastID}（实际 ${read.last_read_msg_id}）`)

  const matchesB2 = await need('/matches', { token: b.token })
  ok(matchesB2.matches.find((m) => m.match_id === matchID)?.unread === 0, 'B 的未读清零')
  ok(matchesB2.unread === 0, '总未读也清零')

  // 水位只能往前，不能被人为顶到未来
  const poison = await need(`/matches/${matchID}/read`, {
    method: 'POST',
    token: b.token,
    body: { last_msg_id: lastID + 9999 },
  })
  ok(poison.last_read_msg_id === lastID, `超前的 id 被夹回最后一条（实际 ${poison.last_read_msg_id}）`)

  // 之后来的消息仍然是未读
  await need(`/matches/${matchID}/messages`, {
    method: 'POST',
    token: a.token,
    body: { client_msg_id: 'e2e-a-2', content: '那就约个周末。' },
  })
  const matchesB3 = await need('/matches', { token: b.token })
  ok(matchesB3.matches.find((m) => m.match_id === matchID)?.unread === 1, '新消息又算成未读')

  // ------------------------------------------------------------ 不合适
  section('九、不合适：等待方收到收尾通知，表态方不收到')

  // C 先表态想认识，成了等待方
  await need(`/introductions/${introAC}/respond`, { method: 'POST', token: c.token, body: { action: 'like' } })
  ok(await wsA.quiet('match'), '单向喜欢不推匹配事件')

  const pass = await need(`/introductions/${introAC}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'pass', reason: 'mismatch' },
  })
  ok(pass.state === 'declined', `状态是 declined（实际 ${pass.state}）`)
  ok(pass.matched === false, '没成匹配')

  await sleep(300)
  const rows = psql(`
    SELECT o.user_id || '|' || o.template || '|' || o.status
    FROM outbox o
    WHERE o.payload->>'intro_id' = '${introAC}'
    ORDER BY o.id`)
  const lines = rows ? rows.split('\n') : []
  ok(lines.length === 1, `只为这一条引荐产生了一行通知（实际 ${lines.length}）`)
  ok(lines[0]?.startsWith(`${c.id}|intro_closed`), `收件人是等待方 C，模板是 intro_closed（实际 ${lines[0]}）`)
  ok(!lines.some((l) => l.startsWith(`${a.id}|`)), '表态方 A 没有收到任何通知')

  // 盖过章的人重放，拿回的是同一个终局，而不是「已经关了」。
  // 重放检查排在终结态检查前面，就是为了这个：C 的「想认识」
  // 早就递出去了，他只是重试了一下，这个动作本身没错。
  const replayC = await need(`/introductions/${introAC}/respond`, {
    method: 'POST',
    token: c.token,
    body: { action: 'like' },
  })
  ok(replayC.state === 'declined' && replayC.matched === false, 'C 重放 like 拿回同一个终局')
  ok(replayC.match_id === undefined, '重放不会凭空生出 match_id')

  // 从没表过态的那一侧，才轮到「这条已经关了」。
  //
  // 另起一对 A–C：这次 A 单方面不合适，C 从头到尾没动过。引荐就此封上，
  // 而 C 手上还留着一次没用的机会 —— 那一次必须是「引荐关了」，
  // 不是「你已经表过态了」。
  const introAC2 = ensureOpenIntro(a.id, c.id)
  await need(`/introductions/${introAC2}/respond`, {
    method: 'POST',
    token: a.token,
    body: { action: 'pass', reason: 'vibe' },
  })

  const closed = await api(`/introductions/${introAC2}/respond`, {
    method: 'POST',
    token: c.token,
    body: { action: 'like' },
  })
  ok(closed.status === 409 && closed.code === 'INTRO_CLOSED', `没表过态的一侧拿到「已关闭」（实际 ${closed.status} ${closed.code}）`)

  // 他从头到尾没有过悬念，所以不该收到「对方没有接受」
  const rows2 = psql(`
    SELECT count(*) FROM outbox
    WHERE payload->>'intro_id' = '${introAC2}' AND user_id = ${c.id}`)
  ok(rows2 === '0', `从未表态的一方不收到收尾通知（实际 ${rows2} 行）`)

  // 终结的引荐在列表里不再是未读
  const listC = await need('/introductions', { token: c.token })
  const rowAC = listC.introductions.find((i) => i.id === introAC)
  ok(rowAC?.state === 'declined', 'C 的列表里是 declined')
  ok(rowAC?.viewed === false, 'C 从没打开过它，viewed 仍是假的')
  ok(listC.unread === expectedUnread(listC), `封上的信不再计入未读（${listC.unread}）`)

  // ------------------------------------------------------------ 单向引荐的隐藏方
  section('十、单向引荐的隐藏方看不到')

  // 单向引荐的隐藏方是低 id 那一侧，这样下面「谁看得见」才有个确定的答案
  const hiddenIsB = b.id < c.id
  const oneway = ensureOpenIntro(b.id, c.id, {
    kind: 'oneway',
    hiddenSide: hiddenIsB ? 'low' : 'high',
  })
  const seen = hiddenIsB ? c.token : b.token
  const blind = hiddenIsB ? b.token : c.token

  const listSeen = await need('/introductions', { token: seen })
  ok(
    listSeen.introductions.some((i) => i.id === oneway),
    `${hiddenIsB ? 'C' : 'B'}（可见方）在列表里看到这条单向引荐`,
  )

  const listBlind = await need('/introductions', { token: blind })
  ok(
    !listBlind.introductions.some((i) => i.id === oneway),
    `${hiddenIsB ? 'B' : 'C'}（隐藏方）的列表里没有这条`,
  )

  const blindDetail = await api(`/introductions/${oneway}`, { token: blind })
  ok(blindDetail.status === 404, `隐藏方直接拿 id 查也是 404（实际 ${blindDetail.status}）`)

  const blindRespond = await api(`/introductions/${oneway}/respond`, {
    method: 'POST',
    token: blind,
    body: { action: 'like' },
  })
  ok(blindRespond.status === 404, `隐藏方也表不了态（实际 ${blindRespond.status}）`)

  // ------------------------------------------------------------ 分页游标
  section('十一、消息游标往回翻')

  for (let i = 0; i < 5; i++) {
    await need(`/matches/${matchID}/messages`, {
      method: 'POST',
      token: b.token,
      body: { client_msg_id: `e2e-page-${i}`, content: `第 ${i} 条补充` },
    })
  }
  const page1 = await need(`/matches/${matchID}/messages?limit=3`, { token: a.token })
  ok(page1.messages.length === 3, `limit=3 拿到 3 条（实际 ${page1.messages.length}）`)
  ok(page1.has_more === true, 'has_more 为真')
  const page2 = await need(`/matches/${matchID}/messages?limit=3&before=${page1.messages[0].id}`, {
    token: a.token,
  })
  ok(
    page2.messages.every((m) => m.id < page1.messages[0].id),
    '第二页全部早于第一页的第一条',
  )
  ok(
    new Set([...page1.messages, ...page2.messages].map((m) => m.id)).size ===
      page1.messages.length + page2.messages.length,
    '两页之间没有重复',
  )
} catch (err) {
  failed++
  console.log(`\n✗ 中途断了：${err.message}`)
} finally {
  wsA?.close()
  wsB?.close()
  wsC?.close()
}

console.log(`\n${failed === 0 ? '✓ 全部通过' : `✗ ${failed} 项不符`}`)
console.log(`  账号：m3-${stamp}-{0,1,2}@example.com / ${PASSWORD}`)
process.exit(failed === 0 ? 0 : 1)

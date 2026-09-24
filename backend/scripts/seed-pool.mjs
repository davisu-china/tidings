/**
 * 造一批可用于匹配的测试账号，用来验收 M2 的引荐生成。
 *
 * 走的是真 HTTP 接口，不是直接写库：注册 → 建档 → 头像 + 3 张照片
 * → 偏好。这样这批账号的 status 是被产品自己的判定翻成 active 的，
 * 「能被引荐」这件事就不是脚本替它声明的。
 *
 * 为什么值得做成脚本而不是手工点：验收标准是「造 20 个测试账号」，
 * 而手工点 20 遍必然会漏掉某一步（多半是第 3 张照片），
 * 然后拿到一个「一个引荐都没生成」的结果，还以为是匹配引擎坏了。
 *
 * 前置：本地栈起着（make up），api 在 8081。
 * 跑法：node backend/scripts/seed-pool.mjs [数量]
 *   SEED_BASE   默认 http://localhost:8081
 *   SEED_CITY   默认 110000（北京）
 *   SEED_PREFIX 邮箱前缀，默认 pool
 *
 * 头像用后端自带的人脸 fixture，相册由它缩放而来（macOS 的 sips）——
 * 人脸检测是入池的硬条件，随便找张图会被 pigo 挡下来。
 */
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = resolve(HERE, '../..')

const BASE = process.env.SEED_BASE ?? 'http://localhost:8081'
const CITY = Number(process.env.SEED_CITY ?? 110000)
const PREFIX = process.env.SEED_PREFIX ?? 'pool'
const COUNT = Number(process.argv[2] ?? 20)

const OUT = '/tmp/tidings-seed'
const FACE = resolve(REPO, 'backend/internal/pkg/facedetect/testdata/sample.jpg')
const PHOTOS = [0, 1, 2].map((i) => resolve(OUT, `p${i}.jpg`))

mkdirSync(OUT, { recursive: true })
if (!existsSync(PHOTOS[0])) {
  // sips 是 macOS 自带的，这个脚本和 e2e-onboarding.mjs 一样只在 macOS 上跑
  ;[880, 760, 1000].forEach((w, i) => {
    execFileSync('sips', ['-Z', String(w), FACE, '--out', PHOTOS[i]], { stdio: 'ignore' })
  })
}
const FACE_BYTES = readFileSync(FACE)
const PHOTO_BYTES = PHOTOS.map((p) => readFileSync(p))

// 昵称：两两组合，保证 2–12 个字、互不重复、不含任何联系方式。
// 用真名风格的词而不是「测试用户01」—— 昵称校验会拦数字，
// 而且引荐卡上要显示它，假名字会让卡看起来像坏掉的。
const SURNAMES = '林周陈许沈顾苏谢钟傅柯邵'.split('')
const GIVEN = ['砚', '迟', '屿', '青野', '南亭', '知遥', '亦白', '和风', '秋叙', '素笺', '长风', '明川']

const OCCUPATIONS = ['产品经理', '后端工程师', '中学教师', '外科医生', '建筑师', '编辑', '律师', '插画师', '数据分析师', '翻译']
const HOBBIES = ['徒步', '摄影', '做饭', '羽毛球', '看展', '骑行', '养猫', '爬山', '古典乐', '下厨', '潜水', '写字']
const INTROS = [
  '喜欢摄影和徒步，周末多半在外面。工作日反而安静，下班回家做饭。',
  '做菜是认真的爱好，不是凑合吃饭。周末常去菜市场，也常约朋友来家里。',
  '话不多，但聊到喜欢的事会停不下来。最近在读非虚构。',
  '爱运动，每周三次。也爱安静地待着，不冲突。',
  '南方人，来了几年。喜欢这座城市的秋天。',
  '工作忙，但会留出周日下午完全不安排。',
  '喜欢看展和逛旧书店，一个人也能逛一下午。',
  '养了一只猫。作息规律，早睡早起。',
]

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** 统一的请求封装：非 2xx 或信封 code 不是 OK 都当失败，直接把响应体抛出来。 */
async function api(path, { method = 'GET', token, body, raw } = {}) {
  const headers = {}
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const res = await fetch(`${BASE}/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  if (!res.ok) throw new Error(`${method} ${path} → ${res.status} ${text.slice(0, 200)}`)

  const json = JSON.parse(text)
  if (raw) return json
  if (json.code !== 'OK') throw new Error(`${method} ${path} → ${json.code} ${json.message}`)
  return json.data
}

/** 直传：拿票据 → PUT 到 MinIO。图片字节不经过 API。 */
async function upload(token, bytes) {
  const ticket = await api('/media/upload-url', { method: 'POST', token, body: { ext: '.jpg' } })
  const put = await fetch(ticket.upload_url, {
    method: 'PUT',
    headers: { 'Content-Type': 'image/jpeg' },
    body: bytes,
  })
  if (!put.ok) throw new Error(`直传失败 ${put.status} ${await put.text()}`)
  return ticket.object_key
}

function profileFor(i) {
  const male = i % 2 === 0
  return {
    nickname: SURNAMES[i % SURNAMES.length] + GIVEN[Math.floor(i / SURNAMES.length) % GIVEN.length],
    gender: male ? 'M' : 'F',
    // 1990–1999，同龄区间，避免年龄偏好把人对掉
    birth_ym: 199000 + (i % 10) * 100 + ((i * 7) % 12) + 1,
    city_code: CITY,
    height_cm: male ? 170 + (i % 12) : 158 + (i % 12),
    education_level: (i % 4) + 1,
    hometown_code: [110000, 310000, 440100, 510100][i % 4],
    weight_kg: male ? 62 + (i % 15) : 48 + (i % 12),
    school_name: '',
    occupation: OCCUPATIONS[i % OCCUPATIONS.length],
    company: '',
    income_band: (i % 6) + 1,
    chronotype: (i % 3) + 1,
    smoking: [0, 0, 1, 0][i % 4],
    drinking: (i % 2),
    hobbies: [HOBBIES[i % HOBBIES.length], HOBBIES[(i + 3) % HOBBIES.length]].join('、'),
    intro: INTROS[i % INTROS.length],
    expectation: '',
    // 婚育与婚史：三项里「想要」与「不要」撞上就淘汰（§12.1）。
    // 测试池里全填「再说」，配合下面偏好侧的 want_child 留空，
    // 让硬条件不成为这一轮生不出引荐的原因。
    want_child: 3,
    marital_status: 1,
  }
}

/** 偏好：年龄与城市给足区间，软偏好三项全留空。 */
function preferenceFor() {
  return {
    age_min: 22,
    age_max: 45,
    city_codes: [],
    want_child: null,
    // 1 = 接受有婚史。留空的话也放行（未填按不限），
    // 显式填 1 是为了让偏好这一行有内容，好确认它真的被读到了。
    accept_divorced: 1,
    accept_remote: 1,
    edu_min: null,
    height_min: null,
    height_max: null,
    income_min: null,
    income_max: null,
  }
}

async function createOne(i, stamp) {
  const email = `${PREFIX}-${stamp}-${String(i).padStart(2, '0')}@example.com`
  const password = 'tidings-seed-2026'

  const auth = await api('/auth/register', { method: 'POST', body: { email, password } })
  const token = auth.access_token

  const p = profileFor(i)
  await api('/me/profile', { method: 'PATCH', token, body: p })

  const avatarKey = await upload(token, FACE_BYTES)
  await api('/me/avatar', { method: 'PUT', token, body: { object_key: avatarKey } })

  for (const bytes of PHOTO_BYTES) {
    const key = await upload(token, bytes)
    await api('/me/photos', { method: 'POST', token, body: { object_key: key } })
  }

  await api('/me/preferences', { method: 'PUT', token, body: preferenceFor() })

  // status 是后端在最后一张照片落库时翻的（applyProfileState / syncAfterMedia）。
  // 这里回读一次确认，而不是假设它翻了 —— 假设错了，
  // 后果是整池人都进不了候选集，而症状看起来像匹配引擎没跑。
  const me = await api('/me', { token })
  if (me.status !== 'active') {
    throw new Error(`${email} 建档后 status = ${me.status}，没进池（缺 ${JSON.stringify(me.missing_required ?? [])}）`)
  }
  return { email, nickname: p.nickname, gender: p.gender }
}

const stamp = Date.now().toString(36)
console.log(`造 ${COUNT} 个账号 → ${BASE}，城市 ${CITY}`)

const created = []
let failed = 0
for (let i = 0; i < COUNT; i++) {
  try {
    created.push(await createOne(i, stamp))
    process.stdout.write(`\r  ${created.length}/${COUNT}`)
  } catch (e) {
    failed++
    console.log(`\n  ✗ 第 ${i} 个失败：${e.message}`)
  }
  // 不打太急：注册走的是 bcrypt cost 12，图片要跑人脸检测和三档派生图
  await sleep(60)
}

const males = created.filter((u) => u.gender === 'M').length
console.log(`\n✓ 成功 ${created.length} 个（男 ${males} / 女 ${created.length - males}），失败 ${failed} 个`)
console.log(`  邮箱前缀：${PREFIX}-${stamp}-`)
if (created.length > 0) {
  console.log(`  密码统一：tidings-seed-2026`)
  console.log(`  例：${created[0].email} ${created[0].nickname}`)
}
process.exit(failed === 0 ? 0 : 1)

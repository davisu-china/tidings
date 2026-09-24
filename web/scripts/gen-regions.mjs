/**
 * 生成 web/src/lib/regions.data.ts —— 省市两级联动用的全国区划数据。
 *
 * 数据源是 npm 包 province-city-china（民政部 / 国家统计局的国标 GB/T 2260
 * 数据），钉死在 devDependencies 里。**生成物提交进仓库**，构建不依赖本脚本
 * —— 与后端的 schools.json 同一套做法，区别是这个脚本在仓库里，那个不在。
 *
 * 跑法：
 *   node scripts/gen-regions.mjs           写文件
 *   node scripts/gen-regions.mjs --check   只比对，有漂移退出码 1（给 CI 用）
 *
 * 只取两级（省 + 地级市）。区县那一级（dist/area.json）不取：建档页让人选到
 * 区县没有意义，而且体积会从 15 KB 涨到 200 KB。
 */
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const HERE = dirname(fileURLToPath(import.meta.url))
const OUT = resolve(HERE, '../src/lib/regions.data.ts')

const require = createRequire(import.meta.url)
const PKG_JSON = require.resolve('province-city-china/package.json')
const PKG_DIR = dirname(PKG_JSON)
const { version } = JSON.parse(readFileSync(PKG_JSON, 'utf8'))

const read = (f) => readFileSync(resolve(PKG_DIR, 'dist', f))
const sha = (buf) => createHash('sha256').update(buf).digest('hex').slice(0, 16)

const provinceRaw = read('province.json')
const cityRaw = read('city.json')
const provinces = JSON.parse(provinceRaw.toString('utf8'))
const cities = JSON.parse(cityRaw.toString('utf8'))

// ---------------------------------------------------------------- 归一化

/** 省名去掉行政级别后缀：黑龙江省 → 黑龙江，内蒙古自治区 → 内蒙古。 */
function shortProvince(name) {
  return name.replace(/特别行政区$/, '').replace(/(维吾尔|壮族|回族)?自治区$/, '').replace(/(省|市)$/, '')
}

/**
 * 市名去掉「市」「地区」后缀，保留「盟」与「自治州」——去掉之后
 * 「阿坝藏族羌族自治州」会歧义，「兴安盟」会变成一个不存在的词。
 *
 * 归一后若与某个省名相同则保留原后缀：吉林市去掉「市」就是「吉林」，
 * 与吉林省撞车，下拉里会出现两个「吉林」。
 */
function shortCity(name, provinceNames) {
  const stripped = name.replace(/(市|地区)$/, '')
  return provinceNames.has(stripped) ? name : stripped
}

/**
 * 四条不是城市的条目，上游名字里带着省名前缀和一个连字符，直接进下拉
 * 会是四道很显眼的丑：「河南省-省直辖县级行政区划」。
 *
 * 显式列举而不是写正则去猜：这四条是上游数据的脏法，不是一条规则。
 * 河南省 / 湖北省 / 海南省的是省直辖，新疆的是自治区直辖，改名后各自
 * 在自己省里仍然是唯一的。
 *
 * 代价（已知缺口）：仙桃、潜江、天门、济源、石河子这些县级市的用户
 * 选不到自己所在的那个市，只能选这一条或省会。
 */
const RENAME = {
  419000: '省直辖县级行政区划',
  429000: '省直辖县级行政区划',
  469000: '省直辖县级行政区划',
  659000: '自治区直辖县级行政区划',
}

const provinceNames = new Set(provinces.map((p) => shortProvince(p.name)))

/** 旧 dict.ts 里那份 45 条的 CITIES。任何一个码在新数据里查不到，
 *  老账号的城市名就会变成空串 —— 这里是那道机器保证。 */
const LEGACY_CITY_CODES = [
  110000, 310000, 440100, 440300, 330100, 320100, 510100, 420100, 610100, 500000, 120000, 320500,
  330200, 370200, 370100, 350200, 350100, 430100, 410100, 340100, 210100, 210200, 220100, 230100,
  130100, 140100, 360100, 450100, 460100, 520100, 530100, 620100, 650100, 150100, 640100, 630100,
  540100, 320200, 320600, 330300, 330400, 440400, 440600, 441900, 441300,
]

// ---------------------------------------------------------------- 组装

const byProvince = new Map()
for (const c of cities) {
  const key = String(c.province).padStart(2, '0')
  if (!byProvince.has(key)) byProvince.set(key, [])
  byProvince.get(key).push(c)
}

const out = provinces
  .map((p) => {
    const key = String(p.province).padStart(2, '0')
    const own = (byProvince.get(key) ?? []).sort((a, b) => a.code.localeCompare(b.code))
    const code = Number(p.code)
    const name = shortProvince(p.name)

    // 没有地级条目的省：四个直辖市 + 港澳台。让省自己当自己的市 ——
    // 否则这几个地方的用户在「市」那一级无事可做，而且老账号里
    // 310000 这种「省码即市码」的存量值会查不到名字。
    const list =
      own.length > 0
        ? own.map((c) => ({
            code: Number(c.code),
            name: RENAME[c.code] ?? shortCity(c.name, provinceNames),
          }))
        : [{ code, name }]

    return { code, name, cities: list }
  })
  .sort((a, b) => a.code - b.code)

// ---------------------------------------------------------------- 自检

const fail = (msg) => {
  console.error(`✗ ${msg}`)
  process.exit(1)
}

if (out.length !== 34) fail(`省级应有 34 条，得到 ${out.length}`)

const allCities = out.flatMap((p) => p.cities)
for (const p of out) {
  if (p.cities.length === 0) fail(`${p.name} 一个市都没有`)
  // 省内的市名必须唯一，否则下拉里会出现两条一模一样的选项
  const names = new Set(p.cities.map((c) => c.name))
  if (names.size !== p.cities.length) fail(`${p.name} 内有重名城市`)
  for (const c of p.cities) {
    if (c.code % 100 !== 0) fail(`${c.name} 的码 ${c.code} 不是地级码（末两位应为 00）`)
    if (String(c.code).length !== 6) fail(`${c.name} 的码 ${c.code} 不是 6 位`)
    if (c.name.includes('-')) fail(`${c.name} 里还带着连字符`)
  }
}

// 老账号的城市必须都能查到名字
const known = new Set(allCities.map((c) => c.code))
for (const code of LEGACY_CITY_CODES) {
  if (!known.has(code)) fail(`旧 CITIES 里的 ${code} 在新数据里查不到，老账号的城市名会变成空串`)
}

// 全国范围的重名只允许出现在那四条「省直辖县级行政区划」上 —— 它们各自
// 在自己省里唯一，跨省重名要靠 regions.ts 的 cityLabel 补省名区分。
const byName = new Map()
for (const c of allCities) byName.set(c.name, (byName.get(c.name) ?? 0) + 1)
const dupNames = [...byName].filter(([, n]) => n > 1).map(([n]) => n)
for (const n of dupNames) {
  if (!n.endsWith('直辖县级行政区划')) fail(`出现了预料之外的重名：${n}`)
}

// ---------------------------------------------------------------- 输出

const lines = out.map((p) => {
  const cs = p.cities.map((c) => `{ code: ${c.code}, name: ${JSON.stringify(c.name)} }`).join(', ')
  return `  { code: ${p.code}, name: ${JSON.stringify(p.name)}, cities: [${cs}] },`
})

const file = `/**
 * 全国省市两级区划数据（34 个省级 / ${allCities.length} 个市）。
 *
 * 本文件由 scripts/gen-regions.mjs 生成，**不要手改** —— 改这里会被下一次
 * 生成覆盖。要改数据改那个脚本，然后 \`npm run gen:regions\`。
 *
 * 来源：npm 包 province-city-china@${version}（国标 GB/T 2260，
 * 民政部 / 国家统计局）。源文件校验和：
 *   province.json ${sha(provinceRaw)}
 *   city.json     ${sha(cityRaw)}
 *
 * 两条处理规则（详见生成脚本）：
 *   1. 四个直辖市与港澳台在上游数据里没有地级条目，这里让省自己当自己的市，
 *      所以 310000 既是省码也是市码。
 *   2. 市名去掉了「市」「地区」后缀，保留「盟」「自治州」；吉林市例外
 *      （去掉会与吉林省撞名）。
 */

export interface RegionCity {
  readonly code: number
  readonly name: string
}

export interface RegionProvince {
  readonly code: number
  readonly name: string
  readonly cities: readonly RegionCity[]
}

export const PROVINCES: readonly RegionProvince[] = [
${lines.join('\n')}
]
`

if (process.argv.includes('--check')) {
  let current = ''
  try {
    current = readFileSync(OUT, 'utf8')
  } catch {
    fail('regions.data.ts 不存在，先跑一次 node scripts/gen-regions.mjs')
  }
  if (current !== file) {
    console.error('✗ regions.data.ts 与数据源不一致，跑一次 `npm run gen:regions`')
    process.exit(1)
  }
  console.log('✓ regions.data.ts 与 province-city-china@' + version + ' 一致')
  process.exit(0)
}

writeFileSync(OUT, file)
console.log(
  `✓ 生成 regions.data.ts：${out.length} 个省级 / ${allCities.length} 个市，` +
    `${(Buffer.byteLength(file) / 1024).toFixed(1)} KB`,
)

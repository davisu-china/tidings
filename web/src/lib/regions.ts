/**
 * 省市两级区划的查询层。数据在 regions.data.ts（生成物），这里只放函数。
 *
 * 分成两个文件是因为数据是生成的：查询函数写在生成物里，下一次
 * `npm run gen:regions` 会把它们静默删掉。
 *
 * 代码口径与后端一致：6 位国标码，`code / 10000` 是省级前缀 ——
 * repo/matching.go 里按同城筛选用的就是这个除法。
 */
import { PROVINCES, type RegionCity, type RegionProvince } from './regions.data'

export { PROVINCES }
export type { RegionCity, RegionProvince }

interface CityHit {
  city: RegionCity
  province: RegionProvince
}

/**
 * 反查索引从 PROVINCES 展开，与下拉用的是同一份数据 —— 所以「直辖市自己
 * 当自己的市」这条规则在反查里自动成立（310000 既是省码也是市码），
 * 不需要对直辖市做特判，也就不会出现「下拉里有上海、卡片上城市是空的」
 * 这种只错一半的状态。
 */
const CITY_INDEX = new Map<number, CityHit>()
const PROVINCE_INDEX = new Map<number, RegionProvince>()
for (const p of PROVINCES) {
  PROVINCE_INDEX.set(p.code, p)
  for (const c of p.cities) CITY_INDEX.set(c.code, { city: c, province: p })
}

/** 全国范围内重名的市（只有四条「省直辖县级行政区划」）。 */
const AMBIGUOUS = new Set<string>()
{
  const seen = new Set<string>()
  for (const hit of CITY_INDEX.values()) {
    if (seen.has(hit.city.name)) AMBIGUOUS.add(hit.city.name)
    seen.add(hit.city.name)
  }
}

/** 省级前缀（两位）：330100 → 33。与后端 candidateSQL 的同省判断同一口径。 */
export function provinceNo(code: number): number {
  return Math.floor(code / 10000)
}

/** 市码 → 省码：330100 → 330000。查不到时按前缀推。 */
export function provinceOfCode(code: number): number {
  return CITY_INDEX.get(code)?.province.code ?? provinceNo(code) * 10000
}

/** 省名（去掉行政级别后缀）：330000 → 浙江。 */
export function provinceName(code: number | null | undefined): string {
  if (code === null || code === undefined) return ''
  return PROVINCE_INDEX.get(code)?.name ?? ''
}

/**
 * 市名。查不到返回空串 —— 与 dict.ts 里那份 45 条的旧实现行为一致，
 * 调用点（引荐卡、/me 摘要）不必改成判 undefined。
 */
export function cityName(code: number | null | undefined): string {
  if (code === null || code === undefined) return ''
  return CITY_INDEX.get(code)?.city.name ?? ''
}

/**
 * 需要单独显示时用的市名。全国重名的那四条（河南/湖北/海南的
 * 「省直辖县级行政区划」）补上省名，否则偏好页的 chips 里会出现两条
 * 一模一样的城市，用户根本分不清删的是哪一个。
 */
export function cityLabel(code: number | null | undefined): string {
  if (code === null || code === undefined) return ''
  const hit = CITY_INDEX.get(code)
  if (!hit) return ''
  return AMBIGUOUS.has(hit.city.name) ? `${hit.province.name} · ${hit.city.name}` : hit.city.name
}

/** 该省下辖的市。省码不认识时给空数组，调用点不必判 undefined。 */
export function citiesOf(provinceCode: number | null | undefined): readonly RegionCity[] {
  if (provinceCode === null || provinceCode === undefined) return []
  return PROVINCE_INDEX.get(provinceCode)?.cities ?? []
}

/** 省下拉的选项。34 条，顺序即数据里的顺序。 */
export const PROVINCE_OPTIONS = PROVINCES.map((p) => ({ value: p.code, label: p.name }))

/** 市下拉的选项。省没选时是空数组 —— 那正是「市」应当禁用的时候。 */
export function cityOptions(provinceCode: number | null | undefined) {
  return citiesOf(provinceCode).map((c) => ({ value: c.code, label: c.name }))
}

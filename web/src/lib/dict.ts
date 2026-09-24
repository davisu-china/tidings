// 枚举字典。取值与完整版 PRD 5.4 的字段字典一致，改动要连着那份字典一起改。
// 后端只做数值范围校验（比如 education_level 1–4），label 由前端给。

export const GENDERS = [
  { value: 'F', label: '女' },
  { value: 'M', label: '男' },
] as const

export const EDUCATION_LEVELS = [
  { value: 1, label: '大专及以下' },
  { value: 2, label: '本科' },
  { value: 3, label: '硕士' },
  { value: 4, label: '博士' },
] as const

/** 只出区间、不出具体数字 —— 这是决策 11 明确要展示的字段。 */
export const INCOME_BANDS = [
  { value: 1, label: '10 万以下' },
  { value: 2, label: '10–20 万' },
  { value: 3, label: '20–35 万' },
  { value: 4, label: '35–50 万' },
  { value: 5, label: '50–100 万' },
  { value: 6, label: '100 万以上' },
] as const

export const CHRONOTYPES = [
  { value: 1, label: '早睡早起' },
  { value: 2, label: '夜猫子' },
  { value: 3, label: '不规律' },
] as const

/**
 * 「不合适」的原因（决策 07：三选一必填，不要求输入文字）。
 *
 * value 与后端 model 里的 code 一一对应，存的是 code 不是中文 ——
 * 「感觉不合适」这类说法早晚要改，而这一列是要拿来统计的。
 */
export const PASS_REASONS = [
  { value: 'mismatch', label: '条件不符' },
  { value: 'vibe', label: '感觉不合适' },
  { value: 'other', label: '其他' },
] as const

/** 吸烟与饮酒共用同一套取值（0 不 / 1 偶尔 / 2 经常）。 */
export const FREQUENCIES = [
  { value: 0, label: '不' },
  { value: 1, label: '偶尔' },
  { value: 2, label: '经常' },
] as const

/**
 * 婚育意愿。三项而不是两项 ——「再说」是真实存在的一种态度，
 * 把它归进「不要」或「想要」都是替用户表态，而这一项是硬条件：
 * 归错了会直接决定他被推给谁。只有「想要」撞上「不要」才淘汰。
 */
export const WANT_CHILDREN = [
  { value: 1, label: '想要孩子' },
  { value: 2, label: '不要孩子' },
  { value: 3, label: '再说' },
] as const

/**
 * 婚史。与偏好侧的「是否接受有婚史」是两回事：
 * 这一项说的是「我自己的情况」，那一项说的是「我要求对方怎样」，
 * 匹配时后端把两边对着比。所以文案要写成陈述句，不能写成选择。
 */
export const MARITAL_STATUSES = [
  { value: 1, label: '未婚' },
  { value: 2, label: '离异' },
] as const

/**
 * 婚史接受度（偏好侧）。
 *
 * 「不接受」是 0 而不是缺省 —— 后端把 NULL 当「不限」，
 * 而这是一个明确的表态。界面上是个开关，所以只有两项。
 */
export const DIVORCED_ACCEPTANCE = [
  { value: 1, label: '接受' },
  { value: 0, label: '不接受' },
] as const

/**
 * 异地接受度（偏好侧）。
 *
 * 与婚史同理，只在偏好侧存一份：异地接受度是「我对关系形态的要求」，
 * 不是「我未婚」那类关于自己的事实。同城时不参与判定。
 */
export const REMOTE_ACCEPTANCE = [
  { value: 1, label: '接受' },
  { value: 2, label: '不接受' },
] as const

/** 静默时段的钟点文案，`22` → `22:00`。 */
export function hourLabel(h: number | null | undefined): string {
  if (h === null || h === undefined) return ''
  return `${String(h).padStart(2, '0')}:00`
}
/**
 * 城市列表。用的是国标 GB/T 2260 的 6 位代码，但只收了直辖市、
 * 省会与主要城市 —— 完整列表四百多条，建档向导不该让人滚到底。
 * 后端只校验 110000–659999 这个范围，所以补全列表不需要改后端。
 */
export const CITIES = [
  { code: 110000, name: '北京' },
  { code: 310000, name: '上海' },
  { code: 440100, name: '广州' },
  { code: 440300, name: '深圳' },
  { code: 330100, name: '杭州' },
  { code: 320100, name: '南京' },
  { code: 510100, name: '成都' },
  { code: 420100, name: '武汉' },
  { code: 610100, name: '西安' },
  { code: 500000, name: '重庆' },
  { code: 120000, name: '天津' },
  { code: 320500, name: '苏州' },
  { code: 330200, name: '宁波' },
  { code: 370200, name: '青岛' },
  { code: 370100, name: '济南' },
  { code: 350200, name: '厦门' },
  { code: 350100, name: '福州' },
  { code: 430100, name: '长沙' },
  { code: 410100, name: '郑州' },
  { code: 340100, name: '合肥' },
  { code: 210100, name: '沈阳' },
  { code: 210200, name: '大连' },
  { code: 220100, name: '长春' },
  { code: 230100, name: '哈尔滨' },
  { code: 130100, name: '石家庄' },
  { code: 140100, name: '太原' },
  { code: 360100, name: '南昌' },
  { code: 450100, name: '南宁' },
  { code: 460100, name: '海口' },
  { code: 520100, name: '贵阳' },
  { code: 530100, name: '昆明' },
  { code: 620100, name: '兰州' },
  { code: 650100, name: '乌鲁木齐' },
  { code: 150100, name: '呼和浩特' },
  { code: 640100, name: '银川' },
  { code: 630100, name: '西宁' },
  { code: 540100, name: '拉萨' },
  { code: 320200, name: '无锡' },
  { code: 320600, name: '南通' },
  { code: 330300, name: '温州' },
  { code: 330400, name: '嘉兴' },
  { code: 440400, name: '珠海' },
  { code: 440600, name: '佛山' },
  { code: 441900, name: '东莞' },
  { code: 441300, name: '惠州' },
] as const

const cityMap = new Map<number, string>(CITIES.map((c) => [c.code, c.name]))

export function cityName(code: number | null | undefined): string {
  if (code === null || code === undefined) return ''
  return cityMap.get(code) ?? ''
}

function labelOf(
  list: readonly { value: number | string; label: string }[],
  value: number | string | null | undefined,
): string {
  if (value === null || value === undefined) return ''
  return list.find((i) => i.value === value)?.label ?? ''
}

export const genderLabel = (v: string | null | undefined) => labelOf(GENDERS, v)
export const educationLabel = (v: number | null | undefined) => labelOf(EDUCATION_LEVELS, v)
export const incomeLabel = (v: number | null | undefined) => labelOf(INCOME_BANDS, v)
export const chronotypeLabel = (v: number | null | undefined) => labelOf(CHRONOTYPES, v)
export const frequencyLabel = (v: number | null | undefined) => labelOf(FREQUENCIES, v)
export const wantChildLabel = (v: number | null | undefined) => labelOf(WANT_CHILDREN, v)
export const maritalLabel = (v: number | null | undefined) => labelOf(MARITAL_STATUSES, v)

/**
 * 缺失项标识 → 人话。首页空状态要写「还差：单位、收入、作息」，
 * 直接把后端的字段名摊给用户看是不合格的。
 */
export const MISSING_LABELS: Record<string, string> = {
  nickname: '昵称',
  gender: '性别',
  birth_ym: '出生年月',
  city_code: '城市',
  height_cm: '身高',
  education_level: '学历',
  avatar_key: '头像',
  photos: '照片（至少 3 张）',
}

export function missingLabels(fields: readonly string[]): string[] {
  return fields.map((f) => MISSING_LABELS[f] ?? f)
}

/** 从 YYYYMM 推到今天的周岁。后端也算了一份，这里只用于界面即时回显。 */
export function ageFromBirthYM(ym: number | null | undefined): number | null {
  if (!ym) return null
  const year = Math.floor(ym / 100)
  const month = ym % 100
  if (year < 1900 || year > 2999 || month < 1 || month > 12) return null

  const now = new Date()
  let age = now.getFullYear() - year
  // 生日当月即算长一岁，与后端 ageFromBirthYM 保持一致
  if (now.getMonth() + 1 < month) age -= 1
  return age
}

export function formatBirthYM(ym: number | null | undefined): string {
  if (!ym) return ''
  const year = Math.floor(ym / 100)
  const month = ym % 100
  return `${year} 年 ${month} 月`
}

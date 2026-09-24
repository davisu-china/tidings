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
// 城市（省市两级区划）不在这里 —— 它是一份 344 条的生成数据，
// 见 lib/regions.ts 与 scripts/gen-regions.mjs。这里只留字段字典。

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
export const frequencyLabel = (v: number | null | undefined) => labelOf(FREQUENCIES, v)
export const wantChildLabel = (v: number | null | undefined) => labelOf(WANT_CHILDREN, v)
export const maritalLabel = (v: number | null | undefined) => labelOf(MARITAL_STATUSES, v)

/**
 * 缺失项标识 → 人话。直接把后端的字段名摊给用户看是不合格的。
 *
 * 现在还用在两个地方：建档向导里「下一步按不动」时点明缺的是什么，
 * 以及「我的」页判断要不要给「去补全」。曾经那张全量缺项清单已经不写了。
 */
export const MISSING_LABELS: Record<string, string> = {
  nickname: '昵称',
  gender: '性别',
  // 字段在界面上叫「出生日期」（年月日一起填），不是「出生年月」
  birth_ym: '出生日期',
  city_code: '城市',
  height_cm: '身高',
  weight_kg: '体重',
  education_level: '学历',
  school_name: '毕业院校',
  avatar_key: '头像',
  // 不带张数：改完之后这条只在 0 张时出现，写「至少 3 张」会是假话。
  photos: '照片',
  // v1.7 起「补充」那 11 项也是必填，标签跟界面上的一致：
  // 控件叫「关于孩子」而不是「婚育意愿」，「工作单位」而不是「公司」。
  hometown_code: '家乡',
  occupation: '职业',
  company: '工作单位',
  income_band: '年收入',
  smoking: '吸烟',
  drinking: '饮酒',
  hobbies: '兴趣',
  intro: '自我介绍',
  expectation: '对另一半的期待',
  want_child: '关于孩子',
  marital_status: '婚史',
}

export function missingLabels(fields: readonly string[]): string[] {
  return fields.map((f) => MISSING_LABELS[f] ?? f)
}

/**
 * 入池要求的最少照片数。对应 backend/internal/service/profile.go 的
 * minPhotosForActive，改一处必须改另一处（同 daysInMonth 的做法）。
 *
 * **只准出现在文案里。** 判「够没够入池」一律看服务端的 missing_required ——
 * 那是唯一的事实来源，前端自己判一遍就有了第二份真相，两边总有一次会对不上。
 * onboarding.tsx 的下一步门禁就是这么做的，别改它。
 */
export const MIN_PHOTOS = 1

/**
 * 建议张数。后端没有这个数，也不参与任何判定 —— 它是纯粹的文案目标：
 * 传够 PHOTO_GOAL 之后就不再提照片的事，有终点才不像催命。
 */
export const PHOTO_GOAL = 3

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

/**
 * 某年某月有几天。与后端 `daysInMonth` 是同一套规则，两处必须一起改 ——
 * 前端少算一天（比如闰年）会让日历上 2 月 29 日直接消失，而后端认它。
 *
 * 用 `new Date(y, m, 0).getDate()` 也能算，但那要把年份塞进 Date：
 * 1900 年附近、`Date` 的两位年份补全历史都会来插一脚。查表更短也更好读。
 */
export function daysInMonth(year: number, month: number): number {
  if (month === 2) {
    const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
    return leap ? 29 : 28
  }
  return month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31
}

/**
 * 出生年可选范围。后端把年龄卡在 18–60 岁（未成年人不得进入这个产品），
 * 所以年份是「今年往前 18 到 60 年」，与 profile.go 的 checkBirthYM 同一条边界。
 *
 * 取 now 参数而不是在模块顶层算一次：SPA 会话可以横跨跨年，import 时求值
 * 会让 18 岁那条边界一直停在打开页面那一天。
 */
export function birthYearRange(now: Date = new Date()): { min: number; max: number } {
  return { min: now.getFullYear() - 60, max: now.getFullYear() - 18 }
}

/**
 * 该年份最多能选到几月。只有 18 岁那一年会被当前月份截断 ——
 * 2008 年 10 月生在 2026 年 9 月还是 17 岁，服务端 checkBirthYM 会直接拒。
 *
 * 另一端不用截：出生月在当前月之后只会**减**一岁，所以 1966 年 12 月
 * 是 59 岁不是 61 岁，最早那一年照样是完整的 12 个月。
 */
export function monthsInYear(year: number, now: Date = new Date()): number {
  return year === now.getFullYear() - 18 ? now.getMonth() + 1 : 12
}

/**
 * 出生日期的人话。day 为 null 时只说到月 —— 老档案只知道年月，
 * 这里不该替它编一个日子出来。
 */
export function formatBirthDate(
  ym: number | null | undefined,
  day: number | null | undefined,
): string {
  if (!ym) return ''
  const year = Math.floor(ym / 100)
  const month = ym % 100
  if (!day) return `${year} 年 ${month} 月`
  return `${year} 年 ${month} 月 ${day} 日`
}

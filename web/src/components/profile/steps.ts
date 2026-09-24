import type { FieldKey } from './schema'

/**
 * 建档的五个步骤。
 *
 * **五步全是必填**（v1.7 起）。第五步原先整步是选填，用户走到第四步停下
 * 就已经可被引荐了 —— 结果是那一整块字段常年空着：线上 125 个 active
 * 账号里公司 124 个空、期望 125 个空。选填的字段就是没人填的字段，
 * 所以它并进了门槛，见 backend/internal/service/profile.go 的 requiredMissing。
 *
 * 每一页只装一块内容：外形是外形，学历是学历。前四页里的两组字段是两对
 * 硬条件 —— 身高配体重、学历配院校 —— 各自成一页，页标题才说得清在讲什么。
 *
 * 照片这一步里只有头像和第 1 张是门槛：照片的要求是 1 张（见
 * backend/internal/service/profile.go 的 minPhotosForActive），第 2、3 张
 * 同性质 —— 有价值，但不拦人。
 */
export const BASIC_FIELDS = [
  'nickname',
  'gender',
  'birth_ym',
  // 出生日必须在这里：buildPatch 只发 keys 里的字段，漏了它就永远存不上。
  // 老档案豁免的是 zod 的必填（见 profileSchemaFor），不是这一步的提交范围。
  'birth_day',
  'city_code',
] as const satisfies readonly FieldKey[]

/** 外形：身高与体重。两项都是必填，所以界面上都没有「选填」标记。 */
export const FIGURE_FIELDS = ['height_cm', 'weight_kg'] as const satisfies readonly FieldKey[]

/**
 * 学历与毕业院校。它们是同一件事的两面 —— 在哪读的、读到什么程度 ——
 * 所以同一页。两项也都是必填。
 */
export const EDUCATION_FIELDS = [
  'education_level',
  'school_name',
] as const satisfies readonly FieldKey[]

/** 照片这一步不提交字段：头像和照片各自有接口，上传完即时生效。 */
export const PHOTO_FIELDS = [] as const satisfies readonly FieldKey[]

/**
 * 补充。11 项全是必填。
 *
 * 作息（chronotype）曾在这一块，后端 000004 迁移删掉了它：它只喂匹配打分里
 * 的生活方式一项，而那条规则对未填项不计入分母 —— 如实填一项不如不填。
 */
export const MORE_FIELDS = [
  'hometown_code',
  'occupation',
  'company',
  'income_band',
  'smoking',
  'drinking',
  'hobbies',
  'intro',
  'expectation',
  // 婚育与婚史。它们同时也参与硬条件过滤（「想要」和「不要」撞上时不会
  // 互相引荐），所以必填这一条不只是完整度问题：空着的时候匹配会按
  // 「不限」处理，等于替用户说了「都行」。
  'want_child',
  'marital_status',
] as const satisfies readonly FieldKey[]

export interface Step {
  key: 'basic' | 'figure' | 'education' | 'photos' | 'more'
  title: string
  fields: readonly FieldKey[]
}

export const STEPS: readonly Step[] = [
  { key: 'basic', title: '基本', fields: BASIC_FIELDS },
  { key: 'figure', title: '外形', fields: FIGURE_FIELDS },
  { key: 'education', title: '学历', fields: EDUCATION_FIELDS },
  { key: 'photos', title: '照片', fields: PHOTO_FIELDS },
  { key: 'more', title: '补充', fields: MORE_FIELDS },
]

/**
 * 缺失项 → 它属于哪一步。
 *
 * 这张表就是 19.6 第一个洞的补法：拿到 missing_required 之后直接跳到
 * 第一个没做完的步骤，而不是每次都从第 1 步重新来过。
 *
 * 它必须覆盖后端 admissionMissing 能报出的每一个 key。漏一个的后果不是
 * 报错而是**静默走错**：firstIncompleteStep 会跳过那个不认识的 key，
 * 用户明明只差第五步，却被丢回第一步重填一遍。
 */
const MISSING_STEP: Record<string, number> = {
  nickname: 0,
  gender: 0,
  birth_ym: 0,
  city_code: 0,
  height_cm: 1,
  weight_kg: 1,
  education_level: 2,
  school_name: 2,
  avatar_key: 3,
  photos: 3,
  hometown_code: 4,
  occupation: 4,
  company: 4,
  income_band: 4,
  smoking: 4,
  drinking: 4,
  hobbies: 4,
  intro: 4,
  expectation: 4,
  want_child: 4,
  marital_status: 4,
}

export function firstIncompleteStep(missing: readonly string[]): number {
  for (const m of missing) {
    const step = MISSING_STEP[m]
    if (step !== undefined) return step
  }
  return 0
}

import type { FieldKey } from './schema'

/**
 * 建档的五个步骤。
 *
 * 顺序不是随便排的：前四步装的是入池门槛（8 项必填 + 头像 + 第 1 张照片），
 * 第五步才是选填。用户在第五步停下来，档案也已经是可被引荐的 ——
 * 让必填项散落在最后一步是不可接受的。
 *
 * 每一页只装一块内容：外形是外形，学历是学历。四页里的两组字段是两对
 * 硬条件 —— 身高配体重、学历配院校 —— 各自成一页，页标题才说得清在讲什么。
 *
 * 照片这一步里只有头像和第 1 张是门槛：照片的要求是 1 张（见
 * backend/internal/service/profile.go 的 minPhotosForActive），第 2、3 张
 * 和第五步的选填项同性质 —— 有价值，但不拦人。
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

export const MORE_FIELDS = [
  'hometown_code',
  'occupation',
  'company',
  'income_band',
  'chronotype',
  'smoking',
  'drinking',
  'hobbies',
  'intro',
  'expectation',
  // 婚育与婚史。放在补充这一步，不进「完整度」的 12 项 ——
  // 后端也没把它们算进那 60/40 的分母里，两边的清单必须是同一份，
  // 否则用户会看到「都填完了但完成度还是 100 差一点」。
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
}

export function firstIncompleteStep(missing: readonly string[]): number {
  for (const m of missing) {
    const step = MISSING_STEP[m]
    if (step !== undefined) return step
  }
  return 0
}

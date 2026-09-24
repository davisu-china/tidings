import type { FieldKey } from './schema'

/**
 * 建档的四个步骤。
 *
 * 顺序不是随便排的：前三步装的全是入池门槛（6 项必填 + 头像 + 3 张照片），
 * 第四步才是选填。用户在第四步停下来，档案也已经是可被引荐的 ——
 * 让必填项散落在最后一步是不可接受的。
 */
export const BASIC_FIELDS = [
  'nickname',
  'gender',
  'birth_year',
  'birth_month',
  'city_code',
] as const satisfies readonly FieldKey[]

export const FIGURE_FIELDS = ['height_cm', 'education_level'] as const satisfies readonly FieldKey[]

/** 照片这一步不提交字段：头像和照片各自有接口，上传完即时生效。 */
export const PHOTO_FIELDS = [] as const satisfies readonly FieldKey[]

export const MORE_FIELDS = [
  'hometown_code',
  'weight_kg',
  'school_name',
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
  key: 'basic' | 'figure' | 'photos' | 'more'
  title: string
  fields: readonly FieldKey[]
}

export const STEPS: readonly Step[] = [
  { key: 'basic', title: '基本', fields: BASIC_FIELDS },
  { key: 'figure', title: '外形', fields: FIGURE_FIELDS },
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
  education_level: 1,
  avatar_key: 2,
  photos: 2,
}

export function firstIncompleteStep(missing: readonly string[]): number {
  for (const m of missing) {
    const step = MISSING_STEP[m]
    if (step !== undefined) return step
  }
  return 0
}

import { z } from 'zod'

import type { Profile, ProfileInput } from '@/api/types'

/**
 * 表单模型。刻意和 ProfileInput 长得不完全一样：
 *
 * 1. 出生日期拆成 birth_ym（YYYYMM）与 birth_day 两个字段，界面上是
 *    年 → 月 → 日三级联动。**年月与日分开存是有意的**：后端的
 *    birth_ym 是入池门槛与年龄筛选的落点（偏好页的岁↔月换算、候选集 SQL
 *    都按它来），而 birth_day 只影响展示。合成一个完整日期要同时改那几处，
 *    收益为零。
 * 2. 所有字段都允许 null —— 向导是分步的，第一步没填的字段在模型里
 *    必须能表示「还没填」，而不是被兜成 0。birth_day 的 null 还多一层
 *    含义：老档案只知道年月，那是「没有这个信息」而不是「没填」。
 */
export interface ProfileForm {
  nickname: string
  gender: 'M' | 'F' | null
  /** YYYYMM，如 199508 */
  birth_ym: number | null
  /** 1–31。null = 只知道年月，或还没选 */
  birth_day: number | null
  city_code: number | null
  height_cm: number | null
  education_level: number | null

  hometown_code: number | null
  weight_kg: number | null
  school_name: string
  occupation: string
  company: string
  income_band: number | null
  chronotype: number | null
  smoking: number | null
  drinking: number | null
  hobbies: string
  intro: string
  expectation: string
  want_child: number | null
  marital_status: number | null
}

/** 必填的数字字段：null 才是「没填」，0 是合法值（吸烟/饮酒）。 */
const requiredNumber = (message: string) =>
  z
    .number()
    .int()
    .nullable()
    .refine((v) => v !== null, { message })

/** 拆兴趣标签。与后端 parseHobbies 同一套分隔符。 */
export function splitHobbies(raw: string): string[] {
  return raw
    .split(/[、,，;；]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

const MAX_HOBBIES = 6
const MAX_HOBBY_RUNES = 8

/**
 * 校验规则与后端 applyInput 一一对应。
 *
 * 前端校验不是为了替代后端 —— 后端每一道都还在。它只是把「传上去才知道不行」
 * 变成「当场就知道」，这是 §19.8 明确要补的洞。
 *
 * 「日」是**条件必填**，所以 schema 是工厂：老档案（有年月、没有日）豁免，
 * 其余必填。两个方向都得照顾 ——
 *   · 无条件可选：新建档的人选完年月就点下一步，日永远是空的，
 *     「精确到日」这件事等于没做；
 *   · 无条件必填：老用户只改城市也要先补一个生日，而 form.trigger 是按
 *     「基本」整块触发的，他连昵称都改不了，除非编一个日子 ——
 *     这与「老数据不编造」直接冲突。
 */
export function profileSchemaFor({ requireBirthDay }: { requireBirthDay: boolean }) {
  const base = z.object({
    nickname: z
      .string()
      .trim()
      .min(2, '昵称需要 2 到 12 个字')
      .max(12, '昵称需要 2 到 12 个字'),
    gender: z
      .enum(['M', 'F'])
      .nullable()
      .refine((v) => v !== null, { message: '请选择性别' }),
    birth_ym: requiredNumber('请选择出生日期'),
    // 「日」自己不挂 refine：条件必填的规则挂在 birth_ym 上（见下），
    // 这样错误落在 Field 读的那个槽里，而且 BASIC_FIELDS 一定含 birth_ym，
    // trigger 不会把这条错误过滤掉。
    birth_day: z.number().int().nullable(),
    city_code: requiredNumber('请选择所在城市'),
    height_cm: requiredNumber('请选择身高'),
    education_level: requiredNumber('请选择学历'),

    hometown_code: z.number().int().nullable(),
    weight_kg: z.number().int().nullable(),
    school_name: z.string().trim().max(40, '不能超过 40 个字'),
    occupation: z.string().trim().max(40, '不能超过 40 个字'),
    company: z.string().trim().max(40, '不能超过 40 个字'),
    income_band: z.number().int().nullable(),
    chronotype: z.number().int().nullable(),
    smoking: z.number().int().nullable(),
    drinking: z.number().int().nullable(),
    hobbies: z
      .string()
      .refine((v) => splitHobbies(v).length <= MAX_HOBBIES, {
        message: `兴趣标签最多 ${MAX_HOBBIES} 个`,
      })
      .refine((v) => splitHobbies(v).every((t) => [...t].length <= MAX_HOBBY_RUNES), {
        message: `单个标签不能超过 ${MAX_HOBBY_RUNES} 个字`,
      }),
    intro: z.string().trim().max(300, '不能超过 300 个字'),
    expectation: z.string().trim().max(300, '不能超过 300 个字'),
    want_child: z.number().int().nullable(),
    marital_status: z.number().int().nullable(),
  })

  if (!requireBirthDay) return base

  return base.superRefine((v, ctx) => {
    if (v.birth_ym !== null && v.birth_day === null) {
      ctx.addIssue({
        code: 'custom',
        path: ['birth_ym'],
        message: '请选择出生日期（含哪一天）',
      })
    }
  })
}

/** 新用户与被补过日的用户走这一份。表单那层用 profileSchemaFor 生成。 */
export const profileSchema = profileSchemaFor({ requireBirthDay: true })

export type FieldKey = keyof ProfileForm

/** 回填。19.6 的第一个洞就是「表单从不回填」，这里是那个洞的修法。 */
export function toFormValues(p: Profile): ProfileForm {
  return {
    nickname: p.nickname ?? '',
    gender: p.gender,
    birth_ym: p.birth_ym,
    birth_day: p.birth_day,
    city_code: p.city_code,
    height_cm: p.height_cm,
    education_level: p.education_level,

    hometown_code: p.hometown_code,
    weight_kg: p.weight_kg,
    school_name: p.school_name,
    occupation: p.occupation,
    company: p.company,
    income_band: p.income_band,
    chronotype: p.chronotype,
    smoking: p.smoking,
    drinking: p.drinking,
    hobbies: p.hobbies.join('、'),
    intro: p.intro,
    expectation: p.expectation,
    want_child: p.want_child,
    marital_status: p.marital_status,
  }
}

/**
 * 只把这一步动过的字段拼成 PATCH 体。
 *
 * 枚举类的选填项跳过 null：后端对 income_band / chronotype 这类字段
 * 只接受 1–6、1–3，传 null 会被拒。代价是选了之后改不回「未填」——
 * 在 MVP 里这是个已知的小缺口，不是疏忽。
 */
export function buildPatch(v: ProfileForm, keys: readonly FieldKey[]): ProfileInput {
  const out: Record<string, unknown> = {}
  for (const k of keys) {
    const val = v[k]
    if (val === null) continue
    out[k] = typeof val === 'string' ? val.trim() : val
  }
  return out as ProfileInput
}

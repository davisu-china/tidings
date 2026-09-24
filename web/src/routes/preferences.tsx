import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import * as React from 'react'
import { Controller, useForm } from 'react-hook-form'
import { Link, useLoaderData } from 'react-router'
import { z } from 'zod'

import { fetchPreference, savePreference } from '@/api/preferences'
import type { Preference, PreferenceInput } from '@/api/types'
import { Field, Row } from '@/components/profile/fields'
import { Button } from '@/components/ui/button'
import { Segmented } from '@/components/ui/segmented'
import { Select, type Option } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { useToast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import {
  CITIES,
  DIVORCED_ACCEPTANCE,
  EDUCATION_LEVELS,
  INCOME_BANDS,
  REMOTE_ACCEPTANCE,
  WANT_CHILDREN,
} from '@/lib/dict'
import { messageOf } from '@/lib/errors'

export async function preferencesLoader() {
  return { preference: await fetchPreference() }
}

/**
 * 「不限」在下拉里的哨兵值。
 *
 * 后端用 NULL 表示不限，而原生 select 的 value 只能是字符串，
 * Select 组件又用空串表示「未选择」（占位项），所以不能拿空串兼作不限。
 *
 * 取 -1 而不是 0：婚史的「不接受」就是 0，拿 0 当不限的话
 * 「不限 / 接受 / 不接受」这一组会有两个 0，React 的 key 直接撞车
 * （表现为控制台里一句 duplicated key，以及选项渲染得莫名其妙）。
 * 所有偏好字段的合法取值都是非负的，-1 不会跟任何一项撞。
 */
const ANY = -1

const anyOf = <T extends number>(
  label: string,
  options: readonly { value: T; label: string }[],
): Option<number>[] => [
  { value: ANY, label },
  ...options.map((o) => ({ value: o.value as number, label: o.label })),
]

const AGE_OPTIONS: Option<number>[] = anyOf(
  '不限',
  Array.from({ length: 43 }, (_, i) => ({ value: 18 + i, label: `${18 + i} 岁` })),
)
const HEIGHT_OPTIONS: Option<number>[] = anyOf(
  '不限',
  Array.from({ length: 71 }, (_, i) => ({ value: 140 + i, label: `${140 + i} cm` })),
)
const EDU_OPTIONS = anyOf('不限', EDUCATION_LEVELS)
const INCOME_OPTIONS = anyOf('不限', INCOME_BANDS)

/** 「对方不限」也是一个选项 —— 不限婚育意愿的人比想象中多。 */
const WANT_CHILD_OPTIONS: Option<number>[] = [{ value: ANY, label: '不限' }, ...WANT_CHILDREN]
const DIVORCED_OPTIONS: Option<number>[] = [{ value: ANY, label: '不限' }, ...DIVORCED_ACCEPTANCE]
const REMOTE_OPTIONS: Option<number>[] = [{ value: ANY, label: '不限' }, ...REMOTE_ACCEPTANCE]

/** 表单模型。与 Preference 同形，只是把 null 换成控件里的哨兵值。 */
interface PrefForm {
  age_min: number
  age_max: number
  city_codes: number[]
  want_child: number
  accept_divorced: number
  accept_remote: number
  edu_min: number
  height_min: number
  height_max: number
  income_min: number
  income_max: number
}

function toFormValues(p: Preference): PrefForm {
  return {
    age_min: p.age_min ?? ANY,
    age_max: p.age_max ?? ANY,
    city_codes: p.city_codes,
    want_child: p.want_child ?? ANY,
    accept_divorced: p.accept_divorced ?? ANY,
    accept_remote: p.accept_remote ?? ANY,
    edu_min: p.edu_min ?? ANY,
    height_min: p.height_min ?? ANY,
    height_max: p.height_max ?? ANY,
    income_min: p.income_min ?? ANY,
    income_max: p.income_max ?? ANY,
  }
}

/** 哨兵值换回 null。后端全程「NULL = 不限」。 */
const orNull = (v: number): number | null => (v === ANY ? null : v)

function buildInput(v: PrefForm): PreferenceInput {
  return {
    age_min: orNull(v.age_min),
    age_max: orNull(v.age_max),
    city_codes: v.city_codes,
    want_child: orNull(v.want_child),
    accept_divorced: orNull(v.accept_divorced),
    accept_remote: orNull(v.accept_remote),
    edu_min: orNull(v.edu_min),
    height_min: orNull(v.height_min),
    height_max: orNull(v.height_max),
    income_min: orNull(v.income_min),
    income_max: orNull(v.income_max),
  }
}

/**
 * 区间校验。
 *
 * 两端的哨兵值都不参与比较：只填了下限（「165 以上」）是完全正常的用法，
 * 这时上限是不限，不能拿 0 去跟 165 比大小。
 *
 * 时间顺序在动态改，这里只说明：下面被引用的是 limitOf 的两个闭包。
 */
const limitOf = (any: number) => (v: number) => (v === any ? null : v)

const rangeIssue = (
  lo: number,
  hi: number,
  path: 'age_max' | 'height_max' | 'income_max',
  message: string,
) => {
  const l = limitOf(ANY)(lo)
  const h = limitOf(ANY)(hi)
  if (l === null || h === null) return null
  if (l <= h) return null
  return { path, message }
}

const prefSchema = z
  .object({
    age_min: z.number().int(),
    age_max: z.number().int(),
    city_codes: z.array(z.number().int()).max(5, '最多选 5 个城市'),
    want_child: z.number().int(),
    accept_divorced: z.number().int(),
    accept_remote: z.number().int(),
    edu_min: z.number().int(),
    height_min: z.number().int(),
    height_max: z.number().int(),
    income_min: z.number().int(),
    income_max: z.number().int(),
  })
  .superRefine((v, ctx) => {
    const issues = [
      rangeIssue(v.age_min, v.age_max, 'age_max', '上限不能小于下限'),
      rangeIssue(v.height_min, v.height_max, 'height_max', '上限不能小于下限'),
      rangeIssue(v.income_min, v.income_max, 'income_max', '上限不能小于下限'),
    ]
    for (const i of issues) {
      if (i) ctx.addIssue({ code: 'custom', path: [i.path], message: i.message })
    }
  })

/** 一个区块。与 /me/edit 同一套外壳，只是保存按钮在这里只有一个。 */
function Block({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: React.ReactNode
}) {
  return (
    <section className="mt-10 border-t border-line pt-8">
      <h2 className="font-serif text-[17px] text-ink">{title}</h2>
      <p className="mt-2 text-[13px] leading-[1.8] text-muted">{description}</p>
      <div className="mt-6 grid gap-5">{children}</div>
    </section>
  )
}

/**
 * 期望城市的多选。
 *
 * 用一排可切换的方块而不是多选下拉：手机上原生多选控件几乎不可用
 * （iOS 是「点一下选中、再点右上角完成」），而这个字段选几个就会看几次。
 * 上限 5 个是后端定的，到顶之后其余的置灰而不是让点了没反应。
 */
function CityPicker({
  value,
  onChange,
  error,
}: {
  value: number[]
  onChange: (next: number[]) => void
  error?: string
}) {
  const full = value.length >= 5

  function toggle(code: number) {
    if (value.includes(code)) {
      onChange(value.filter((c) => c !== code))
      return
    }
    if (full) return
    onChange([...value, code])
  }

  return (
    <Field
      label="期望城市"
      controlId="city_codes"
      group
      error={error}
      hint={
        value.length === 0
          ? '不限城市。选了之后，只会被引荐给这些城市的人。'
          : `已选 ${value.length} 个，最多 5 个。${full ? '要换的话先取消一个。' : ''}`
      }
    >
      <div
        role="group"
        aria-labelledby="city_codes-label"
        className="flex flex-wrap gap-px overflow-hidden rounded-card border border-line bg-line"
      >
        {CITIES.map((c) => {
          const active = value.includes(c.code)
          return (
            <button
              key={c.code}
              type="button"
              aria-pressed={active}
              disabled={!active && full}
              onClick={() => toggle(c.code)}
              className={cn(
                'min-h-[40px] flex-1 basis-[calc(25%-1px)] px-2 text-[13px] transition-colors duration-150',
                'focus-visible:relative focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-accent',
                active
                  ? 'bg-accent-soft text-accent-ink'
                  : 'bg-surface text-ink-2 hover:bg-surface-2',
                !active && full && 'cursor-not-allowed text-muted hover:bg-surface',
              )}
            >
              {c.name}
            </button>
          )
        })}
      </div>
    </Field>
  )
}

export function PreferencesPage() {
  const initial = useLoaderData() as { preference: Preference }
  const qc = useQueryClient()
  const toast = useToast()
  const [saving, setSaving] = React.useState(false)

  const { data: preference } = useQuery({
    queryKey: ['preference'],
    queryFn: () => fetchPreference(),
    initialData: initial.preference,
  })

  const {
    control,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<PrefForm>({
    resolver: zodResolver(prefSchema),
    defaultValues: toFormValues(preference),
    // onChange 而不是 /me/edit 那样的 onTouched：这一页一个自由输入的
    // 字段都没有，全是下拉与分段。onTouched 要等控件失焦，而 Select 与
    // Segmented 都没有 onBlur 可接（它们只暴露 value/onChange），
    // 用 onTouched 的结果是这个表单永远不会校验，区间填反了一路存上去。
    // 反过来，全是离散选择时立刻校验也不会有「打字打到一半就报错」的问题。
    mode: 'onChange',
  })

  const onSubmit = handleSubmit(async (values) => {
    setSaving(true)
    try {
      const next = await savePreference(buildInput(values))
      // 返回的就是存下来的那一份，直接写进缓存并把它当新基线，
      // 省掉一次 GET，也避免「存完了按钮还亮着」。
      qc.setQueryData(['preference'], next)
      reset(toFormValues(next))
      toast.show('已保存')
    } catch (err) {
      // 后端会挡下前端的漏网之鱼（区间反了、城市超过 5 个），
      // 它的 message 是写给用户的中文，原样透出
      toast.show(messageOf(err))
    } finally {
      setSaving(false)
    }
  })

  return (
    <div className="mx-auto max-w-[640px] px-5 py-8">
      <Link to="/me" className="text-[13px] text-muted hover:text-ink-2">
        ← 我的
      </Link>
      <h1 className="mt-4 font-serif text-[22px] leading-tight text-ink">偏好设置</h1>
      <p className="mt-3 text-[13px] leading-[1.8] text-muted">
        硬条件是先筛一道，不合的不会被推给你；软偏好只参与排序，不合也照推，只是排在后面。
        留「不限」的项不参与判定。
      </p>

      {!preference.configured && (
        <p className="mt-6 rounded-card border border-line bg-surface px-4 py-3 text-[13px] leading-[1.8] text-muted">
          还没有设过偏好，现在等于全部不限。填几项能让引荐更贴你的意思 —— 尤其是硬条件那几项。
        </p>
      )}

      <form onSubmit={onSubmit}>
        <Block
          title="硬条件"
          description="不合就淘汰，连打分的机会都没有。所以这里的每一项都要想清楚再填。"
        >
          <Field
            label="年龄"
            controlId="age_min"
            group
            error={errors.age_max?.message}
            hint="两头都可以只填一边。"
          >
            <div className="flex items-center gap-3">
              <Controller
                control={control}
                name="age_min"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={AGE_OPTIONS}
                    id="age_min"
                  />
                )}
              />
              <span className="shrink-0 text-[13px] text-muted">到</span>
              <Controller
                control={control}
                name="age_max"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={AGE_OPTIONS}
                    id="age_max"
                  />
                )}
              />
            </div>
          </Field>

          <Controller
            control={control}
            name="city_codes"
            render={({ field }) => (
              <CityPicker
                value={field.value}
                onChange={field.onChange}
                error={errors.city_codes?.message}
              />
            )}
          />

          <Controller
            control={control}
            name="want_child"
            render={({ field }) => (
              <Field
                label="对方的婚育意愿"
                controlId="want_child"
                group
                hint="「再说」与两种明确意愿都相容，只有「想要」撞上「不要」才会淘汰。"
              >
                <Segmented
                  value={field.value}
                  onChange={field.onChange}
                  options={WANT_CHILD_OPTIONS}
                  columns={2}
                />
              </Field>
            )}
          />

          <Controller
            control={control}
            name="accept_divorced"
            render={({ field }) => (
              <Field
                label="接受有婚史"
                controlId="accept_divorced"
                group
                hint="选「不接受」之后，离异的人不会出现在你的引荐里。"
              >
                <Segmented
                  value={field.value}
                  onChange={field.onChange}
                  options={DIVORCED_OPTIONS}
                  columns={3}
                />
              </Field>
            )}
          />

          <Controller
            control={control}
            name="accept_remote"
            render={({ field }) => (
              <Field
                label="接受异地"
                controlId="accept_remote"
                group
                hint="只有双方都接受，异地的人才会被引荐过来。同城不受这项影响。"
              >
                <Segmented
                  value={field.value}
                  onChange={field.onChange}
                  options={REMOTE_OPTIONS}
                  columns={3}
                />
              </Field>
            )}
          />
        </Block>

        <Block title="软偏好" description="只影响排序，不淘汰。三项合计占匹配分的 0.65。">
          <Controller
            control={control}
            name="edu_min"
            render={({ field }) => (
              <Field label="最低学历" controlId="edu_min" group>
                <Segmented
                  value={field.value}
                  onChange={field.onChange}
                  options={EDU_OPTIONS}
                  columns={3}
                />
              </Field>
            )}
          />

          <Field label="身高" controlId="height_min" group error={errors.height_max?.message}>
            <Row>
              <Controller
                control={control}
                name="height_min"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={HEIGHT_OPTIONS}
                    id="height_min"
                  />
                )}
              />
              <Controller
                control={control}
                name="height_max"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={HEIGHT_OPTIONS}
                    id="height_max"
                  />
                )}
              />
            </Row>
          </Field>

          <Field
            label="收入"
            controlId="income_min"
            group
            error={errors.income_max?.message}
            hint="只有区间，没有具体数字。"
          >
            <Row>
              <Controller
                control={control}
                name="income_min"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={INCOME_OPTIONS}
                    id="income_min"
                  />
                )}
              />
              <Controller
                control={control}
                name="income_max"
                render={({ field }) => (
                  <Select
                    value={field.value}
                    onChange={field.onChange}
                    options={INCOME_OPTIONS}
                    id="income_max"
                  />
                )}
              />
            </Row>
          </Field>
        </Block>

        <div className="mt-8 flex items-center gap-4">
          <Button type="submit" disabled={!isDirty || saving}>
            {saving ? <Spinner /> : '保存'}
          </Button>
          {isDirty && !saving && <span className="text-[13px] text-muted">有未保存的改动</span>}
        </div>
      </form>
    </div>
  )
}

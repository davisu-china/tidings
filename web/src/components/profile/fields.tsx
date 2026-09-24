import * as React from 'react'
import { Controller, useController, type Control, type FieldErrors } from 'react-hook-form'

import { Input, Textarea } from '@/components/ui/input'
import { FieldError, Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Select } from '@/components/ui/select'
import { ageFromBirthYM, CHRONOTYPES, EDUCATION_LEVELS, formatBirthDate, FREQUENCIES, GENDERS, INCOME_BANDS, MARITAL_STATUSES, WANT_CHILDREN } from '@/lib/dict'

import { BirthDatePicker } from './BirthDatePicker'
import { RegionPicker } from './RegionPicker'
import { SchoolSelect } from './SchoolSelect'
import { splitHobbies, type ProfileForm } from './schema'

export interface SectionProps {
  control: Control<ProfileForm>
  errors: FieldErrors<ProfileForm>
}

/**
 * 字段外壳：标签 + 控件 + 说明 + 错误。
 *
 * 关联关系在这里统一接线，而不是让每个调用点各写一遍：
 * 普通控件用 label 的 htmlFor 指向控件的 id；radiogroup 不是可标注元素，
 * 改用 aria-labelledby。说明和错误各自有 id，通过 aria-describedby 挂上去 ——
 * 读屏软件才会在聚焦时念出「引荐按城市圈定范围」这类提示。
 *
 * 说明文字放在控件下面而不是上面：它是读完控件之后的补充，不是前置条件。
 * 校验没过时不显示说明，错误要独占那一行。
 */
export function Field({
  label,
  controlId,
  group,
  hint,
  error,
  optional,
  children,
}: {
  label: string
  controlId: string
  /** 控件是 radiogroup 这类分组容器，走 aria-labelledby 而不是 htmlFor */
  group?: boolean
  hint?: React.ReactNode
  error?: string
  /** 选填项标出来。不标的话用户会把每一项都当成必须填的 */
  optional?: boolean
  children: React.ReactNode
}) {
  const labelId = `${controlId}-label`
  const hintId = `${controlId}-hint`
  const errorId = `${controlId}-error`
  const describedBy = error ? errorId : hint ? hintId : undefined

  let control = children
  if (React.isValidElement(children)) {
    const extra: Record<string, unknown> = { 'aria-describedby': describedBy }
    if (group) extra['aria-labelledby'] = labelId
    else extra.id = controlId
    control = React.cloneElement(children as React.ReactElement<Record<string, unknown>>, extra)
  }

  return (
    <div>
      <div className="flex items-baseline gap-2">
        <Label id={labelId} htmlFor={group ? undefined : controlId}>
          {label}
        </Label>
        {optional && <span className="text-[12px] text-muted">选填</span>}
      </div>
      <div className="mt-2">{control}</div>
      {error ? (
        <FieldError id={errorId}>{error}</FieldError>
      ) : (
        hint && (
          <p id={hintId} className="mt-1.5 text-[13px] leading-[1.7] text-muted">
            {hint}
          </p>
        )
      )}
    </div>
  )
}

/** 两个字段并排。窄屏仍然单列 —— 一行塞两个数字框在手机上会挤到看不清。 */
export function Row({ children }: { children: React.ReactNode }) {
  return <div className="grid gap-5 sm:grid-cols-2">{children}</div>
}

// 出生日期与城市都不在这里：它们各自是复合控件（年月日三级、省市两级），
// 见 BirthDatePicker.tsx 与 RegionPicker.tsx。这里只留单层的选项表。

// 身高用原生 select：手机上它会唤起系统滚轮，精确停在 165 比任何自绘控件都容易（19.2）
const HEIGHT_OPTIONS = Array.from({ length: 71 }, (_, i) => {
  const h = 140 + i
  return { value: h, label: `${h} cm` }
})

const MAX_HOBBIES = 6

/**
 * 出生日期。年 → 月 → 日是一个问题，却在表单里是两个字段
 * （birth_ym + birth_day），接线放在这里，免得 BasicFields 里再套一层。
 */
function BirthDateField({ control, errors }: SectionProps) {
  const ymField = useController({ control, name: 'birth_ym' })
  const dayField = useController({ control, name: 'birth_day' })
  const ym = ymField.field.value
  const day = dayField.field.value

  const age = ageFromBirthYM(ym)
  // 年龄仍然是按月的口径（ageFromBirthYM 只看到月），日不参与计算 ——
  // 这里的日只是把生日写完整。
  //
  // 老档案只知道年月：说清「还差哪一天」，而不是把它当成一个错误。
  const hint =
    age === null
      ? undefined
      : day === null
        ? `${formatBirthDate(ym, null)}，你现在 ${age} 岁。补上具体哪一天吧。`
        : `${formatBirthDate(ym, day)}，你现在 ${age} 岁。`

  return (
    <Field label="出生日期" controlId="birth_date" group error={errors.birth_ym?.message} hint={hint}>
      <BirthDatePicker
        idBase="birth_date"
        ym={ym}
        day={day}
        onChange={(next) => {
          ymField.field.onChange(next.ym)
          dayField.field.onChange(next.day)
        }}
      />
    </Field>
  )
}

/** 基本：昵称 · 性别 · 出生日期 · 城市。四项全是必填，也是入池门槛的头四项。 */
export function BasicFields({ control, errors }: SectionProps) {
  return (
    <div className="grid gap-5">
      <Controller
        control={control}
        name="nickname"
        render={({ field }) => (
          <Field label="昵称" controlId="nickname" error={errors.nickname?.message}>
            <Input
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              placeholder="别人怎么称呼你"
              autoComplete="nickname"
            />
          </Field>
        )}
      />

      <Controller
        control={control}
        name="gender"
        render={({ field }) => (
          <Field
            label="性别"
            controlId="gender"
            group
            error={errors.gender?.message}
            hint="设定后不能修改。允许来回切换等于让人免费刷两侧的候选池。"
          >
            <Segmented value={field.value} onChange={field.onChange} options={GENDERS} />
          </Field>
        )}
      />

      <BirthDateField control={control} errors={errors} />

      <Controller
        control={control}
        name="city_code"
        render={({ field }) => (
          <Field label="所在城市" controlId="city_code" group error={errors.city_code?.message} hint="引荐按城市圈定范围。">
            <RegionPicker
              idBase="city_code"
              title="选择所在城市"
              value={field.value}
              onChange={field.onChange}
            />
          </Field>
        )}
      />
    </div>
  )
}

/** 外形：身高与体重。两项并排 —— 它们是一件事的两面，上下排会显得互不相干。 */
export function FigureFields({ control, errors }: SectionProps) {
  return (
    <Row>
      <Controller
        control={control}
        name="height_cm"
        render={({ field }) => (
          <Field label="身高" controlId="height_cm" error={errors.height_cm?.message}>
            <Select
              value={field.value}
              onChange={field.onChange}
              options={HEIGHT_OPTIONS}
              placeholder="请选择身高"
            />
          </Field>
        )}
      />
      <Controller
        control={control}
        name="weight_kg"
        render={({ field }) => (
          <Field label="体重" controlId="weight_kg" error={errors.weight_kg?.message}>
            <Input
              type="number"
              inputMode="numeric"
              min={35}
              max={150}
              value={field.value ?? ''}
              onChange={(e) =>
                field.onChange(e.target.value === '' ? null : Number(e.target.value))
              }
              onBlur={field.onBlur}
              placeholder="公斤"
            />
          </Field>
        )}
      />
    </Row>
  )
}

/**
 * 学历与毕业院校。同一件事的两面 —— 在哪读的、读到什么程度 —— 所以并排，
 * 上下排会让人以为它们是两个互不相干的字段。
 */
export function EducationFields({ control, errors }: SectionProps) {
  return (
    <Row>
      <Controller
        control={control}
        name="education_level"
        render={({ field }) => (
          <Field label="学历" controlId="education_level" group error={errors.education_level?.message}>
            <Segmented
              value={field.value}
              onChange={field.onChange}
              options={EDUCATION_LEVELS}
              columns={2}
            />
          </Field>
        )}
      />
      <Controller
        control={control}
        name="school_name"
        render={({ field }) => (
          <Field
            label="毕业院校"
            controlId="school_name"
            error={errors.school_name?.message}
            hint="输入几个字就能找到，从列表里选。"
          >
            <SchoolSelect value={field.value} onChange={field.onChange} />
          </Field>
        )}
      />
    </Row>
  )
}

/** 补充：全是选填，却占完整度的 40 分 —— 选填给足权重是有意的（§4.2）。 */
export function MoreFields({ control, errors }: SectionProps) {
  return (
    <div className="grid gap-5">
      <Controller
        control={control}
        name="hometown_code"
        render={({ field }) => (
          <Field label="家乡" controlId="hometown_code" group optional error={errors.hometown_code?.message}>
            <RegionPicker
              idBase="hometown_code"
              title="选择家乡"
              value={field.value}
              onChange={field.onChange}
            />
          </Field>
        )}
      />

      <Row>
        <Controller
          control={control}
          name="occupation"
          render={({ field }) => (
            <Field label="职业" controlId="occupation" optional error={errors.occupation?.message}>
              <Input value={field.value} onChange={field.onChange} onBlur={field.onBlur} placeholder="如 产品经理" />
            </Field>
          )}
        />
        <Controller
          control={control}
          name="company"
          render={({ field }) => (
            <Field label="工作单位" controlId="company" optional error={errors.company?.message}>
              <Input value={field.value} onChange={field.onChange} onBlur={field.onBlur} placeholder="如 某互联网公司" />
            </Field>
          )}
        />
      </Row>

      <Controller
        control={control}
        name="income_band"
        render={({ field }) => (
          <Field
            label="年收入"
            controlId="income_band"
            group
            optional
            error={errors.income_band?.message}
            hint="只展示区间，不展示具体数字。"
          >
            <Segmented
              value={field.value}
              onChange={field.onChange}
              options={INCOME_BANDS}
              columns={2}
            />
          </Field>
        )}
      />

      <Controller
        control={control}
        name="want_child"
        render={({ field }) => (
          <Field
            label="关于孩子"
            controlId="want_child"
            group
            optional
            error={errors.want_child?.message}
            hint="「想要」和「不要」撞上时不会互相引荐。选「再说」两边都不挡。"
          >
            <Segmented
              value={field.value}
              onChange={field.onChange}
              options={WANT_CHILDREN}
              columns={3}
            />
          </Field>
        )}
      />

      <Controller
        control={control}
        name="marital_status"
        render={({ field }) => (
          <Field label="婚史" controlId="marital_status" group optional error={errors.marital_status?.message}>
            <Segmented
              value={field.value}
              onChange={field.onChange}
              options={MARITAL_STATUSES}
              columns={2}
            />
          </Field>
        )}
      />

      <Row>
        <Controller
          control={control}
          name="chronotype"
          render={({ field }) => (
            <Field label="作息" controlId="chronotype" group optional error={errors.chronotype?.message}>
              <Segmented value={field.value} onChange={field.onChange} options={CHRONOTYPES} />
            </Field>
          )}
        />
        <Controller
          control={control}
          name="smoking"
          render={({ field }) => (
            <Field label="吸烟" controlId="smoking" group optional error={errors.smoking?.message}>
              <Segmented
                value={field.value}
                onChange={field.onChange}
                options={FREQUENCIES}
                columns={3}
              />
            </Field>
          )}
        />
      </Row>

      <Controller
        control={control}
        name="drinking"
        render={({ field }) => (
          <Field label="饮酒" controlId="drinking" group optional error={errors.drinking?.message}>
            <Segmented
              value={field.value}
              onChange={field.onChange}
              options={FREQUENCIES}
              columns={3}
            />
          </Field>
        )}
      />

      <Controller
        control={control}
        name="hobbies"
        render={({ field }) => {
          const tags = splitHobbies(field.value)
          return (
            <Field
              label="兴趣"
              controlId="hobbies"
              optional
              error={errors.hobbies?.message}
              hint={
                tags.length > 0 ? (
                  <>
                    已识别 <span className="tnum font-mono text-ink-2">{tags.length}</span>/
                    {MAX_HOBBIES} 个：{tags.join(' · ')}
                  </>
                ) : (
                  '用顿号分隔，最多 6 个。逗号也能识别。'
                )
              }
            >
              <Input
                value={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
                placeholder="摄影、徒步、做饭"
              />
            </Field>
          )
        }}
      />

      <Controller
        control={control}
        name="intro"
        render={({ field }) => (
          <Field
            label="自我介绍"
            controlId="intro"
            optional
            error={errors.intro?.message}
            hint={
              <>
                <span className="tnum font-mono">{[...field.value].length}</span>/300 字。写你周末
                怎么过，比写「性格开朗」有用。
              </>
            }
          >
            <Textarea
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              rows={4}
              placeholder="说点具体的。"
            />
          </Field>
        )}
      />

      <Controller
        control={control}
        name="expectation"
        render={({ field }) => (
          <Field
            label="对另一半的期待"
            controlId="expectation"
            optional
            error={errors.expectation?.message}
            hint={
              <>
                <span className="tnum font-mono">{[...field.value].length}</span>/300 字
              </>
            }
          >
            <Textarea
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              rows={3}
              placeholder="想找一个什么样的人。"
            />
          </Field>
        )}
      />
    </div>
  )
}

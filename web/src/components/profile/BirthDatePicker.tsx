import * as React from 'react'

import { Sheet, SheetTrigger } from '@/components/ui/sheet'
import { Wheel, WheelGroup, type WheelOption } from '@/components/ui/wheel'
import { ageFromBirthYM, birthYearRange, daysInMonth, formatBirthDate, monthsInYear } from '@/lib/dict'

/**
 * 空着打开时年列的落点：今年往前 28 年。
 *
 * 有人会直接点「完成」留下一个 1 月 1 日的假生日 —— 这是有意接受的。
 * 另一条路是「不滚就不给点完成」，但那意味着每一次选择都要先空滚一下，
 * 而三个滚轮并排摆着、上面还写着当前值，误提交的人比想象中少。
 * PRD 5.3 对身高体重给的是同一条思路：「初值落在中位数，避免用户被迫
 * 从最小值滚起」。
 */
const DEFAULT_AGE = 28

interface Draft {
  year: number
  month: number
  day: number
}

function seed(ym: number | null, day: number | null): Draft {
  if (ym === null) {
    return { year: new Date().getFullYear() - DEFAULT_AGE, month: 1, day: 1 }
  }
  const year = Math.floor(ym / 100)
  const month = Math.min(ym % 100, monthsInYear(year))
  // 老档案只知道年月，日落在 1 —— 上面的实时回显会把它显示出来，
  // 用户看得见自己要提交的是什么。
  return { year, month, day: Math.min(day ?? 1, daysInMonth(year, month)) }
}

/**
 * 选项表里没有就把它补在开头。
 *
 * 存量数据可能落在**今天**的可选范围之外：2026 年建的号存了 1966 年，
 * 到 2027 年 min 就变成 1967 了。不补的话滚轮找不到这个值、退到第 0 项，
 * 等于打开弹层就把人家的生日改掉。与 RegionSelect 对未知城市码是同一条原则。
 */
function withStored(list: WheelOption[], stored: number): WheelOption[] {
  if (list.some((o) => o.value === stored)) return list
  return [{ value: stored, label: String(stored) }, ...list]
}

/**
 * 出生日期：一个字段，点开是底部弹层的年 / 月 / 日三列滚轮。
 *
 * 取代了原来的三个并排原生 select。三个 select 是「一个问题摊成三个控件」，
 * 用户得依次点开三次系统滚轮；现在是一次点开、一屏选完。
 *
 * 值契约没变：对外仍然是一次写两个字段（ym = YYYYMM、day = 1–31），
 * 因为 `birth_day` 在库里是独立一列（老档案只有年月）。接线在 fields.tsx。
 */
export function BirthDatePicker({
  ym,
  day,
  onChange,
  idBase,
  disabled,
  'aria-labelledby': ariaLabelledby,
  'aria-describedby': ariaDescribedby,
}: {
  /** YYYYMM；null = 还没选 */
  ym: number | null
  /** 1–31；null = 只知道年月（老档案） */
  day: number | null
  onChange: (next: { ym: number; day: number }) => void
  /** id 前缀。触发器是 `${idBase}`，三列是 `${idBase}_year` / `_month` / `_day` */
  idBase: string
  disabled?: boolean
  /** Field 在 group 模式下注入，见 fields.tsx */
  'aria-labelledby'?: string
  'aria-describedby'?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [draft, setDraft] = React.useState<Draft>(() => seed(ym, day))
  // 范围外的存量年份。整个弹层打开期间钉住不动 —— 跟着 draft 走的话，
  // 用户一滚开这一项就消失，整列少一行，滚轮会突然跳一格。
  const [pinnedYear, setPinnedYear] = React.useState<number | null>(null)

  const { min, max } = React.useMemo(() => birthYearRange(), [])

  function openSheet() {
    const s = seed(ym, day)
    setDraft(s)
    setPinnedYear(s.year < min || s.year > max ? s.year : null)
    setOpen(true)
  }

  const yearOptions = React.useMemo(() => {
    const out: WheelOption[] = []
    for (let y = max; y >= min; y--) out.push({ value: y, label: String(y) })
    return pinnedYear === null ? out : withStored(out, pinnedYear)
  }, [min, max, pinnedYear])

  const monthOptions = React.useMemo(
    () =>
      Array.from({ length: monthsInYear(draft.year) }, (_, i) => ({
        value: i + 1,
        label: String(i + 1),
      })),
    [draft.year],
  )

  const dayOptions = React.useMemo(
    () =>
      Array.from({ length: daysInMonth(draft.year, draft.month) }, (_, i) => ({
        value: i + 1,
        label: String(i + 1),
      })),
    [draft.year, draft.month],
  )

  // 三级联动。夹紧而不是像旧版那样清空：滚轮里不存在「空」这一格，
  // 停在半路比替用户挪一格更糟。回显就在滚轮上方，挪了看得见。
  function setYear(year: number) {
    const month = Math.min(draft.month, monthsInYear(year))
    setDraft({ year, month, day: Math.min(draft.day, daysInMonth(year, month)) })
  }
  function setMonth(month: number) {
    setDraft({ ...draft, month, day: Math.min(draft.day, daysInMonth(draft.year, month)) })
  }

  const draftYm = draft.year * 100 + draft.month
  const age = ageFromBirthYM(draftYm)

  return (
    <>
      <SheetTrigger
        id={idBase}
        text={ym === null ? '' : formatBirthDate(ym, day)}
        placeholder="请选择"
        open={open}
        disabled={disabled}
        onClick={openSheet}
        labelledBy={ariaLabelledby}
        describedBy={ariaDescribedby}
      />

      <Sheet
        open={open}
        title="选择出生日期"
        onClose={() => setOpen(false)}
        onConfirm={() => {
          onChange({ ym: draftYm, day: draft.day })
          setOpen(false)
        }}
      >
        {/* 三列数字并排，只有这一行能说明它们合起来是哪一天 */}
        <p className="pt-3 text-center text-[13px] text-muted">
          {formatBirthDate(draftYm, draft.day)}
          {age !== null && ` · ${age} 岁`}
        </p>
        <div className="px-4 pb-1 pt-2">
          <WheelGroup cols="1.4fr 1fr 1fr">
            <Wheel
              id={`${idBase}_year`}
              label="出生年"
              caption="年"
              options={yearOptions}
              value={draft.year}
              onChange={setYear}
            />
            <Wheel
              id={`${idBase}_month`}
              label="出生月"
              caption="月"
              options={monthOptions}
              value={draft.month}
              onChange={setMonth}
            />
            <Wheel
              id={`${idBase}_day`}
              label="出生日"
              caption="日"
              options={dayOptions}
              value={draft.day}
              onChange={(d) => setDraft({ ...draft, day: d })}
            />
          </WheelGroup>
        </div>
      </Sheet>
    </>
  )
}

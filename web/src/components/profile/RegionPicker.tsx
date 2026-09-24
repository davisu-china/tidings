import * as React from 'react'

import { Sheet, SheetTrigger } from '@/components/ui/sheet'
import { Wheel, WheelGroup, type WheelOption } from '@/components/ui/wheel'
import { citiesOf, cityName, PROVINCES, provinceName, provinceOfCode } from '@/lib/regions'

interface Draft {
  province: number
  city: number
}

/**
 * 「一个城市字段」的选择器：所在城市、家乡。
 *
 * 对外只读写一个市码，省由市码反查得到 —— 与它替换掉的 RegionSelect 契约
 * 完全一致，所以 fields.tsx 那边只是换了个组件名。
 *
 * 弹层里是一屏两列：左省右市，滚省时右边的市整列跟着换。PRD 5.3 的
 * 「省市两级弹层 —— 先省后市，存国标 6 位行政区划码」。
 */
export function RegionPicker({
  value,
  onChange,
  idBase,
  title,
  disabled,
  'aria-labelledby': ariaLabelledby,
  'aria-describedby': ariaDescribedby,
}: {
  value: number | null
  onChange: (code: number) => void
  /** id 前缀。触发器是 `${idBase}`，两列是 `${idBase}_province` / `_city` */
  idBase: string
  /** 弹层标题：「选择所在城市」/「选择家乡」 */
  title: string
  disabled?: boolean
  'aria-labelledby'?: string
  'aria-describedby'?: string
}) {
  const [open, setOpen] = React.useState(false)
  const [draft, setDraft] = React.useState<Draft>(() => seed(value))
  // 库里存着、但区划表里没有的市码（老数据或上游改过区划）。只在它原本
  // 那个省里补出来，换省之后就该消失 —— 它不属于别处。
  const [pinned, setPinned] = React.useState<{ province: number; city: number } | null>(null)

  function openSheet() {
    const s = seed(value)
    setDraft(s)
    const known = citiesOf(s.province).some((c) => c.code === s.city)
    setPinned(known ? null : { province: s.province, city: s.city })
    setOpen(true)
  }

  const provinceOptions = React.useMemo(
    () => PROVINCES.map((p) => ({ value: p.code, label: p.name })),
    [],
  )

  const cityOptions = React.useMemo(() => {
    const list: WheelOption[] = citiesOf(draft.province).map((c) => ({
      value: c.code,
      label: c.name,
    }))
    // 只在它还**是当前值**的时候补出来。补它是为了别把人家存过的值悄悄改掉，
    // 不是让它在本省当常驻项 —— 用户一旦滚走再滚回来，它就该消失。
    const shown = pinned && pinned.province === draft.province && pinned.city === draft.city
    if (shown && !list.some((o) => o.value === pinned.city)) {
      return [{ value: pinned.city, label: `未知城市 ${pinned.city}` }, ...list]
    }
    return list
  }, [draft.province, draft.city, pinned])

  // 换省：市落到新省的第一项。
  //
  // 旧版这里是**清空**（城市下拉变回「市」）。滚轮里没有「空」这一格，
  // 落到第一项是它唯一说得通的对应 —— 而且弹层上方的回显会写出
  // 「浙江 · 杭州」，用户看得见自己选到哪儿了。
  //
  // 不写「原来的市若还在新省里就留着」那种判断：行政区划码全国唯一，
  // 一个市只属于一个省，那个分支恒为假。
  function setProvince(province: number) {
    const list = citiesOf(province)
    if (list.length === 0) return
    setDraft({ province, city: list[0].code })
  }

  return (
    <>
      <SheetTrigger
        id={idBase}
        text={cityName(value)}
        placeholder="请选择"
        open={open}
        disabled={disabled}
        onClick={openSheet}
        labelledBy={ariaLabelledby}
        describedBy={ariaDescribedby}
      />

      <Sheet
        open={open}
        title={title}
        onClose={() => setOpen(false)}
        onConfirm={() => {
          onChange(draft.city)
          setOpen(false)
        }}
      >
        <p className="pt-3 text-center text-[13px] text-muted">
          {provinceName(draft.province)} · {cityName(draft.city)}
        </p>
        <div className="px-4 pb-1 pt-2">
          <WheelGroup cols="1fr 1fr">
            <Wheel
              id={`${idBase}_province`}
              label="省份"
              caption="省"
              options={provinceOptions}
              value={draft.province}
              onChange={setProvince}
            />
            <Wheel
              id={`${idBase}_city`}
              label="城市"
              caption="市"
              options={cityOptions}
              value={draft.city}
              onChange={(city) => setDraft({ ...draft, city })}
            />
          </WheelGroup>
        </div>
      </Sheet>
    </>
  )
}

/**
 * 打开弹层时的初值。省由市码反查，反查不到就按 6 位码的省级前缀推
 * （provinceOfCode 就是这么做的）—— 于是未知码也能落在一个像样的省上，
 * 而不是把省列也一起丢掉。
 */
function seed(value: number | null): Draft {
  const first = PROVINCES[0].code
  if (value === null) return { province: first, city: citiesOf(first)[0].code }
  const province = provinceOfCode(value)
  // 省份本身不在表里（更老的码）时退回第一条，市保持不变、由 pinned 补出来
  const province2 = PROVINCES.some((p) => p.code === province) ? province : first
  return { province: province2, city: value }
}

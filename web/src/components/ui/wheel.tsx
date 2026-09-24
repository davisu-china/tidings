import * as React from 'react'

import { cn } from '@/lib/cn'

/**
 * 滚轮选择器。出生日期的年/月/日、城市弹层的省/市都用它。
 *
 * 做法照 stubborn-love 的 WheelPicker.vue（PRD 5.3 说这套控件「已在
 * stubborn-love 中实现并验证」）：scroll-snap 吸附 + 中间一行高亮。
 * 尺寸定义在 index.css 的 .wheel-frame 上，这边只读不写。
 */

/** 一行的高度。与 .wheel-frame 里的 --wheel-item-h 是同一个数，改要一起改。 */
export const WHEEL_ITEM_H = 40

export interface WheelOption {
  value: number
  label: string
}

/**
 * 外面这个框。中间那条指示带和上下渐隐都画在这里 —— 它们是**整个框**的，
 * 不是某一列的：三列并排时指示带要横跨过去，每条列各画一条会断成三截。
 */
export function WheelGroup({
  cols,
  children,
}: {
  /** grid-template-columns。三列日期比两列省市需要更宽的年列 */
  cols: string
  children: React.ReactNode
}) {
  return (
    <div className="wheel-frame">
      <div className="wheel-cols" style={{ gridTemplateColumns: cols }}>
        {children}
      </div>
      <div className="wheel-band" aria-hidden />
      <div className="wheel-fade" aria-hidden />
    </div>
  )
}

/**
 * 一列滚轮。
 *
 * 值完全由外面控制，这里只负责「滚动 ↔ 值」的换算。换算只有一个式子：
 * 停在第 i 项 ⟺ scrollTop = i * 行高。首尾各留两行 padding，所以第 0 项
 * 也能滚到正中，不需要给 index 加减偏移。
 *
 * **键盘是补回来的，不是原生的。** 换成自绘滚轮之后，一个滚动容器对键盘
 * 用户完全不可达 —— 而它替换掉的原生 select 本来可达。所以这里有
 * role=listbox + 方向键，这是补丁，不是等价物。
 */
export function Wheel({
  id,
  label,
  caption,
  options,
  value,
  onChange,
}: {
  id: string
  /** 读屏听到的列名（「出生年」）。caption 是屏幕上那一个字（「年」） */
  label: string
  caption: string
  options: readonly WheelOption[]
  value: number | null
  onChange: (value: number) => void
}) {
  const ref = React.useRef<HTMLDivElement>(null)
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null)

  // 选项表里没有当前值就是 -1（理论上不该发生，各调用点都做了兜底）。
  // 落到第 0 项，至少不会停在一个空列上。
  const found = options.findIndex((o) => o.value === value)
  const index = found < 0 ? 0 : found

  const scrollToIndex = React.useCallback((i: number, smooth = false) => {
    ref.current?.scrollTo({
      top: i * WHEEL_ITEM_H,
      behavior: smooth ? 'smooth' : 'auto',
    })
  }, [])

  // 外部值变了就跟着滚过去：打开弹层时定位到当前值、换省之后市列重建、
  // 键盘改了值。用 layout effect 是为了和这一帧的绘制同一拍 ——
  // 放到 useEffect 里会先按旧位置画一帧，看上去像闪了一下。
  //
  // options 在依赖里是因为市列会整列换掉：长度变了但 index 可能还是 0，
  // 只有 options 变了才知道该重新对齐。
  React.useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const target = index * WHEEL_ITEM_H
    if (Math.abs(el.scrollTop - target) > 1) el.scrollTop = target
  }, [index, options])

  // scroll 是连续发的（惯性滚动时每帧都有），停下来才认一次。
  // 不等 scrollend：它在 Safari 26 才有，而且合成事件根本不触发它。
  const onScroll = () => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      const el = ref.current
      if (!el || options.length === 0) return
      const i = Math.round(el.scrollTop / WHEEL_ITEM_H)
      const next = options[Math.max(0, Math.min(options.length - 1, i))]
      if (next && next.value !== value) onChange(next.value)
    }, 120)
  }

  React.useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  // 双保险：scroll-snap 理论上已经吸附好了，这一条兜住「停在两项之间」。
  // 用原生监听而不是 onScrollEnd，避免依赖 React 对这个事件的支持。
  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    const onEnd = () => {
      const i = Math.round(el.scrollTop / WHEEL_ITEM_H)
      const target = i * WHEEL_ITEM_H
      if (Math.abs(el.scrollTop - target) > 1) el.scrollTo({ top: target, behavior: 'smooth' })
    }
    el.addEventListener('scrollend', onEnd)
    return () => el.removeEventListener('scrollend', onEnd)
  }, [])

  const onKeyDown = (e: React.KeyboardEvent) => {
    const n = options.length
    if (n === 0) return
    const jump: Record<string, number> = {
      ArrowUp: index - 1,
      ArrowDown: index + 1,
      PageUp: index - 5,
      PageDown: index + 5,
      Home: 0,
      End: n - 1,
    }
    const raw = jump[e.key]
    if (raw === undefined) return
    e.preventDefault()
    const next = Math.max(0, Math.min(n - 1, raw))
    // 只改值，滚动交给上面那个 layout effect —— 两条路都去动 scrollTop
    // 会互相打断，平滑滚动会被瞬间拉回。
    if (next !== index) onChange(options[next].value)
  }

  return (
    <div className="wheel-col">
      <div
        ref={ref}
        id={id}
        role="listbox"
        aria-label={label}
        aria-activedescendant={`${id}_opt_${index}`}
        tabIndex={0}
        onScroll={onScroll}
        onKeyDown={onKeyDown}
        className="wheel-scroll"
      >
        {options.map((o, i) => (
          <div
            key={o.value}
            id={`${id}_opt_${i}`}
            role="option"
            aria-selected={i === index}
            // 就是这个选项的值，和原来 <option value> 同义。e2e 靠它定位。
            data-value={String(o.value)}
            // 点一个看得见的邻居也能选它 —— 手指比滚轮准
            onClick={() => scrollToIndex(i, true)}
            className={cn('wheel-item', i === index && 'is-active')}
          >
            {o.label}
          </div>
        ))}
      </div>
      <div className="wheel-caption" aria-hidden>
        {caption}
      </div>
    </div>
  )
}

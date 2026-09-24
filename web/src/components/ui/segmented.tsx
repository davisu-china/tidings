import { cn } from '@/lib/cn'

import type { Option } from './select'

interface SegmentedProps<T extends string | number> {
  value: T | null
  onChange: (value: T) => void
  options: readonly Option<T>[]
  /** 两三项排一行；六项那种排两行更省横向空间 */
  columns?: 2 | 3 | 4
  disabled?: boolean
  id?: string
  /** 分组没有 htmlFor 可用，标签要靠 aria-labelledby 关联进来 */
  'aria-labelledby'?: string
  'aria-describedby'?: string
  className?: string
}

/**
 * 分段选择。用于 2–4 个选项的字段。
 *
 * 比下拉少一次点击，而且选项全都看得见 —— 性别、学历、吸烟这类字段
 * 用户在填之前就想知道有哪些选项，藏在展开层里反而要来回翻。
 */
export function Segmented<T extends string | number>({
  value,
  onChange,
  options,
  columns = 2,
  disabled,
  className,
  ...aria
}: SegmentedProps<T>) {
  return (
    <div
      role="radiogroup"
      {...aria}
      className={cn(
        'grid gap-px overflow-hidden rounded-card border border-line bg-line',
        columns === 2 && 'grid-cols-2',
        columns === 3 && 'grid-cols-3',
        columns === 4 && 'grid-cols-4',
        className,
      )}
    >
      {options.map((o) => {
        const active = value === o.value
        return (
          <button
            key={String(o.value)}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            onClick={() => onChange(o.value)}
            className={cn(
              'h-11 px-2 text-[15px] transition-colors duration-150',
              'focus-visible:outline-none focus-visible:bg-accent-soft',
              // 选中用 accent 底 + on-accent 字，未选是纸面底色
              active
                ? 'bg-accent text-on-accent'
                : 'bg-surface text-ink-2 hover:bg-surface-2',
              disabled && 'cursor-not-allowed opacity-45',
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

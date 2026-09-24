import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/cn'

export interface Option<T extends string | number> {
  value: T
  label: string
}

interface SelectProps<T extends string | number> {
  value: T | null
  onChange: (value: T) => void
  options: readonly Option<T>[]
  placeholder?: string
  id?: string
  className?: string
  disabled?: boolean
}

/**
 * 下拉选择。用的是原生 select 而不是 Radix —— 在手机上原生控件会唤起
 * 系统选择器（iOS 的滚轮），精确停在 165 或 1995 年 8 月比任何自绘控件都容易，
 * 而这正是 19.2 要的效果（「滑块在手机上很难精确停在 165」）。
 *
 * 收起状态外观完全由我们控制：appearance-none 之后自己画边框与箭头。
 */
export function Select<T extends string | number>({
  value,
  onChange,
  options,
  placeholder = '请选择',
  id,
  className,
  disabled,
}: SelectProps<T>) {
  // select 的 value 只能是字符串，数字选项在这里来回转一次
  const isNumeric = options.length > 0 && typeof options[0].value === 'number'

  return (
    <div className={cn('relative', className)}>
      <select
        id={id}
        disabled={disabled}
        value={value === null ? '' : String(value)}
        onChange={(e) => {
          const raw = e.target.value
          onChange((isNumeric ? Number(raw) : raw) as T)
        }}
        className={cn(
          'h-11 w-full appearance-none rounded-card border border-line bg-surface',
          'pl-3 pr-9 text-[16px] text-ink',
          'transition-colors duration-150 focus:border-accent focus:outline-none',
          'disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-muted',
          // 占位项没有 value，未选择时文字用 muted
          value === null && 'text-muted',
        )}
      >
        <option value="" disabled>
          {placeholder}
        </option>
        {options.map((o) => (
          <option key={String(o.value)} value={String(o.value)}>
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown
        aria-hidden
        className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted"
      />
    </div>
  )
}

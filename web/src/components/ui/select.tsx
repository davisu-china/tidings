import * as React from 'react'

import { ChevronDown } from 'lucide-react'

import { cn } from '@/lib/cn'

export interface Option<T extends string | number> {
  value: T
  label: string
}

/** 一组选项。渲染成原生 optgroup：组标题只作层级，本身选不中。 */
export interface OptionGroup<T extends string | number> {
  label: string
  options: readonly Option<T>[]
}

export type SelectItem<T extends string | number> = Option<T> | OptionGroup<T>

function isGroup<T extends string | number>(item: SelectItem<T>): item is OptionGroup<T> {
  return 'options' in item
}

// 原生 select 的 props 全部透传（id / disabled / aria-* / data-* 都从这里来）。
// 这不是顺手写的：Field 注入了 aria-describedby，逐个解构又不接 ...rest 的话
// 它会被静默丢掉 —— 读屏听不到提示与错误，而且没有任何测试能发现。
interface SelectProps<T extends string | number>
  extends Omit<
    React.ComponentPropsWithoutRef<'select'>,
    'value' | 'onChange' | 'children' | 'multiple'
  > {
  value: T | null
  onChange: (value: T) => void
  /** 平铺的选项，或分组的选项（分组渲染成 optgroup） */
  options: readonly SelectItem<T>[]
  placeholder?: string
  /** 选项是数字时显式声明。不传则退回「看第一个叶子」的推断 */
  numeric?: boolean
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
  className,
  numeric,
  ...rest
}: SelectProps<T>) {
  // select 的 value 只能是字符串，数字选项在这里来回转一次。
  // 分组时得看组里的叶子，组标题自己没有 value。
  //
  // 推断在空列表时失效（leaf 是 undefined）→ onChange 会悄悄回传字符串，
  // 污染 number | null 字段。级联控件的子级下拉（父级没选时列表是空的）
  // 正好是这个形状，所以它们一律显式传 numeric。
  const leaf = options.flatMap((o) => (isGroup(o) ? o.options : [o]))[0]
  const isNumeric = numeric ?? (leaf !== undefined && typeof leaf.value === 'number')

  return (
    <div className={cn('relative', className)}>
      <select
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
        {...rest}
      >
        <option value="" disabled>
          {placeholder}
        </option>
        {options.map((o) =>
          isGroup(o) ? (
            <optgroup key={o.label} label={o.label}>
              {o.options.map((c) => (
                <option key={String(c.value)} value={String(c.value)}>
                  {c.label}
                </option>
              ))}
            </optgroup>
          ) : (
            <option key={String(o.value)} value={String(o.value)}>
              {o.label}
            </option>
          ),
        )}
      </select>
      <ChevronDown
        aria-hidden
        className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted"
      />
    </div>
  )
}

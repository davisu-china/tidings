import { cn } from '@/lib/cn'

/**
 * 完成度进度条。不用 Radix Progress：这里只需要一根 2px 的线，
 * 它的可访问性职责（aria-valuenow 那一套）自己写比包一层更清楚。
 */
export function Progress({
  value,
  className,
  label,
}: {
  /** 0–100 */
  value: number
  className?: string
  label?: string
}) {
  const clamped = Math.max(0, Math.min(100, Math.round(value)))
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={clamped}
      aria-label={label ?? '档案完成度'}
      className={cn('h-[2px] w-full bg-line-soft', className)}
    >
      <div
        className="h-full bg-accent transition-[width] duration-300"
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

import { cn } from '@/lib/cn'

/**
 * 加载指示。它是反馈不是装饰 —— 19.3 说的「全站只有印章一处动效」
 * 不适用于加载态。
 */
export function Spinner({ className }: { className?: string }) {
  return (
    <span
      role="status"
      aria-label="加载中"
      className={cn(
        'inline-block h-4 w-4 animate-spin rounded-full border-[1.5px] border-current border-t-transparent',
        className,
      )}
    />
  )
}

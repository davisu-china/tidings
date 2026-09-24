import * as React from 'react'

import { cn } from '@/lib/cn'

/**
 * 输入框：1px 线框、4px 圆角、focus 时边框转 accent。
 * 不用外发光环 —— 那是 shadcn 默认长相里最显眼的一处，与「纸」冲突。
 *
 * 字号固定 16px：iOS 上小于 16px 的输入框获得焦点时会触发整页放大。
 */
export const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<'input'>>(
  function Input({ className, ...props }, ref) {
    return (
      <input
        ref={ref}
        className={cn(
          'h-11 w-full rounded-card border border-line bg-surface px-3 text-[16px] text-ink',
          'placeholder:text-muted/70',
          'transition-colors duration-150',
          'focus:border-accent focus:outline-none',
          'disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-muted',
          'aria-[invalid=true]:border-seal',
          className,
        )}
        {...props}
      />
    )
  },
)

export const Textarea = React.forwardRef<
  HTMLTextAreaElement,
  React.ComponentProps<'textarea'>
>(function Textarea({ className, ...props }, ref) {
  return (
    <textarea
      ref={ref}
      className={cn(
        'w-full resize-none rounded-card border border-line bg-surface px-3 py-2.5',
        'text-[16px] leading-[1.9] text-ink placeholder:text-muted/70',
        'transition-colors duration-150 focus:border-accent focus:outline-none',
        'aria-[invalid=true]:border-seal',
        className,
      )}
      {...props}
    />
  )
})

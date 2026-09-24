import * as LabelPrimitive from '@radix-ui/react-label'
import * as React from 'react'

import { cn } from '@/lib/cn'

/** 表单标签。字号比正文小一档，颜色用 ink-2 —— 它是说明，不是内容。 */
export const Label = React.forwardRef<
  React.ComponentRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(function Label({ className, ...props }, ref) {
  return (
    <LabelPrimitive.Root
      ref={ref}
      className={cn('block text-[13px] font-medium text-ink-2', className)}
      {...props}
    />
  )
})

/** 字段下方的错误提示。用 muted 而不是红 —— 红色只留给印章。 */
export function FieldError({ children, id }: { children?: React.ReactNode; id?: string }) {
  if (!children) return null
  // role=alert 让读屏软件在错误出现时立刻念出来，而不是等用户再走一遍
  return (
    <p id={id} role="alert" className="mt-1.5 text-[13px] text-muted">
      {children}
    </p>
  )
}

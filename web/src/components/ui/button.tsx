import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import * as React from 'react'

import { cn } from '@/lib/cn'

// 信纸体系里按钮的几条硬规矩：
//   · 圆角 4px（rounded-card），不是 8px
//   · 没有投影，状态变化靠底色与边框
//   · focus 换成 accent 边框，不用外发光环
//   · 高度默认 44px —— iOS HIG 的最小触控目标，静默按钮也不例外
const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-card font-sans text-[15px] font-medium transition-colors duration-150 disabled:pointer-events-none disabled:opacity-45 focus-visible:outline-none focus-visible:ring-0',
  {
    variants: {
      variant: {
        default: 'bg-accent text-on-accent hover:bg-accent-ink',
        outline: 'border border-accent text-accent hover:bg-accent-soft',
        // 静默按钮：没有边框、没有底色。与 default 的视觉权重差距很大，
        // 这是有意的 —— 它承载的动作要么无关紧要，要么需要克制（比如「不合适」）
        quiet: 'text-muted hover:text-ink-2',
        ghost: 'text-ink-2 hover:bg-surface-2',
        line: 'border border-line text-ink-2 hover:border-ink-2',
      },
      size: {
        sm: 'h-9 px-3 text-[14px]',
        // 默认就是 44px：触控目标不达标是移动端最常见的体验缺陷
        default: 'h-11 px-5',
        lg: 'h-12 px-6 text-[16px]',
        icon: 'h-11 w-11',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: ButtonProps) {
  const Comp = asChild ? Slot : 'button'
  return (
    <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />
  )
}

export { buttonVariants }

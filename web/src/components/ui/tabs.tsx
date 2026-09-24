import * as TabsPrimitive from '@radix-ui/react-tabs'
import * as React from 'react'

import { cn } from '@/lib/cn'

export const Tabs = TabsPrimitive.Root

export const TabsList = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(function TabsList({ className, ...props }, ref) {
  return (
    <TabsPrimitive.List
      ref={ref}
      className={cn('flex gap-6 border-b border-line', className)}
      {...props}
    />
  )
})

/**
 * 标签。选中态用一条 2px 的 accent 底线，不加底色、不加圆角 ——
 * 它是纸上的两道折痕，不是两个按钮。
 */
export const TabsTrigger = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(function TabsTrigger({ className, ...props }, ref) {
  return (
    <TabsPrimitive.Trigger
      ref={ref}
      className={cn(
        'relative -mb-px h-11 px-0.5 text-[15px] text-muted transition-colors duration-150',
        'hover:text-ink-2 focus-visible:outline-none',
        'data-[state=active]:text-ink',
        "data-[state=active]:after:absolute data-[state=active]:after:inset-x-0",
        'data-[state=active]:after:-bottom-px data-[state=active]:after:h-[2px]',
        'data-[state=active]:after:bg-accent',
        className,
      )}
      {...props}
    />
  )
})

export const TabsContent = TabsPrimitive.Content

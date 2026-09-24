import { ChevronDown } from 'lucide-react'
import * as React from 'react'
import { createPortal } from 'react-dom'

import { cn } from '@/lib/cn'

/**
 * 底部弹层。出生日期、城市、家乡三个复合控件都走它。
 *
 * 手写而不引 @radix-ui/react-dialog：这个仓库在这一层一直是手写的
 * （toast / segmented / progress 都没有对应依赖），而这个弹层不需要动画、
 * 不需要嵌套、不需要过渡状态，要的东西一共四件 —— 焦点、Escape、滚动锁、遮罩。
 *
 * 没有入场动画，理由写在 index.css 那段注释里。
 */
export function Sheet({
  open,
  title,
  onClose,
  onConfirm,
  children,
}: {
  open: boolean
  title: string
  /** 取消：丢弃草稿。Escape、点遮罩走的是同一条路 */
  onClose: () => void
  onConfirm: () => void
  children: React.ReactNode
}) {
  const panelRef = React.useRef<HTMLDivElement>(null)
  // 打开前焦点在哪儿。关掉要还回去，否则键盘用户会被扔回文档开头。
  const restoreRef = React.useRef<HTMLElement | null>(null)

  React.useEffect(() => {
    if (!open) return
    const active = document.activeElement
    restoreRef.current = active instanceof HTMLElement ? active : null

    const first = panelRef.current?.querySelector<HTMLElement>(FOCUSABLE)
    first?.focus()

    return () => {
      // isConnected：触发它的那个按钮可能已经被卸载了（比如整屏切换）
      if (restoreRef.current?.isConnected) restoreRef.current.focus()
    }
  }, [open])

  // 背景不许滚。挂在 <html> 上而不是 body：iOS Safari 上 body 的
  // overflow: hidden 拦不住橡皮筋，手指一划底下的页面照样跟着走。
  React.useEffect(() => {
    if (!open) return
    const root = document.documentElement
    const prev = root.style.overflow
    root.style.overflow = 'hidden'
    return () => {
      root.style.overflow = prev
    }
  }, [open])

  // Escape 挂在 document 上而不是面板上：点过遮罩之后焦点会落到 body，
  // 挂在面板上的话那之后就按不动了。
  React.useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // 别让 Escape 继续冒泡：它是「关掉这一层」，
        // 不该顺带把别的东西也关掉
        e.stopPropagation()
        onClose()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  // Tab 在弹层里绕圈。不做的话 Tab 一下就跑到底下的页面去了，
  // 而弹层还盖在上面 —— 看得见的地方全都没反应。
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return
    const nodes = panelRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE)
    if (!nodes || nodes.length === 0) return
    const first = nodes[0]
    const last = nodes[nodes.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }

  return createPortal(
    <div className="fixed inset-0 z-50">
      <div className="sheet-mask" onClick={onClose} />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onKeyDown={onKeyDown}
        className="sheet-panel"
        // --overlay-shadow 没进 @theme，没有对应的工具类。toast.tsx 也是这么用的。
        style={{ boxShadow: 'var(--overlay-shadow)' }}
      >
        <div className="sheet-head">
          <button
            type="button"
            onClick={onClose}
            className={cn(
              'rounded-card px-2 py-1.5 text-[15px] text-muted',
              'transition-colors duration-150 hover:text-ink-2',
              'focus-visible:outline-none focus-visible:border focus-visible:border-accent',
            )}
          >
            取消
          </button>
          <span className="font-serif text-[15px] text-ink">{title}</span>
          <button
            type="button"
            onClick={onConfirm}
            className={cn(
              'rounded-card px-2 py-1.5 text-[15px] font-medium text-accent',
              'transition-colors duration-150 hover:text-accent-ink',
              'focus-visible:outline-none focus-visible:border focus-visible:border-accent',
            )}
          >
            完成
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}

/**
 * 打开弹层的那个按钮。长得和 ui/select.tsx 一模一样 —— 收起状态是
 * 表单里唯一的形态，滚轮只在点开之后才出现，所以静止时表单的节奏没变。
 *
 * Field 在 group 模式下**不会**给控件挂 id（fields.tsx:58），
 * 所以 id 得自己挂，aria-labelledby / aria-describedby 得自己透传 ——
 * 漏了不会有任何测试报错，只是读屏悄悄听不到标签和提示。
 */
export function SheetTrigger({
  id,
  text,
  placeholder = '请选择',
  open,
  disabled,
  onClick,
  labelledBy,
  describedBy,
}: {
  id: string
  /** 已选中的值的人话。空串 = 还没选 */
  text: string
  placeholder?: string
  open: boolean
  disabled?: boolean
  onClick: () => void
  labelledBy?: string
  describedBy?: string
}) {
  return (
    <div className="relative">
      <button
        type="button"
        id={id}
        disabled={disabled}
        onClick={onClick}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        className={cn(
          'h-11 w-full rounded-card border border-line bg-surface pl-3 pr-9 text-left',
          'text-[16px] transition-colors duration-150',
          'focus:border-accent focus:outline-none',
          'disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-muted',
          text === '' ? 'text-muted' : 'text-ink',
        )}
      >
        <span className="block truncate">{text === '' ? placeholder : text}</span>
      </button>
      <ChevronDown
        aria-hidden
        className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted"
      />
    </div>
  )
}

/** 能被 Tab 够到的东西。滚轮列是 [tabindex="0"] 的 div，所以这条也覆盖它们。 */
const FOCUSABLE =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

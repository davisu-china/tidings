import * as React from 'react'

import { cn } from '@/lib/cn'

interface ToastItem {
  id: number
  message: string
}

interface ToastApi {
  /** 提示一句话。默认 4 秒后自己退场。 */
  show: (message: string) => void
}

const ToastContext = React.createContext<ToastApi | null>(null)

export function useToast(): ToastApi {
  const ctx = React.useContext(ToastContext)
  if (!ctx) throw new Error('useToast 必须在 <ToastProvider> 内使用')
  return ctx
}

const DURATION = 4000

/**
 * 提示条。
 *
 * 有意做得比一般 toast 更安静：底部居中、surface 底、发丝线边框、
 * 没有投影、没有图标、没有红色。它承载的多半是「这一步没存上」这类信息，
 * 而信纸体系里红色的分量只留给印章。
 */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = React.useState<ToastItem[]>([])
  const nextId = React.useRef(0)
  const timers = React.useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const show = React.useCallback((message: string) => {
    const id = nextId.current++
    setItems((prev) => [...prev, { id, message }])
    timers.current.set(
      id,
      setTimeout(() => {
        setItems((prev) => prev.filter((t) => t.id !== id))
        timers.current.delete(id)
      }, DURATION),
    )
  }, [])

  // 卸载时清掉定时器，否则会在已经卸载的组件上 setState
  React.useEffect(() => {
    const pending = timers.current
    return () => {
      pending.forEach(clearTimeout)
      pending.clear()
    }
  }, [])

  const api = React.useMemo(() => ({ show }), [show])

  return (
    <ToastContext.Provider value={api}>
      {children}
      {/* aria-live 让读屏软件念出提示，视觉上它只是个安静的条 */}
      <div
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 px-4 pb-[calc(env(safe-area-inset-bottom,0px)+16px)]"
      >
        {items.map((t) => (
          <div
            key={t.id}
            className={cn(
              'pointer-events-auto max-w-[min(420px,100%)] rounded-card border border-line',
              'bg-surface px-4 py-2.5 text-[14px] leading-relaxed text-ink-2',
            )}
            style={{ boxShadow: 'var(--overlay-shadow)' }}
          >
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

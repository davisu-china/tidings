import * as React from 'react'

import { searchSchools } from '@/api/profile'
import type { SchoolItem } from '@/api/types'
import { cn } from '@/lib/cn'

/** 输入到发请求之间的等待。太短会把每个字都打成一次请求，太长打字时会顿。 */
const DEBOUNCE_MS = 180

interface SchoolSelectProps {
  value: string
  onChange: (value: string) => void
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
  disabled?: boolean
}

/**
 * 毕业院校：输入即联想，但**只能从列表里选**。
 *
 * 为什么不让自由输入：校名是拿去归一 school_tier 的入参，而 tier 参与
 * 匹配打分。手打「北大」还是「北京大学」还是「北京大学（本部）」
 * 决定的是这个人是 5 档还是 2 档 —— 同一个人的档案不该因为怎么打字
 * 而在池子里换一个位置。
 *
 * 所以输入框里的文字只是**查询词**，不是值：没选中任何一项就失焦，
 * 文字会退回上一个已提交的校名。想清空就点右边的 ×。
 *
 * 库里没有的旧值（改档前填的、或者院校库没收录的）照样回显 ——
 * 组件不认识的校名不该被它悄悄抹掉。
 */
export function SchoolSelect({
  value,
  onChange,
  id,
  disabled,
  ...aria
}: SchoolSelectProps) {
  const [text, setText] = React.useState(value)
  const [items, setItems] = React.useState<SchoolItem[]>([])
  const [open, setOpen] = React.useState(false)
  const [active, setActive] = React.useState(0)
  const [failed, setFailed] = React.useState(false)
  const [loading, setLoading] = React.useState(false)

  const boxRef = React.useRef<HTMLDivElement>(null)
  const listId = `${id ?? 'school_name'}-list`

  // 已提交的值。回退时用它，而不是闭包里的 value —— 那些 setTimeout
  // 与事件回调拿到的可能是上一轮的 value。
  const committed = React.useRef(value)

  // 外部改了值（回填、提交后重置）时把输入框同步过来
  React.useEffect(() => {
    committed.current = value
    setText(value)
  }, [value])

  // 输入防抖。中断上一次请求：慢的那个后到会把新结果盖掉
  React.useEffect(() => {
    const q = text.trim()
    if (!open || q === '' || q === value) {
      setItems([])
      return
    }

    const ac = new AbortController()
    const timer = window.setTimeout(() => {
      setLoading(true)
      searchSchools(q, 10, ac.signal)
        .then((res) => {
          setItems(res.items)
          setActive(0)
          setFailed(false)
          setLoading(false)
        })
        .catch((err: unknown) => {
          // 院校库查不到不该让整个表单不可用：它是选填项，
          // 而且用户已经选过的值还在。静默降级成「没结果」。
          if (!(err instanceof DOMException && err.name === 'AbortError')) {
            setItems([])
            setFailed(true)
            setLoading(false)
          }
        })
    }, DEBOUNCE_MS)

    return () => {
      window.clearTimeout(timer)
      ac.abort()
    }
  }, [text, open, value])

  // 点到外面就收起，并把没选中的输入退回已提交的值
  React.useEffect(() => {
    if (!open) return
    function onDocDown(e: MouseEvent) {
      if (!boxRef.current?.contains(e.target as Node)) {
        setOpen(false)
        setText(committed.current)
      }
    }
    document.addEventListener('mousedown', onDocDown)
    return () => document.removeEventListener('mousedown', onDocDown)
  }, [open])

  function commit(name: string) {
    committed.current = name
    onChange(name)
    setText(name)
    setOpen(false)
    setItems([])
  }

  /**
   * 失焦也要退回。不然用 Tab 走开会把没选中的查询词留在框里 ——
   * 看着像填了，提交上去的还是旧值。
   *
   * 延迟一点再退：手机上点选项是 blur 先到、click 后到，
   * 立刻退回会在那一下点击落地之前把列表卸掉，点了等于没点。
   * 焦点还在本组件里（mousedown 已经 preventDefault，输入框不会失焦）
   * 就说明是点选项，不退回。
   */
  function onBlur() {
    window.setTimeout(() => {
      const el = document.activeElement
      if (el && boxRef.current?.contains(el)) return
      setOpen(false)
      setText(committed.current)
    }, 150)
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      setOpen(false)
      setText(value)
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (items.length === 0) return
      e.preventDefault()
      setOpen(true)
      setActive((i) => {
        const next = e.key === 'ArrowDown' ? i + 1 : i - 1
        return (next + items.length) % items.length
      })
      return
    }
    if (e.key === 'Enter') {
      if (open && items[active]) {
        e.preventDefault() // 别让 Enter 顺手把表单提交了
        commit(items[active].name)
      }
    }
  }

  return (
    <div ref={boxRef} className="relative">
      <input
        id={id}
        type="text"
        role="combobox"
        aria-expanded={open && items.length > 0}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={
          open && items[active] ? `${listId}-${active}` : undefined
        }
        aria-invalid={aria['aria-invalid']}
        aria-describedby={aria['aria-describedby']}
        disabled={disabled}
        value={text}
        autoComplete="off"
        placeholder="输入校名，如 复旦 / fudan / fd"
        onChange={(e) => {
          setText(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={onBlur}
        onKeyDown={onKeyDown}
        className={cn(
          'h-11 w-full rounded-card border border-line bg-surface px-3 text-[16px] text-ink',
          'placeholder:text-muted/70 transition-colors duration-150',
          'focus:border-accent focus:outline-none',
          'disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-muted',
          'aria-[invalid=true]:border-seal',
          value !== '' && 'pr-9',
        )}
      />

      {/* 清空。学校是选填项，填了之后必须能撤回空 */}
      {value !== '' && !disabled && (
        <button
          type="button"
          aria-label="清空毕业院校"
          onClick={() => commit('')}
          className={cn(
            'absolute right-2 top-1/2 grid h-6 w-6 -translate-y-1/2 place-items-center',
            'rounded-full text-muted transition-colors hover:bg-surface-2 hover:text-ink',
            'focus-visible:outline-none focus-visible:bg-accent-soft',
          )}
        >
          <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden>
            <path
              d="M4 4l8 8M12 4l-8 8"
              stroke="currentColor"
              strokeWidth="1.6"
              strokeLinecap="round"
            />
          </svg>
        </button>
      )}

      {/* 有结果、或者确定没结果（查完了）才弹面板。查的过程中挂着的是一个
          没有子元素的 ul —— 那会是一道 2px 高的空边框，每敲一个字闪一下，
          看着像坏了。等待期间什么都不显示才是对的。 */}
      {open && text.trim() !== '' && text.trim() !== value && (items.length > 0 || !loading) && (
        <ul
          id={listId}
          role="listbox"
          className={cn(
            'absolute z-20 mt-1 max-h-64 w-full overflow-y-auto',
            'rounded-card border border-line bg-surface shadow-lg',
          )}
        >
          {items.map((s, i) => (
            <li
              key={s.name}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === active}
              onMouseEnter={() => setActive(i)}
              // 用 mousedown 而不是 click：click 要等鼠标抬起，
              // 而输入框的 blur 已经先把列表收起来了
              onMouseDown={(e) => {
                e.preventDefault()
                commit(s.name)
              }}
              className={cn(
                'cursor-pointer px-3 py-2.5 text-[15px]',
                i === active ? 'bg-accent-soft text-ink' : 'text-ink-2',
              )}
            >
              {s.name}
            </li>
          ))}

          {/* 查的过程中什么都不说：立刻写「没有匹配」会在每次按键后
              闪一下，而那多半只是请求还没回来 */}
          {items.length === 0 && !loading && (
            <li className="px-3 py-2.5 text-[14px] text-muted">
              {failed ? '院校库暂时查不了，稍后再试' : '没有匹配的学校'}
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

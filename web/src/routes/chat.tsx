import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, SendHorizontal } from 'lucide-react'
import * as React from 'react'
import { Link, useLoaderData, useParams, type LoaderFunctionArgs } from 'react-router'

import {
  fetchMessages,
  markRead,
  sendMessage,
  type Message,
  type MessageList,
} from '@/api/conversation'
import { useToast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { messageOf } from '@/lib/errors'

/** 与后端 msgMaxRunes 一致。按「字」算，不按字节 —— 一个汉字是一个字。 */
const MSG_MAX = 1000
/** 往回翻一页的条数，与后端 msgPageDefault 一致。 */
const PAGE = 30

export async function chatLoader({ params }: LoaderFunctionArgs) {
  const id = Number(params.id)
  if (!Number.isInteger(id) || id <= 0) return { messages: null as MessageList | null }
  try {
    return { messages: await fetchMessages(id) }
  } catch {
    // 不是当事人和不存在给的是同一个 404（与引荐的反枚举规则一致）
    return { messages: null as MessageList | null }
  }
}

/** 按字计数。`str.length` 数的是 UTF-16 码元，emoji 会被算成两个。 */
function runeLength(s: string): number {
  return Array.from(s).length
}

function newClientMsgID(): string {
  // 幂等键要跨重试稳定，所以要一次生成、跟着这次发送走到底。
  // randomUUID 只在安全上下文里有（localhost 算安全上下文）。
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
}

function timeOf(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function dayOf(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const sameYear = d.getFullYear() === now.getFullYear()
  const md = `${d.getMonth() + 1} 月 ${d.getDate()} 日`
  return sameYear ? md : `${d.getFullYear()} 年 ${md}`
}

/**
 * 会话（§19.7）。
 *
 * 纯文字、1–1000 字、实时收发。收走 WebSocket（别人做了什么），
 * 发走 HTTP POST —— 发消息需要鉴权、内容校验、幂等键和错误码，
 * 这些都不该在长连接上再实现一遍。
 */
export function ChatPage() {
  const { messages: initial } = useLoaderData() as { messages: MessageList | null }
  const { id } = useParams()
  const matchID = Number(id)

  if (!initial) return <MissingChat />

  return <Chat matchID={matchID} initial={initial} />
}

function MissingChat() {
  return (
    <div className="mx-auto w-full max-w-[640px] px-5 py-6">
      <BackLink />
      <p className="mt-16 text-center font-serif text-[15px] leading-[1.9] text-muted">
        这条会话不在了。
      </p>
    </div>
  )
}

function Chat({ matchID, initial }: { matchID: number; initial: MessageList }) {
  const toast = useToast()
  const key = ['messages', matchID] as const

  const { data } = useQuery({
    queryKey: key,
    queryFn: () => fetchMessages(matchID),
    initialData: initial,
  })

  // 更早的那几页单独放着，不进 query 缓存。
  //
  // 缓存里那条 query 是「最新一页」，实时事件一来就会失效重取；
  // 把历史也塞进去的话，每来一条消息都要把翻过的几页一起重取一遍。
  const [older, setOlder] = React.useState<Message[]>([])
  const [hasMore, setHasMore] = React.useState(initial.has_more)
  const [loadingOlder, setLoadingOlder] = React.useState(false)

  // 合并时要按 id 去重：最新一页是「最后 N 条」，对方多发几条之后，
  // 窗口往后挪，上次翻出来的那批就会和窗口出现重叠。
  const messages = React.useMemo(() => {
    const seen = new Set<number>()
    const out: Message[] = []
    for (const m of [...older, ...data.messages]) {
      if (m.id > 0) {
        if (seen.has(m.id)) continue
        seen.add(m.id)
      }
      out.push(m)
    }
    return out
  }, [older, data.messages])

  const lastID = messages.length > 0 ? messages[messages.length - 1].id : 0
  const ended = data.other.status !== 'active'

  const scrollRef = useScrollToBottom(lastID)
  useMarkRead(matchID, data.other.unread, lastID)

  async function loadOlder() {
    const first = messages.find((m) => m.id > 0)
    if (!first || loadingOlder) return
    setLoadingOlder(true)
    try {
      const page = await fetchMessages(matchID, first.id, PAGE)
      setOlder((prev) => [...page.messages, ...prev])
      setHasMore(page.has_more)
    } catch (err) {
      toast.show(messageOf(err))
    } finally {
      setLoadingOlder(false)
    }
  }

  return (
    <div
      className={cn(
        'mx-auto flex w-full max-w-[640px] flex-col',
        // 底部 tab 占掉的 56px 与 iPhone 的横条都要减掉，聊天区才能自己滚
        'h-[calc(100dvh-56px-env(safe-area-inset-bottom,0px))] lg:h-dvh',
      )}
    >
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3">
        <BackLink />
        <span className="truncate font-serif text-[16px] text-ink">
          {data.other.nickname ?? '这位朋友'}
        </span>
      </header>

      <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-4">
        {hasMore && (
          <div className="mb-4 flex justify-center">
            <button
              type="button"
              onClick={loadOlder}
              disabled={loadingOlder}
              className="min-h-[44px] px-3 text-[13px] text-muted transition-colors duration-150 hover:text-ink-2 disabled:opacity-45"
            >
              {loadingOlder ? '正在取…' : '看更早的'}
            </button>
          </div>
        )}

        <div className="flex flex-col gap-3">
          {messages.map((m, i) => {
            const prev = messages[i - 1]
            const showDay = !prev || dayOf(prev.created_at) !== dayOf(m.created_at)
            return (
              <React.Fragment key={m.id > 0 ? m.id : m.client_msg_id}>
                {showDay && (
                  <div className="my-2 text-center font-mono text-[11px] text-muted">
                    {dayOf(m.created_at)}
                  </div>
                )}
                <Bubble message={m} />
              </React.Fragment>
            )
          })}
        </div>
      </div>

      {ended ? (
        <p className="shrink-0 border-t border-line px-4 py-4 text-center text-[13px] text-muted">
          对话已结束。
        </p>
      ) : (
        <Composer matchID={matchID} />
      )}
    </div>
  )
}

function BackLink() {
  return (
    <Link
      to="/matches"
      aria-label="返回会话列表"
      className="inline-flex h-11 w-11 shrink-0 items-center justify-center text-muted transition-colors duration-150 hover:text-ink-2"
    >
      <ChevronLeft aria-hidden className="h-4 w-4" strokeWidth={1.5} />
    </Link>
  )
}

/**
 * 一条消息。
 *
 * 自己的靠右、用 accent 底；对方的靠左、用 surface 底加发丝线。
 * 不用气泡尾巴、不用圆角气泡 —— 圆角是这一套设计里要避开的东西。
 */
function Bubble({ message }: { message: Message }) {
  const pending = message.id <= 0

  return (
    <div className={cn('flex', message.mine ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'max-w-[78%] rounded-card px-3.5 py-2.5 text-[15px] leading-[1.7] whitespace-pre-wrap break-words',
          message.mine
            ? 'bg-accent-soft text-ink'
            : 'border border-line bg-surface text-ink',
          pending && 'opacity-60',
        )}
      >
        {message.content}
        <span className="tnum mt-1 block font-mono text-[10px] text-muted">
          {pending ? '发送中' : timeOf(message.created_at)}
        </span>
      </div>
    </div>
  )
}

/**
 * 输入区。
 *
 * 回车发送只在指针精确的设备上生效（也就是有实体键盘的）：手机上
 * 回车键就是换行键，抢过来当发送用会让想分行的人没法分行。手机上
 * 发消息点右边的按钮。
 */
function Composer({ matchID }: { matchID: number }) {
  const qc = useQueryClient()
  const toast = useToast()
  const key = ['messages', matchID] as const

  const [text, setText] = React.useState('')
  const ref = React.useRef<HTMLTextAreaElement>(null)
  const [finePointer] = React.useState(
    () => typeof window !== 'undefined' && window.matchMedia('(pointer: fine)').matches,
  )

  const count = runeLength(text)
  const tooLong = count > MSG_MAX
  const canSend = text.trim().length > 0 && !tooLong

  const send = useMutation({
    mutationFn: ({ clientMsgID, content }: { clientMsgID: string; content: string }) =>
      sendMessage(matchID, clientMsgID, content),

    onMutate: async ({ clientMsgID, content }) => {
      // 先停掉在飞的重取：它们回来时会把这条乐观消息冲掉
      await qc.cancelQueries({ queryKey: key })
      const prev = qc.getQueryData<MessageList>(key)

      const optimistic: Message = {
        id: 0,
        match_id: matchID,
        sender_id: 0,
        mine: true,
        content,
        client_msg_id: clientMsgID,
        created_at: new Date().toISOString(),
      }
      qc.setQueryData<MessageList>(key, (old) =>
        old ? { ...old, messages: [...old.messages, optimistic] } : old,
      )
      return { prev }
    },

    onSuccess: (saved, { clientMsgID }) => {
      // 用服务端那行换掉乐观的那行。按 client_msg_id 对，不按内容 ——
      // 同一个内容连发两条是完全正常的事。
      qc.setQueryData<MessageList>(key, (old) => {
        if (!old) return old
        const idx = old.messages.findIndex((m) => m.client_msg_id === clientMsgID)
        const messages = [...old.messages]
        if (idx >= 0) messages[idx] = saved
        else messages.push(saved)
        return { ...old, messages }
      })
      // 会话列表上的「最后一条」和未读数都变了
      void qc.invalidateQueries({ queryKey: ['matches'] })
    },

    onError: (err, _vars, ctx) => {
      // 回到发送前。留着那条半透明的气泡会让人以为发出去了。
      if (ctx?.prev !== undefined) qc.setQueryData(key, ctx.prev)
      toast.show(messageOf(err))
    },
  })

  function submit() {
    if (!canSend || send.isPending) return
    const content = text.trim()
    // 幂等键在这里生成、跟着这次内容走到底。重试要复用它，否则
    // 服务端会当成两条新消息各存一次。
    send.mutate({ clientMsgID: newClientMsgID(), content })
    setText('')
    ref.current?.focus()
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key !== 'Enter' || e.shiftKey || !finePointer) return
    // 输入法候选框里的回车不算发送 —— 中文输入时那是在选字
    if (e.nativeEvent.isComposing) return
    e.preventDefault()
    submit()
  }

  // 跟着内容长高，最多五行。超出的部分自己滚。
  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 132)}px`
  }, [text])

  return (
    <div className="shrink-0 border-t border-line px-4 py-3">
      <div className="flex items-end gap-2">
        <textarea
          ref={ref}
          value={text}
          rows={1}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="说点什么"
          aria-label="消息内容"
          className={cn(
            'min-h-[44px] flex-1 resize-none rounded-card border bg-surface px-3.5 py-3',
            'text-[15px] leading-[1.6] text-ink placeholder:text-muted',
            'focus:outline-none focus-visible:border-accent',
            tooLong ? 'border-seal' : 'border-line',
          )}
        />

        <button
          type="button"
          onClick={submit}
          disabled={!canSend || send.isPending}
          aria-label="发送"
          className={cn(
            'inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-card',
            'bg-accent text-on-accent transition-colors duration-150 hover:bg-accent-ink',
            'disabled:pointer-events-none disabled:opacity-45',
          )}
        >
          <SendHorizontal aria-hidden className="h-[18px] w-[18px]" strokeWidth={1.5} />
        </button>
      </div>

      {/* 只在快满和超了的时候出现。常驻一个「0/1000」是在提醒用户
          他被数着 —— 一条消息本来就该是短的 */}
      {count > MSG_MAX - 100 && (
        <p className={cn('tnum mt-2 text-right font-mono text-[11px]', tooLong ? 'text-seal' : 'text-muted')}>
          {count} / {MSG_MAX}
        </p>
      )}
    </div>
  )
}

/** 有新消息就把视图带到底部。挂到那个可滚动的容器上。 */
function useScrollToBottom(lastID: number) {
  const ref = React.useRef<HTMLDivElement | null>(null)

  React.useEffect(() => {
    // 用 scrollTop 而不是 scrollIntoView：后者会连带把整页也滚一下，
    // 而这个页面本来就不该整页滚动。
    const el = ref.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [lastID])

  return ref
}

/**
 * 把已读水位推上去。角标要的数字只能由「读到哪了」算出来。
 *
 * 这一页每收到一条消息都会重取一次消息列表（实时事件把 query 作废了），
 * 所以这个 effect 会被反复触发；用 ref 记住上次报的 id，同一条只报一次。
 */
function useMarkRead(matchID: number, unread: number, lastID: number) {
  const qc = useQueryClient()
  const marked = React.useRef(0)

  React.useEffect(() => {
    if (unread <= 0 || lastID <= 0 || lastID === marked.current) return
    marked.current = lastID
    void markRead(matchID, lastID)
      .then(() => qc.invalidateQueries({ queryKey: ['matches'] }))
      // 报不上去不该弹错：角标多亮一会儿而已，下次进来自会补上
      .catch(() => undefined)
  }, [matchID, unread, lastID, qc])
}

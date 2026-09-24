import type { QueryClient } from '@tanstack/react-query'

import { fetchWSTicket } from '@/api/conversation'
import { hasSession } from '@/lib/token'

// 站内实时通道的浏览器端（后端见 internal/ws/hub.go）。
//
// 只收不发：发消息走 HTTP POST，这条连接只用来知道「别人做了什么」。
// 收到事件之后前端不自己算新状态，而是把相关的 query 作废、让它们重新
// 拉一次 —— 未读数、消息列表的合并顺序都有服务端算好的一份，
// 在客户端再算一遍迟早会和服务端不一致，而不一致的表现是
// 「角标点不掉」这种没有报错、只能靠用户投诉才发现的问题。

const WS_PATH = '/api/v1/ws'

/**
 * 重连退避。
 *
 * 第一档给得短（电话切走、地铁进站，一秒内就回来了），后面封顶 30 秒：
 * 后端重启时几十个客户端同时重连，间隔再短也只会一起撞上去。
 * 网络恢复和页面切回前台会直接把退避清零，不用等它爬到顶。
 */
const BACKOFF_MS = [1000, 2000, 5000, 10000, 30000]

/** 后端推下来的帧（internal/ws/hub.go 的 frame）。 */
interface Frame {
  type: string
  data?: unknown
}

/**
 * 建立并维持一条实时连接，返回一个断开函数。
 *
 * 由 AppShell 在挂载时调用、卸载时断开 —— 也就是「登录之后才有」，
 * 登出时组件卸载，连接跟着关掉，不必自己判断会话还在不在。
 */
export function connectRealtime(qc: QueryClient): () => void {
  let closed = false
  let sock: WebSocket | null = null
  let timer: number | null = null
  let attempt = 0

  function clearTimer() {
    if (timer !== null) {
      window.clearTimeout(timer)
      timer = null
    }
  }

  function schedule() {
    if (closed || timer !== null) return
    const delay = BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)]
    attempt += 1
    timer = window.setTimeout(() => {
      timer = null
      void open()
    }, delay)
  }

  async function open() {
    if (closed || sock) return

    // 票据 60 秒有效、且只能用一次（服务端握手时 GETDEL 掉）。
    // 所以是「每次连接前换一张」，不能取一张存起来反复用 ——
    // 存起来的那张在第一次重连时就已经是废纸了。
    let ticket: string
    try {
      ticket = (await fetchWSTicket()).ticket
    } catch {
      // 换不到票通常是网络或后端不可用。会话真的失效时 client.ts
      // 自己会把人送回登录页，这里只管接着退避重试。
      schedule()
      return
    }
    if (closed) return

    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${scheme}//${window.location.host}${WS_PATH}?ticket=${encodeURIComponent(ticket)}`

    let ws: WebSocket
    try {
      ws = new WebSocket(url)
    } catch {
      // 构造失败（url 非法、浏览器禁用了 WebSocket）——重试也不会变好，
      // 但也不能就此放弃：多半是被某个扩展临时拦了。
      schedule()
      return
    }
    sock = ws

    ws.onopen = () => {
      attempt = 0
    }

    ws.onmessage = (ev) => {
      if (typeof ev.data !== 'string') return
      let frame: Frame
      try {
        frame = JSON.parse(ev.data) as Frame
      } catch {
        return
      }
      onEvent(qc, frame)
    }

    // onerror 之后一定会跟一个 onclose，所以只在这里安排重连，
    // 两处都写会让每次断开重连两次。
    ws.onclose = () => {
      if (sock === ws) sock = null
      schedule()
    }
  }

  /** 网络回来、页面切回前台时立刻重连，不等退避爬完。 */
  function wake() {
    if (closed) return
    if (!hasSession()) return
    if (sock && sock.readyState === WebSocket.OPEN) return
    attempt = 0
    clearTimer()
    // 半开连接（readyState 还是 OPEN 但对面早没了）交给心跳兜底：
    // 后端的 ping 每 30 秒一次，两次没应答就会被关掉，然后走 onclose。
    if (sock && sock.readyState === WebSocket.CONNECTING) return
    if (sock) {
      sock.close()
      return
    }
    void open()
  }

  window.addEventListener('online', wake)
  document.addEventListener('visibilitychange', onVisible)

  function onVisible() {
    if (document.visibilityState === 'visible') wake()
  }

  void open()

  return () => {
    closed = true
    clearTimer()
    window.removeEventListener('online', wake)
    document.removeEventListener('visibilitychange', onVisible)
    const ws = sock
    sock = null
    ws?.close()
  }
}

/**
 * 一条事件该让哪些缓存失效。
 *
 * 故意不做局部合并（把新消息 append 进列表）：消息列表是按游标翻页的，
 * 客户端插进去的那一条和下一次翻页拿到的会有重叠，去重的逻辑要写两遍
 * 还得跟服务端的排序保持一致。作废重拉是慢一点点，但只有一份真相。
 */
function onEvent(qc: QueryClient, frame: Frame) {
  const data = (frame.data ?? {}) as { match_id?: number; intro_id?: number }

  switch (frame.type) {
    case 'message':
      // 会话列表要动的是「最后一条」和未读数，所以它一起作废
      void qc.invalidateQueries({ queryKey: ['matches'] })
      if (data.match_id) {
        void qc.invalidateQueries({ queryKey: ['messages', data.match_id] })
      }
      break

    case 'match':
      // 成匹配对列表、引荐列表、以及正开着的那张引荐卡都是新的
      void qc.invalidateQueries({ queryKey: ['matches'] })
      void qc.invalidateQueries({ queryKey: ['introductions'] })
      if (data.intro_id) {
        void qc.invalidateQueries({ queryKey: ['intro', data.intro_id] })
      }
      break

    default:
      // 认不出来的类型直接忽略：新版本后端可能加了事件，旧前端
      // 装作没看见就行，不该整页报错
      break
  }
}

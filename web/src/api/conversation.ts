import { api } from './client'

// 与 internal/service/conversation.go 的 json tag 一一对应。

export type MatchStatus = 'active' | 'ended'

export interface Match {
  match_id: number
  other_id: number
  status: MatchStatus
  nickname: string | null
  cover_url: string
  created_at: string
  /** 最后一条消息的时刻。一条都没发过时是 null，列表按这个排序 */
  last_msg_at: string | null
  last_content: string | null
  /** 最后一条是不是我发的。列表上要写「我：」 */
  last_mine: boolean | null
  unread: number
}

export interface MatchList {
  matches: Match[]
  /** 所有会话的未读之和。底部导航的角标用它，省得前端自己加 */
  unread: number
}

export interface Message {
  id: number
  match_id: number
  sender_id: number
  mine: boolean
  content: string
  /**
   * 发送方的本地幂等键。实时通道把消息原样推给双方，发送方那一份
   * 会用乐观插入的那条对上它 —— 前端靠这个去重，不靠 content 比对。
   */
  client_msg_id: string
  created_at: string
}

export interface MessageList {
  /** 一定按时间升序。游标往回翻出来的那一页在服务端就翻好了 */
  messages: Message[]
  /** 更早的还有没有。false 就到头了，不必再往上滚 */
  has_more: boolean
  other: Match
}

export interface WSTicket {
  ticket: string
  expires_at: string
}

export function fetchMatches(signal?: AbortSignal): Promise<MatchList> {
  return api.get<MatchList>('/matches', { signal })
}

/**
 * 取会话消息。
 *
 * before 是一条消息 id（不含），往回翻一页。不传就是最新的一页 ——
 * 服务端只认游标，没有 offset：会话是一直在长的，用编号分页会在
 * 有人发新消息时把同一条消息翻出来两次。
 */
export function fetchMessages(
  matchId: number,
  before?: number,
  limit?: number,
  signal?: AbortSignal,
): Promise<MessageList> {
  const q = new URLSearchParams()
  if (before) q.set('before', String(before))
  if (limit) q.set('limit', String(limit))
  const qs = q.toString()
  return api.get<MessageList>(`/matches/${matchId}/messages${qs ? `?${qs}` : ''}`, { signal })
}

/**
 * 发一条消息。
 *
 * clientMsgId 必填且由前端生成：网络超时后重发同一个 id，服务端会把
 * 第一次那行原样还回来，而不是又存一条。这个键同时是实时去重的依据，
 * 所以重发必须复用同一个值，不能每次重试重新生成。
 */
export function sendMessage(
  matchId: number,
  clientMsgId: string,
  content: string,
): Promise<Message> {
  return api.post<Message>(`/matches/${matchId}/messages`, {
    client_msg_id: clientMsgId,
    content,
  })
}

/**
 * 推进已读水位。
 *
 * §18.3 的接口表里没有这一条，但 §19.7 要求会话列表有未读角标、
 * 库里也有 match_reads 这张表 —— 少了它角标永远点不掉。做成 POST
 * 而不是塞进上面那个 GET：GET 每次预取都会跑，让一个读接口去写
 * 水位，等于滚一下列表就把所有会话标成已读。
 *
 * lastMsgId 可以省略，服务端会按当前最后一条封顶；传了超过末尾的值
 * 也会被夹回去，免得客户端把一个未来的水位钉死在库里。
 */
export function markRead(
  matchId: number,
  lastMsgId?: number,
): Promise<{ last_read_msg_id: number }> {
  return api.post<{ last_read_msg_id: number }>(
    `/matches/${matchId}/read`,
    lastMsgId ? { last_msg_id: lastMsgId } : {},
  )
}

/**
 * 换一张实时通道的票据。
 *
 * 60 秒有效、且只够用一次（服务端签完记一个键，握手时 GETDEL 消费掉）。
 * 所以每次重连都要重新换一张，不能缓存起来反复用。
 */
export function fetchWSTicket(): Promise<WSTicket> {
  return api.post<WSTicket>('/auth/ws-ticket')
}

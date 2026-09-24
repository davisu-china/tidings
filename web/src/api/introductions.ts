import { api } from './client'

/**
 * 引荐列表里对方的信息。这是一个白名单而不是档案的投影 ——
 * 后端只给引荐卡上要用的字段（§19.2），学校、单位、体重这些
 * 卡上不出现的项压根不下发，前端也就不可能误显示。
 */
export interface IntroPerson {
  user_id: number
  nickname: string | null
  birth_ym: number | null
  city_code: number | null
  education_level: number | null
  height_cm: number | null
  income_band: number | null
  intro: string
  /**
   * 职业。只有详情页会下发（§4.6 的展示清单里有它，卡片草图上没有），
   * 所以列表里这个键根本不出现 —— 用可选而不是 `| null` 来标这件事。
   */
  occupation?: string
  cover_url: string
}

export type IntroKind = 'paired' | 'oneway'
export type IntroState = 'pending' | 'viewed' | 'responded' | 'matched' | 'declined' | 'expired'

export type IntroAction = 'like' | 'pass'
/** 「不合适」的原因（决策 07：三选一必填）。中文文案在 lib/dict.ts。 */
export type PassReason = 'mismatch' | 'vibe' | 'other'

export interface Introduction {
  id: number
  issue_no: number
  kind: IntroKind
  state: IntroState
  created_at: string
  expires_at: string
  viewed: boolean
  my_action: IntroAction | null
  other: IntroPerson
}

export interface IntroList {
  introductions: Introduction[]
  /** 没看过的条数。没有推送权限的人靠它兜底（§19.5） */
  unread: number
}

/**
 * 一次表态的结果。
 *
 * 不回整张引荐卡 —— 表态之后前端要的只有这三件事。`match_id` 只在
 * matched 时有值，也是重放时唯一还需要服务端告诉我们的东西（那条
 * 会话的 id 可能丢在了上一次丢失的响应里）。
 */
export interface RespondResult {
  intro_id: number
  state: IntroState
  my_action: IntroAction
  matched: boolean
  match_id?: number
}

export function fetchIntroductions(signal?: AbortSignal): Promise<IntroList> {
  return api.get<IntroList>('/introductions', { signal })
}

/**
 * 取一条引荐的详情。
 *
 * 这个 GET 有副作用：它会把「打开」这件事落下来（§13.1 的 pending → viewed，
 * 72 小时的时钟就此停住）。所以它只能在详情页调用，绝不能拿去做预取。
 */
export function fetchIntroduction(id: number, signal?: AbortSignal): Promise<Introduction> {
  return api.get<Introduction>(`/introductions/${id}`, { signal })
}

/**
 * 表态。reason 只有 pass 需要，且必填 —— 后端会拒掉没有原因的 pass。
 */
export function respondIntroduction(
  id: number,
  action: IntroAction,
  reason?: PassReason,
): Promise<RespondResult> {
  const body = action === 'pass' ? { action, reason } : { action }
  return api.post<RespondResult>(`/introductions/${id}/respond`, body)
}

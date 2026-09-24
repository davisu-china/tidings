import { api } from './client'

/**
 * VAPID 公钥。
 *
 * 这个接口在 /push/public-key，不在 /me 下面、也不需要登录 ——
 * 安装引导里就要用到它，而那个时机用户可能还没进到已登录的页面。
 * 未配置密钥的环境返回空串（不是错误），调用方据此安静地跳过订阅。
 */
export async function fetchPushPublicKey(): Promise<string> {
  const res = await api.get<{ public_key: string }>('/push/public-key')
  return res.public_key
}

/** 浏览器 PushSubscription.toJSON() 的形状，逐字段对应后端要的三样东西。 */
export interface PushSubscribeInput {
  endpoint: string
  keys: { p256dh: string; auth: string }
}

/**
 * 登记订阅。
 *
 * 路径是 /push/subscribe，没有 /me 那一段 —— 接口本身要登录，
 * 但路由挂在无前缀的鉴权组上（见后端 router.go）。这里曾经写成
 * /me/push/subscribe，那是一个 404，而且因为 syncPushSubscription
 * 是静默失败的，它一直没发出声音。
 */
export function subscribePush(input: PushSubscribeInput): Promise<{ subscribed: boolean }> {
  return api.post<{ subscribed: boolean }>('/push/subscribe', input)
}

/**
 * 退订。
 *
 * endpoint 走请求体：它是一条最长 1024 字符、含大量 / 与 = 的完整 URL。
 * 这是本项目唯一一个带 body 的 DELETE（见 client.ts 的说明）。
 */
export function unsubscribePush(endpoint: string): Promise<{ subscribed: boolean }> {
  return api.del<{ subscribed: boolean }>('/push/subscribe', { endpoint })
}

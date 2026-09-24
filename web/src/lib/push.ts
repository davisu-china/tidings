import { fetchPushPublicKey, subscribePush, unsubscribePush } from '@/api/push'

/**
 * Web Push 的浏览器侧。
 *
 * 这一层只管「浏览器能不能订阅、订没订上」，不管文案也不管界面 ——
 * 装了没有、要不要引导去装，是页面的事（§19.5）。
 *
 * 三条贯穿全篇的事实：
 *
 *  1. iOS 上 PushManager 只在「已添加到主屏幕」的 Web App 里存在。
 *     标签页里它压根不在 window 上，所以这不是权限问题，
 *     是能力问题 —— 再怎么请求都没用，只能引导去装。
 *  2. 订阅属于浏览器，不属于账号。同一个人换账号登录、或者同一个
 *     浏览器换个人用，浏览器手里的那条订阅是同一个。归属关系由
 *     服务端的 upsert 按 endpoint 改绑（见 repo.UpsertPushSub），
 *     所以每次登录后要重新登记一次，见 syncPushSubscription。
 *  3. 开发环境下 Vite 不注册 Service Worker（vite.config.ts 里
 *     devOptions.enabled 为 false），navigator.serviceWorker.ready
 *     会永远挂着不 resolve。所以这里一律用带超时的取注册，
 *     绝不用裸的 ready。
 */

/** iOS 上的非标准字段：主屏幕启动时为 true。 */
interface StandaloneNavigator extends Navigator {
  standalone?: boolean
}

export function isIOS(): boolean {
  const ua = navigator.userAgent
  if (/iPad|iPhone|iPod/.test(ua)) return true
  // iPadOS 13 起桌面版 Safari 的 UA 是 Macintosh，和真的 Mac 只差
  // 触点数 —— 这是官方推荐的判法，没有别的可靠信号。
  return ua.includes('Macintosh') && navigator.maxTouchPoints > 1
}

/** 是不是从主屏幕图标启动的（而不是标签页）。 */
export function isStandalone(): boolean {
  if (window.matchMedia('(display-mode: standalone)').matches) return true
  return (navigator as StandaloneNavigator).standalone === true
}

export function pushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window
}

export function notificationPermission(): NotificationPermission | 'unsupported' {
  if (!('Notification' in window)) return 'unsupported'
  return Notification.permission
}

/**
 * 取一个已经激活的 Service Worker 注册，等它装好。
 *
 * 超时不是防御性编程：开发环境确实没有 SW，而 ready 是个永不 resolve
 * 的 Promise。不加超时的话，设置页上的开关会一直转圈，看起来像卡死了 ——
 * 而这恰恰是开发时最常见的情形。
 *
 * 只有「要新建订阅」时才需要等：首次访问时 SW 可能正在安装，
 * 那一刻 subscribe 会失败，而用户刚点完按钮，重来一次很别扭。
 */
async function activeRegistration(timeoutMs = 4000): Promise<ServiceWorkerRegistration | null> {
  const existing = await navigator.serviceWorker.getRegistration()
  if (existing?.active) return existing

  return Promise.race([
    navigator.serviceWorker.ready,
    new Promise<null>((resolve) => setTimeout(() => resolve(null), timeoutMs)),
  ])
}

/**
 * 只在已经激活时取注册，不等。
 *
 * 用于「处理一条已经存在的订阅」：订阅必须先有激活的 SW 才可能存在，
 * 所以没有激活的注册就等于没有订阅，直接返回。这条路径上等一秒都是白等 ——
 * 而它挂在退出登录里，开发环境没有 SW，等待会变成点「退出」之后卡四秒。
 */
async function readyRegistration(): Promise<ServiceWorkerRegistration | null> {
  const existing = await navigator.serviceWorker.getRegistration()
  return existing?.active ? existing : null
}

/**
 * VAPID 公钥 → 订阅时用的字节串。
 *
 * 公钥是 base64url 编码的，而 applicationServerKey 要的是原始字节。
 * 补齐 padding 再换回标准 base64 才能交给 atob。
 */
function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const raw = atob(padded.replace(/-/g, '+').replace(/_/g, '/'))
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  return bytes
}

function sameKey(existing: ArrayBuffer | null, ours: Uint8Array<ArrayBuffer>): boolean {
  if (!existing) return false
  const a = new Uint8Array(existing)
  if (a.length !== ours.length) return false
  for (let i = 0; i < a.length; i++) if (a[i] !== ours[i]) return false
  return true
}

/** 订阅的三样东西。缺任何一样都说明浏览器给的不是一条可用的订阅。 */
function toInput(sub: PushSubscription) {
  const json = sub.toJSON()
  const endpoint = json.endpoint ?? sub.endpoint
  const p256dh = json.keys?.p256dh
  const auth = json.keys?.auth
  if (!endpoint || !p256dh || !auth) {
    throw new Error('浏览器返回的推送订阅不完整')
  }
  return { endpoint, keys: { p256dh, auth } }
}

export type PushState =
  /** 这台设备已经订上了，通知会到 */
  | 'on'
  /** 能开，但还没开 */
  | 'off'
  /** 用户在浏览器层面拒绝了。只能去浏览器设置里改，应用内无法重新请求 */
  | 'denied'
  /** 这个浏览器没有推送能力（iOS 标签页、旧浏览器） */
  | 'unsupported'
  /** SW 没注册上。开发环境就是这样 */
  | 'no-worker'

/**
 * 现在这台设备是什么状态。
 *
 * 以浏览器为准，不查服务端：本地的订阅在不在、权限给没给，
 * 这两件事决定了通知会不会响，服务端那行只是一条备注。
 *
 * 不等 SW 装好：这是给界面看的，答得慢比答得略保守更糟 ——
 * 开发环境没有 SW，等下去就是设置页空白四秒。
 */
export async function currentPushState(): Promise<PushState> {
  if (!pushSupported()) return 'unsupported'

  const reg = await readyRegistration()
  if (!reg) return 'no-worker'

  if (Notification.permission === 'denied') return 'denied'

  const sub = await reg.pushManager.getSubscription()
  return sub && Notification.permission === 'granted' ? 'on' : 'off'
}

export type EnableResult =
  /** 订阅成功，服务端已登记 */
  | 'subscribed'
  /** 用户拒绝了通知权限。这是一个正常的选择，不是错误 */
  | 'denied'
  /** 这个浏览器没有推送能力（iOS 标签页、旧浏览器） */
  | 'unsupported'
  /** SW 没注册上。开发环境就是这样，生产环境不该出现 */
  | 'no-worker'
  /** 服务端没配 VAPID 密钥 */
  | 'unconfigured'

/**
 * 开启推送。必须在用户手势里调用（点开关那一下）——
 * iOS 上不在手势里请求权限会直接被拒，而且不会再有第二次机会。
 *
 * 顺序是刻意的：注册 → 权限 → 公钥 → 订阅。请求权限前面只留本地查询，
 * 不留网络往返 —— iOS 的用户手势有一个几秒钟的窗口，先 await 一次
 * 慢请求再弹权限，窗口可能已经关了，而这一次拒绝就是永久拒绝。
 */
export async function enablePush(): Promise<EnableResult> {
  if (!pushSupported()) return 'unsupported'

  const reg = await activeRegistration()
  if (!reg) return 'no-worker'

  // 先单独要权限，而不是让 subscribe 顺带弹窗：这样「用户拒绝了」
  // 和「订阅失败」是两条可分辨的分支，前者不该报错。
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') return 'denied'

  const key = await fetchPushPublicKey()
  if (!key) return 'unconfigured'

  const applicationServerKey = urlBase64ToUint8Array(key)

  // 已经有一条订阅、但用的不是当前这把公钥时，subscribe 会抛
  // InvalidStateError 且永远抛 —— 换过 VAPID 密钥的环境会一直卡在这。
  // 所以先退掉旧的那条，让它按新公钥重建。
  const existing = await reg.pushManager.getSubscription()
  if (existing && !sameKey(existing.options.applicationServerKey, applicationServerKey)) {
    await existing.unsubscribe()
  }

  const sub = await reg.pushManager.subscribe({
    // 规范要求：只发会让用户看见的通知。本项目只发通知，正好。
    userVisibleOnly: true,
    applicationServerKey,
  })

  await subscribePush(toInput(sub))
  return 'subscribed'
}

/**
 * 关闭推送。
 *
 * 先告诉服务端再退本地：反过来的话本地这条已经没了，而服务端的行
 * 还在，得等下一次投递撞上 404/410 才会被停用（虽然也会自愈，
 * 但那中间用户以为关了、其实还会收到）。
 * 服务端那一步失败不阻止本地退订 —— 用户要的是这台设备不再响。
 */
export async function disablePush(): Promise<void> {
  if (!pushSupported()) return

  const reg = await readyRegistration()
  const sub = await reg?.pushManager.getSubscription()
  if (!sub) return

  try {
    await unsubscribePush(sub.endpoint)
  } catch {
    // 服务端记录没删掉：本地照样退，服务端那条会在投递失败后自愈
  }
  await sub.unsubscribe()
}

/**
 * 把浏览器手里那条订阅重新登记一次，认到当前账号名下。
 *
 * 登录后调用。换账号登录时浏览器里的订阅是旧的（属于上一个账号），
 * 服务端的 upsert 按 endpoint 改绑 —— 不重新登记的话，新账号收不到推、
 * 旧账号还会继续收到。
 *
 * 失败不抛也不提示：这是用户没有发起的后台校准，弹一个他处理不了的
 * 错误只会让人以为产品坏了。真正的开关在「我的」页，那里会如实报错。
 *
 * 这里等 SW 装好（和界面查状态不同）：它在后台跑，没人等它，
 * 而首次访问时 SW 可能正好还在安装 —— 那时跳过就等于这次登录白登了。
 */
export async function syncPushSubscription(): Promise<void> {
  try {
    if (!pushSupported() || Notification.permission !== 'granted') return

    const reg = await activeRegistration()
    const sub = await reg?.pushManager.getSubscription()
    if (!sub) return

    await subscribePush(toInput(sub))
  } catch {
    // 见上：后台校准失败是静默的
  }
}

import { clearTokens, getAccessToken, getRefreshToken, saveTokens } from '@/lib/token'

import type { Envelope, ErrorCode, TokenPair } from './types'

// 写死的 base：环境差异由 nginx 反代吸收（开发期是 Vite 的 proxy），
// 所以前端不需要任何 VITE_* 环境变量。
const API_BASE = '/api/v1'

/** 业务错误。UI 按 code 分支，message 只用于兜底展示。 */
export class ApiError extends Error {
  readonly code: ErrorCode | string
  readonly status: number
  readonly retryAfter: number | null

  constructor(status: number, code: string, message: string, retryAfter: number | null) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.retryAfter = retryAfter
  }
}

/** 网络层失败（断网、超时、被拦截）。与业务错误分开，提示文案也不同。 */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super('网络连接失败')
    this.name = 'NetworkError'
    this.cause = cause
  }
}

let onSessionExpired: (() => void) | null = null

/**
 * 不需要凭据的路径。
 *
 * 用白名单而不是黑名单：漏登记一个该登录的接口，结果是它照旧发出去、
 * 照旧 401，能看见；反过来（默认谁都放行）则是所有人一登录不上就
 * 静默失败，排查时完全没有线索。
 */
const PUBLIC_PATHS = ['/auth/', '/push/public-key']

/**
 * 挂在公开前缀下、但确实要登录的接口。
 *
 * 实时通道的票据按 §18.3 放在 /auth/ws-ticket，可它认的是 access token ——
 * 不排除掉的话，未登录时这一发会照旧飞出去换一个 401，而上面那段
 * 「一点凭据都没有就不发」的守卫正好被绕过。
 */
const AUTH_REQUIRED_PATHS = ['/auth/ws-ticket']

function needsAuth(path: string): boolean {
  if (AUTH_REQUIRED_PATHS.some((prefix) => path.startsWith(prefix))) return true
  return !PUBLIC_PATHS.some((prefix) => path.startsWith(prefix))
}

/**
 * 这个页面里出现过 access token 没有。
 *
 * 用来区分两种「手上没有凭据」：会话被清掉了（别的标签页登出、
 * 令牌被服务端作废），该把人送回登录页；本来就没登录，那是路由守卫
 * 的事，不该整页跳转、更不该扣一顶「登录状态已失效」的帽子。
 */
let everHadToken = false

/** 由路由层注册：refresh 也救不回来时清干净并回登录页。 */
export function setSessionExpiredHandler(fn: (() => void) | null): void {
  onSessionExpired = fn
}

/**
 * 拆信封。code 不为 OK 就抛 ApiError —— 调用方只管 try/catch，
 * 不必每次判断 res.code。
 */
async function unwrap<T>(res: Response): Promise<T> {
  const retryAfter = res.headers.get('Retry-After')
  let body: Envelope<T> | null = null
  try {
    body = (await res.json()) as Envelope<T>
  } catch {
    // 网关或代理返回的非 JSON 错误页
    throw new ApiError(res.status, 'INTERNAL', '服务开小差了，请稍后再试', null)
  }

  if (body.code !== 'OK') {
    throw new ApiError(
      res.status,
      body.code,
      body.message || '请求失败',
      retryAfter ? Number(retryAfter) : null,
    )
  }
  return body.data as T
}

/**
 * 静默续期。模块级单例 promise：并发请求同时 401 时只发一次 refresh，
 * 否则 N 个请求会换出 N 组凭据，而 refresh token 是一次性的 ——
 * 后到的那几个必然失败，用户会被莫名其妙踢下线。
 */
let refreshing: Promise<boolean> | null = null

async function refreshTokens(): Promise<boolean> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) return false

  try {
    // 这里用裸 fetch：走 request() 会在 401 时再次触发续期，直接递归。
    const res = await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })
    if (!res.ok) return false

    const body = (await res.json()) as Envelope<TokenPair>
    if (body.code !== 'OK' || !body.data) return false

    saveTokens(body.data)
    return true
  } catch {
    return false
  }
}

function ensureRefreshed(): Promise<boolean> {
  refreshing ??= refreshTokens().finally(() => {
    // 无论成败都放开，让下一次 401 有机会重新尝试
    refreshing = null
  })
  return refreshing
}

interface RequestOptions {
  /** 内部用：标记这是续期后的重试，避免无限循环 */
  retried?: boolean
  signal?: AbortSignal
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts: RequestOptions = {},
): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const token = getAccessToken()
  if (token) {
    headers.Authorization = `Bearer ${token}`
    everHadToken = true
  }

  // 一点凭据都没有，就说明这台浏览器上没有会话 —— 这一发必输，不发。
  //
  // 不是为了省一次往返：React Router 把同一分支上的 loader 并行跑，
  // 根 loader 判出「没登录」的时候，子 loader 的请求已经飞出去了。
  // 未登录整页打开 / 就是这种情况 —— 控制台里留下两条 401，
  // 而用户看到的只是一个正常的登录页，谁也看不出那两条红字从哪来。
  //
  // 也不交给下面的 401 分支处理：那里会 clearTokens 并把用户整页送到
  // /login?expired=1，可这个人从来就没有过期过。
  if (!token && !getRefreshToken() && needsAuth(path)) {
    // 这一页原本有会话、现在没了（多半是另一个标签页登出），
    // 那就是真的失效了，和下面的 401 分支一样把人送走
    if (everHadToken) onSessionExpired?.()
    throw new ApiError(401, 'UNAUTHORIZED', '登录状态已失效', null)
  }

  // 重试标记发给服务端，日志里能把同一次请求的两跳串起来
  if (opts.retried) headers['X-Retry'] = '1'

  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: opts.signal,
    })
  } catch (err) {
    throw new NetworkError(err)
  }

  if (res.status === 401 && !opts.retried) {
    // 登录/注册/刷新自己的 401 是业务结果（密码错、凭据失效），
    // 拿它们去续期只会绕一圈再失败
    const isAuthPath = path.startsWith('/auth/')

    if (!isAuthPath && (await ensureRefreshed())) {
      return request<T>(method, path, body, { ...opts, retried: true })
    }

    // 续期也没救回来：清干净本地状态，交给路由层回登录页
    if (!isAuthPath) {
      clearTokens()
      onSessionExpired?.()
    }
  }

  return unwrap<T>(res)
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('POST', path, body, opts),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('PATCH', path, body, opts),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('PUT', path, body, opts),
  // 带 body 的 DELETE。撤订推送的端点最长 1024 字符、且是 URL 不安全的
  // 原文，塞进 query 要先编码再让服务端解码，不如直接放 body 里。
  // 少数代理会吞掉 DELETE 的 body，所以别把这个口子用在别处 ——
  // 常规删除仍然是只有路径的那一种。
  del: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('DELETE', path, body, opts),
}

/**
 * 直传文件到预签名 URL。
 *
 * 用 XHR 而不是 fetch：只有 XHR 能报告上传进度，而 8MB 的图在移动网络下
 * 没有进度条会让用户以为卡死了。
 *
 * 这个请求是真正跨域的（URL 里的 Host 是签名的一部分，不能改写成同源），
 * 所以 CORS 必须由 MinIO 回答 —— 见 deploy/nginx/default.conf.template。
 */
export function uploadFile(
  url: string,
  file: File,
  onProgress?: (percent: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url, true)
    // 不设 Content-Type 之外的任何头：预签名 V4 把参与签名的头也算进去了，
    // 多带一个自定义头会让签名校验失败
    xhr.setRequestHeader('Content-Type', file.type || 'image/jpeg')

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new ApiError(xhr.status, 'INTERNAL', '图片上传失败，请重试', null))
    }
    xhr.onerror = () => reject(new NetworkError())
    xhr.onabort = () => reject(new DOMException('aborted', 'AbortError'))

    signal?.addEventListener('abort', () => xhr.abort(), { once: true })
    xhr.send(file)
  })
}

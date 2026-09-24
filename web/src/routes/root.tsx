import { useQueryClient } from '@tanstack/react-query'
import * as React from 'react'
import { isRouteErrorResponse, Link, redirect, useRouteError, type LoaderFunctionArgs } from 'react-router'

import { fetchMe } from '@/api/auth'
import { ApiError, NetworkError } from '@/api/client'
import type { Me } from '@/api/types'
import { AppShell } from '@/components/AppShell'
import { Button } from '@/components/ui/button'
import { syncPushSubscription } from '@/lib/push'
import { connectRealtime } from '@/lib/realtime'
import { hasSession } from '@/lib/token'

/**
 * 强制建档拦截的落点（19.6 的第二个洞）。
 *
 * 关键在 await：守卫必须等 /me 回来才知道 status。同步判本地有没有 token
 * 会先渲染业务页再跳走，用户会看到首页闪一下。
 */
export async function rootLoader(_args: LoaderFunctionArgs) {
  if (!hasSession()) throw redirect('/login')

  let me: Me
  try {
    me = await fetchMe()
  } catch (err) {
    // 只有「凭据不行」才回登录页。断网时凭据是好的，把人踢下线只会让他
    // 连登都登不回去 —— 登录同样需要网络。
    if (err instanceof ApiError) throw redirect('/login')
    throw err
  }

  if (me.next_step === 'onboarding') throw redirect('/onboarding')
  return { me }
}

/** 加载失败时的兜底页。没有它，loader 抛错会是一片空白。 */
export function RootError() {
  const error = useRouteError()

  const offline = error instanceof NetworkError
  const message = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : offline
      ? '网络连接失败。已经填好的内容都还在，联网后重新加载就行。'
      : '页面没能加载出来。'

  return (
    <div className="mx-auto flex max-w-[420px] flex-col items-center px-6 py-24 text-center">
      <p className="font-serif text-[17px] leading-[1.9] text-ink-2">{message}</p>
      <Button variant="outline" className="mt-8" onClick={() => window.location.reload()}>
        重新加载
      </Button>
      <Link to="/login" className="mt-4 text-[13px] text-muted hover:text-ink-2">
        回到登录页
      </Link>
    </div>
  )
}

export function Root() {
  const qc = useQueryClient()

  // 每次带壳启动时把浏览器手里那条推送订阅重新登记一次，认到当前账号名下。
  //
  // 放在这里而不是 loader 里：loader 每次路由切换都会跑，那会变成
  // 每翻一页发一次 POST。而 Root 只在整页加载时挂载一次，正好。
  //
  // 走到 Root 就意味着已经登录（rootLoader 挡在前面），所以不需要
  // 再判一次登录态。失败是静默的，理由见 syncPushSubscription 的注释。
  React.useEffect(() => {
    void syncPushSubscription()
  }, [])

  // 实时通道同理挂在这里：它的生命周期就是「登录之后」，而 Root 的
  // 挂载与卸载正好对应登录与登出 —— 登出会把整页送到 /login，
  // 这个组件随之卸载，连接跟着关掉，不必自己去判断会话还在不在。
  React.useEffect(() => connectRealtime(qc), [qc])

  return <AppShell />
}

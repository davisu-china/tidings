import * as React from 'react'
import { redirect, useNavigate, useSearchParams } from 'react-router'

import { fetchMe, login, register } from '@/api/auth'
import { ApiError } from '@/api/client'
import type { Me } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FieldError, Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { messageOf } from '@/lib/errors'
import { hasSession, saveTokens } from '@/lib/token'

const MIN_PASSWORD = 8

type Mode = 'login' | 'register'

/** 已登录的人不该再看到登录页。先静默问一次 /me，问不出来才留在这里。 */
export async function loginLoader() {
  if (!hasSession()) return null

  let me: Me
  try {
    me = await fetchMe()
  } catch {
    // 凭据过期或断网：留在登录页，不要把人弹来弹去
    return null
  }
  // redirect 必须在 try 之外抛 —— 它本身就是抛一个 Response，
  // 放在 try 里会被下面的 catch 一起吞掉
  throw redirect(me.next_step === 'onboarding' ? '/onboarding' : '/')
}

/**
 * 账号被锁时后端会给 Retry-After（秒）。换算成人话 ——
 * 「请 14 分钟后重试」比「429」有用得多。
 */
function lockedMessage(err: ApiError): string {
  if (!err.retryAfter || err.retryAfter <= 0) return err.message
  const minutes = Math.ceil(err.retryAfter / 60)
  return `${err.message}（约 ${minutes} 分钟后可再试）`
}

export function LoginPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()

  // 被踢下线时路由守卫会带 ?expired=1 过来，告诉用户是「登录过期」而不是「你操作错了」
  const expired = params.get('expired') === '1'

  const [mode, setMode] = React.useState<Mode>('login')
  const [email, setEmail] = React.useState('')
  const [password, setPassword] = React.useState('')
  const [confirm, setConfirm] = React.useState('')
  const [error, setError] = React.useState<string | null>(null)
  const [busy, setBusy] = React.useState(false)

  function switchMode(next: Mode) {
    setMode(next)
    setError(null)
    setConfirm('')
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    const mail = email.trim()
    if (!mail.includes('@')) {
      setError('请输入邮箱地址')
      return
    }
    if (mode === 'register') {
      if ([...password].length < MIN_PASSWORD) {
        setError(`密码至少 ${MIN_PASSWORD} 位`)
        return
      }
      if (password !== confirm) {
        setError('两次输入的密码不一致')
        return
      }
    } else if (password === '') {
      setError('请输入密码')
      return
    }

    setBusy(true)
    try {
      const result = mode === 'register' ? await register(mail, password) : await login(mail, password)
      saveTokens(result)
      navigate(result.next_step === 'onboarding' ? '/onboarding' : '/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? lockedMessage(err) : messageOf(err))
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-dvh flex-col justify-center px-6 py-12">
      <div className="mx-auto w-full max-w-[360px]">
        <h1 className="font-serif text-[28px] leading-none text-ink">有信</h1>
        <p className="mt-4 text-[14px] leading-[1.9] text-muted">
          我们替你留意。遇到合适的，就给你写一封。
        </p>

        <Tabs value={mode} onValueChange={(v) => switchMode(v as Mode)} className="mt-9">
          <TabsList>
            <TabsTrigger value="login">登录</TabsTrigger>
            <TabsTrigger value="register">注册</TabsTrigger>
          </TabsList>

          <form onSubmit={onSubmit} className="mt-6 grid gap-5" noValidate>
            {expired && !error && (
              <p className="text-[13px] text-muted">登录已过期，请重新登录。</p>
            )}

            <div>
              <Label htmlFor="email">邮箱</Label>
              <Input
                id="email"
                type="email"
                className="mt-2"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="email"
                autoCapitalize="none"
                autoCorrect="off"
                inputMode="email"
                placeholder="you@example.com"
              />
            </div>

            <div>
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                className="mt-2"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={mode === 'register' ? 'new-password' : 'current-password'}
                placeholder={mode === 'register' ? `至少 ${MIN_PASSWORD} 位` : undefined}
              />
            </div>

            {mode === 'register' && (
              <div>
                <Label htmlFor="confirm">确认密码</Label>
                <Input
                  id="confirm"
                  type="password"
                  className="mt-2"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  autoComplete="new-password"
                />
              </div>
            )}

            <FieldError>{error ?? undefined}</FieldError>

            <Button type="submit" size="lg" disabled={busy} className="mt-1">
              {busy ? <Spinner /> : mode === 'register' ? '注册并开始建档' : '登录'}
            </Button>

            {mode === 'register' && (
              <p className="text-[13px] leading-[1.8] text-muted">
                现在没有验证码，也没有找回密码。密码请自己存好。
              </p>
            )}
          </form>
        </Tabs>
      </div>
    </div>
  )
}

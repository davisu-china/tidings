import * as React from 'react'

import { fetchPushPublicKey } from '@/api/push'
import { Button } from '@/components/ui/button'
import {
  currentPushState,
  disablePush,
  enablePush,
  isIOS,
  isStandalone,
  type PushState,
} from '@/lib/push'

import { InstallGuide, installGuideDismissed } from './InstallGuide'

/**
 * 「通知」这一块（§14 / §19.5）。
 *
 * 状态一律以浏览器为准，不问服务端 —— 通知会不会响，取决于这台设备
 * 有没有订阅、权限给没给。所以每次进来都重新查一遍本地状态，
 * 而不是信某个缓存里的布尔值。
 *
 * 授权只在点击里请求。页面加载时自动 subscribe() 在 iOS 上会被直接
 * 拒绝，而且不再给第二次机会 —— 这一条比这一屏的任何文案都重要。
 */

type View = PushState | 'loading' | 'unconfigured' | 'ios-install'

const HINTS: Record<Exclude<View, 'loading'>, React.ReactNode> = {
  on: '这台设备会收到新引荐的通知。',
  off: '有新的引荐时通知你一声，不在站内也知道。',
  denied:
    '通知被浏览器挡住了，我们没法再问第二次。要到浏览器的网站设置里把「通知」改回允许，再回来开。',
  unsupported: '这个浏览器不支持网页通知。',
  unconfigured: '这个环境还没配推送密钥，通知发不出去。',
  'no-worker':
    '当前环境下通知服务没就绪：开发模式不注册 Service Worker，跑在 npm run dev 里就是这样。构建后的版本才有。',
  'ios-install': '先在 iPhone 上把它装到主屏幕，这里才会出现开关。',
}

export function PushSetting() {
  const [view, setView] = React.useState<View>('loading')
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [guideOpen, setGuideOpen] = React.useState(false)

  // iOS 上「装到主屏幕」不是一条可选提示，而是这个开关的前置条件：
  // 没装就没有 PushManager，也就没有开关可谈。
  const iosNeedsInstall = isIOS() && !isStandalone()

  const refresh = React.useCallback(async () => {
    setView(await currentPushState())
  }, [])

  React.useEffect(() => {
    let alive = true
    void (async () => {
      // 先问一次服务端配没配密钥：没配的话，开关按下去也只会走到一个
      // 用户无法解决的错误上，不如一开始就说清楚。
      try {
        if (!(await fetchPushPublicKey())) {
          if (alive) setView('unconfigured')
          return
        }
      } catch {
        // 拿不到公钥就照常走，让按钮去暴露真正的原因
      }
      const state = await currentPushState()
      if (alive) setView(state)
    })()
    return () => {
      alive = false
    }
  }, [])

  React.useEffect(() => {
    setGuideOpen(iosNeedsInstall && !installGuideDismissed())
  }, [iosNeedsInstall])

  // iOS 标签页里 PushManager 根本不存在，于是状态必然是 unsupported。
  // 但对这个用户来说「不支持」是句错话 —— 装上就能用，所以换成安装引导。

  async function onToggle() {
    setBusy(true)
    setError(null)
    try {
      if (view === 'on') {
        await disablePush()
        await refresh()
        return
      }

      const result = await enablePush()
      if (result === 'subscribed') {
        await refresh()
      } else {
        // 被拒绝、没装、没配密钥都已经有对应说明，不再叠一句话
        setView(result)
      }
    } catch {
      // 抛出来的都是真故障（网络、接口 5xx）。这是用户主动发起的操作，
      // 失败必须说出来，不能像后台校准那样静默。
      setError('没能改过来，稍后再试一次。')
    } finally {
      setBusy(false)
    }
  }

  // 提前返回而不是在 JSX 里三目：这样下面那段里 view 已经排除了 loading，
  // 查提示文案时类型才收得窄。
  if (view === 'loading') {
    return (
      <section className="mt-10 border-t border-line-soft pt-6">
        <h2 className="text-[14px] text-ink-2">通知</h2>
      </section>
    )
  }

  const shown: Exclude<View, 'loading'> =
    iosNeedsInstall && view === 'unsupported' ? 'ios-install' : view

  return (
    <section className="mt-10 border-t border-line-soft pt-6">
      <h2 className="text-[14px] text-ink-2">通知</h2>

      {iosNeedsInstall && guideOpen && (
        <div className="mt-4">
          <InstallGuide onDismiss={() => setGuideOpen(false)} />
        </div>
      )}

      <div className="mt-3">
        <p className="text-[13px] leading-[1.8] text-muted">{HINTS[shown]}</p>

        {view === 'on' && (
          <Button variant="line" className="mt-4" onClick={onToggle} disabled={busy}>
            关闭通知
          </Button>
        )}
        {view === 'off' && (
          <Button className="mt-4" onClick={onToggle} disabled={busy}>
            开启通知
          </Button>
        )}
        {/* 收起过引导、也还没装的用户，得留一个能再看一遍的入口（§19.5） */}
        {iosNeedsInstall && !guideOpen && (
          <button
            type="button"
            onClick={() => setGuideOpen(true)}
            className="mt-3 min-h-[44px] text-[13px] text-muted transition-colors duration-150 hover:text-ink-2"
          >
            怎么装到主屏幕
          </button>
        )}
        {error && <p className="mt-3 text-[13px] text-muted">{error}</p>}
      </div>
    </section>
  )
}

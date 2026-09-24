import { useQuery } from '@tanstack/react-query'
import { Mail, MessageSquare, User } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { fetchMatches } from '@/api/conversation'
import { fetchIntroductions } from '@/api/introductions'
import { cn } from '@/lib/cn'

type Badge = 'intro' | 'match' | null

const NAV: { to: string; label: string; icon: typeof Mail; end: boolean; badge: Badge }[] = [
  { to: '/', label: '引荐', icon: Mail, end: true, badge: 'intro' },
  { to: '/matches', label: '会话', icon: MessageSquare, end: false, badge: 'match' },
  { to: '/me', label: '我的', icon: User, end: false, badge: null },
]

function navItemClass(active: boolean): string {
  return cn(
    'flex items-center gap-2.5 transition-colors duration-150',
    active ? 'text-accent' : 'text-muted hover:text-ink-2',
  )
}

/**
 * 未读角标。
 *
 * 它是没有推送权限的人的兜底（§19.5）：iOS 上不装到主屏幕就收不到推送，
 * 而他们和装了的人看的是同一套界面，差别只能体现在这里。
 *
 * 复用首页那条 query 的缓存（同一个 key），所以正常情况下不额外发请求；
 * 直接落到别的页面时才自己拉一次。拉失败就不显示 —— 角标拿不到数字
 * 不该在页面上留一个红点或者一句错误。
 */
function useUnread(): { intro: number; match: number } {
  const intros = useQuery({
    queryKey: ['introductions'],
    queryFn: () => fetchIntroductions(),
    // 角标不需要每次路由切换都回源，一分钟内复用缓存
    staleTime: 60_000,
  })

  const matches = useQuery({
    queryKey: ['matches'],
    queryFn: () => fetchMatches(),
    staleTime: 60_000,
  })

  return { intro: intros.data?.unread ?? 0, match: matches.data?.unread ?? 0 }
}

function UnreadDot({ count }: { count: number }) {
  if (count <= 0) return null
  return (
    <span
      className="tnum inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-card bg-seal px-1 font-mono text-[11px] leading-none text-on-accent"
      aria-label={`${count} 条未读`}
    >
      {count > 9 ? '9+' : count}
    </span>
  )
}

/**
 * 应用外壳。
 *
 * 移动优先：< 1024px 是底部固定 tab，≥ 1024px 换成左侧栏、内容区不变。
 * 桌面端不放大字号、不拉伸卡片 —— 只增加列数和留白（见 19.4）。
 */
export function AppShell() {
  const unread = useUnread()

  return (
    <div className="min-h-dvh">
      {/* 桌面侧栏 */}
      <nav className="fixed inset-y-0 left-0 hidden w-[220px] flex-col border-r border-line px-5 pt-10 lg:flex">
        <div className="font-serif text-[19px] text-ink">有信</div>
        <div className="mt-10 flex flex-col gap-1">
          {NAV.map(({ to, label, icon: Icon, end, badge }) => (
            <NavLink key={to} to={to} end={end}>
              {({ isActive }) => (
                <span className={cn(navItemClass(isActive), 'h-11 text-[15px]')}>
                  <Icon aria-hidden className="h-[18px] w-[18px]" strokeWidth={1.5} />
                  {label}
                  {badge === 'intro' && <UnreadDot count={unread.intro} />}
                  {badge === 'match' && <UnreadDot count={unread.match} />}
                </span>
              )}
            </NavLink>
          ))}
        </div>
      </nav>

      {/*
        min-h-dvh + flex-col：让页面能撑满视口，空状态才有地方垂直居中。
        border-box 下 min-height 含 padding，所以底部给 tab 留的 56px 已经算在里面，
        不会多出一条滚动。普通页面是块级子元素，占自然高度、仍然顶对齐。
      */}
      <main className="flex min-h-dvh flex-col pb-[calc(56px+env(safe-area-inset-bottom,0px))] lg:pb-0 lg:pl-[220px]">
        <Outlet />
      </main>

      {/* 底部 tab。< 640px 单列满宽时它固定在最下面，避开 iPhone 的横条 */}
      <nav className="fixed inset-x-0 bottom-0 z-40 border-t border-line bg-paper pb-safe lg:hidden">
        <div className="mx-auto flex h-14 max-w-[880px] items-stretch">
          {NAV.map(({ to, label, icon: Icon, end, badge }) => {
            const count = badge === 'intro' ? unread.intro : badge === 'match' ? unread.match : 0
            return (
              <NavLink key={to} to={to} end={end} className="flex-1">
                {({ isActive }) => (
                  <span
                    className={cn(
                      navItemClass(isActive),
                      'h-full flex-col justify-center gap-1 text-[11px]',
                    )}
                  >
                    <span className="relative">
                      <Icon aria-hidden className="h-5 w-5" strokeWidth={1.5} />
                      {count > 0 && (
                        <span className="absolute -right-2.5 -top-1.5">
                          <UnreadDot count={count} />
                        </span>
                      )}
                    </span>
                    {label}
                  </span>
                )}
              </NavLink>
            )
          })}
        </div>
      </nav>
    </div>
  )
}

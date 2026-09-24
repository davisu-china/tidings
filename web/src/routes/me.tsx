import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight } from 'lucide-react'
import * as React from 'react'
import { Link, useLoaderData, useNavigate } from 'react-router'

import { logout } from '@/api/auth'
import { fetchProfile } from '@/api/profile'
import type { Profile, UserStatus } from '@/api/types'
import { PushSetting } from '@/components/PushSetting'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/cn'
import { ageFromBirthYM, educationLabel, genderLabel, missingLabels, PHOTO_GOAL } from '@/lib/dict'
import { cityName } from '@/lib/regions'
import { disablePush } from '@/lib/push'
import { clearTokens, getRefreshToken } from '@/lib/token'

export async function meLoader() {
  return { profile: await fetchProfile() }
}

/**
 * 状态提示。
 *
 * 全部用 muted 灰，没有一个字用红色 —— 审核不通过、被封禁都不例外。
 * 红色在这个产品里只属于印章（见 §19.1 的第三条克制规则）。
 */
const STATUS_NOTES: Partial<Record<UserStatus, string>> = {
  onboarding: '档案还没填完，填完才会被引荐给别人。',
  under_review: '资料正在审核，通过之前不会出现在别人的引荐里。',
  banned: '这个账号已被停用。如有疑问请联系我们。',
  deactivated: '账号已停用。',
}

function SummaryLine({ profile }: { profile: Profile }) {
  const parts = [
    profile.age ?? ageFromBirthYM(profile.birth_ym),
    cityName(profile.city_code),
    educationLabel(profile.education_level),
    profile.height_cm ? `${profile.height_cm}cm` : null,
  ].filter((v): v is string | number => v !== null && v !== '')

  if (parts.length === 0) return null
  return <p className="tnum mt-2 font-mono text-[13px] text-muted">{parts.join(' · ')}</p>
}

function EntryLink({ to, label, hint }: { to: string; label: string; hint?: string }) {
  return (
    <Link
      to={to}
      className={cn(
        'flex min-h-[56px] items-center justify-between gap-4 border-b border-line-soft px-1 py-3',
        'transition-colors duration-150 hover:bg-surface-2',
      )}
    >
      <span>
        <span className="block text-[15px] text-ink-2">{label}</span>
        {hint && <span className="mt-0.5 block text-[13px] text-muted">{hint}</span>}
      </span>
      <ChevronRight aria-hidden className="h-4 w-4 shrink-0 text-muted" strokeWidth={1.5} />
    </Link>
  )
}

export function MePage() {
  const { profile: initial } = useLoaderData() as { profile: Profile }
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [leaving, setLeaving] = React.useState(false)

  const { data: profile } = useQuery({
    queryKey: ['profile'],
    queryFn: () => fetchProfile(),
    initialData: initial,
  })

  async function onLogout() {
    setLeaving(true)

    // 先退掉这台设备的推送订阅。订阅属于浏览器、不属于账号，
    // 不退的话这台设备会继续收到上一个账号的引荐通知 —— 共用设备上
    // 那是实打实的泄露。浏览器权限仍然保留，下次登录点一下就能重新开，
    // 不会再弹一次授权。
    try {
      await disablePush()
    } catch {
      // 退订失败不该拦住登出：用户要的是离开
    }

    const token = getRefreshToken()

    // 先把在飞的和排队中的重取停掉。不停的话它们会在登出请求之后、
    // 凭据作废之时打出去，撞回 401 —— 控制台里两条红字，
    // 用户看不出所以然，但这个页面明明什么都没做错。
    await qc.cancelQueries()

    // 服务端登出失败（断网、token 已过期）也必须把本地清干净，
    // 否则用户点了「退出」却还在登录态，比不退更糟
    if (token) {
      try {
        await logout(token)
      } catch {
        // 忽略
      }
    }
    clearTokens()
    qc.clear()
    navigate('/login', { replace: true })
  }

  const missing = missingLabels(profile.missing_required)
  const note = STATUS_NOTES[profile.status]

  return (
    <div className="mx-auto max-w-[640px] px-5 py-8">
      <h1 className="font-serif text-[22px] leading-tight text-ink">我的</h1>

      <div className="mt-7 flex items-start gap-4">
        <div className="h-[64px] w-[64px] shrink-0 overflow-hidden rounded-card border border-line bg-surface-2">
          {profile.avatar_url ? (
            <img src={profile.avatar_url} alt="" className="h-full w-full object-cover" />
          ) : null}
        </div>
        <div className="min-w-0 pt-1">
          <p className="font-serif text-[19px] leading-snug text-ink">
            {profile.nickname ?? '还没填昵称'}
          </p>
          <SummaryLine profile={profile} />
          {profile.gender && (
            <p className="mt-1 text-[13px] text-muted">{genderLabel(profile.gender)}</p>
          )}
        </div>
      </div>

      {note && <p className="mt-6 text-[13px] leading-[1.8] text-muted">{note}</p>}

      {/* 不报完成度、也不列「还差哪几项」：进没进池子只有两种状态，说清楚就够，
          缺项清单该在编辑页里对着字段看。

          入池与否看 status，不看 missing —— 这两件事在加过门槛的老账号上会分叉：
          服务端只在 onboarding 时判翻牌，之后再往门槛里加项，已入池的人不会被
          踢回来（profile.go 的 applyProfileState）。拿 missing 当判据的话，
          他们会被告知一件不成立的事。缺项照样给出口，但那是「补一补更好」，
          不是「你还没进池子」。 */}
      <div className="mt-7">
        {profile.status === 'active' && (
          <p className="text-[13px] leading-[1.8] text-muted">
            入池条件已满足，可以收到引荐了。
            {missing.length === 0 && '选填项填得越多，越容易被记住。'}
          </p>
        )}
        {missing.length > 0 && (
          <Button
            asChild
            variant="outline"
            // 上面那句话只在 active 时才有，没有它时按钮自己撑起 mt-7 那份间距，
            // 不必再加 —— 否则建档中的用户会看到一段凭空的留白。
            className={profile.status === 'active' ? 'mt-5' : undefined}
          >
            <Link to="/me/edit">去补全</Link>
          </Button>
        )}
      </div>

      <nav className="mt-10 border-t border-line-soft">
        <EntryLink to="/me/edit" label="编辑资料" hint="基本、外形、学历、补充" />
        <EntryLink
          to="/me/photos"
          label="照片"
          hint={
            profile.photo_count === 0
              ? '还没有照片'
              : profile.photo_count < PHOTO_GOAL
                ? `${profile.photo_count} 张，再加 ${PHOTO_GOAL - profile.photo_count} 张更容易被记住`
                : `${profile.photo_count} 张，第一张是封面`
          }
        />
        <EntryLink to="/me/preferences" label="偏好设置" hint="硬条件 5 项、软偏好 3 项" />
        <EntryLink to="/me/settings" label="通知与暂停" hint="静默时段、暂停接收引荐" />
      </nav>

      <PushSetting />

      <button
        type="button"
        onClick={onLogout}
        disabled={leaving}
        className="mt-10 min-h-[44px] text-[14px] text-muted transition-colors duration-150 hover:text-ink-2 disabled:opacity-45"
      >
        退出登录
      </button>
    </div>
  )
}

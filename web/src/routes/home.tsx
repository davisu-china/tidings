import { useQuery } from '@tanstack/react-query'
import { useLoaderData } from 'react-router'

import { fetchIntroductions, type IntroList } from '@/api/introductions'
import { fetchProfile } from '@/api/profile'
import type { Profile } from '@/api/types'
import { IntroCard } from '@/components/IntroCard'
import { QuietEmpty } from '@/components/QuietEmpty'

export async function homeLoader() {
  // 两个请求一起发：它们互不依赖，串起来只是白等一个往返
  const [profile, intros] = await Promise.all([fetchProfile(), fetchIntroductions()])
  return { profile, intros }
}

/**
 * 首页。
 *
 * 有引荐时是一叠信纸，没有时是 QuietEmpty。空状态不是兜底分支 ——
 * 决策 14 不设保底，「没有引荐」是常态，这一页在池子长大之前
 * 依然是用户见得最多的一屏。
 */
export function HomePage() {
  const { profile: initialProfile, intros: initialIntros } = useLoaderData() as {
    profile: Profile
    intros: IntroList
  }

  const { data: profile } = useQuery({
    queryKey: ['profile'],
    queryFn: () => fetchProfile(),
    initialData: initialProfile,
  })

  const { data } = useQuery({
    queryKey: ['introductions'],
    queryFn: () => fetchIntroductions(),
    initialData: initialIntros,
  })

  if (data.introductions.length === 0) {
    return (
      <div className="mx-auto flex max-w-[880px] flex-1 flex-col">
        <QuietEmpty missing={profile.missing_required} />
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-[560px] px-4 py-6 sm:px-6">
      {/*
        不写「你有 N 条新引荐」—— 每次打开都被告知一个数字，
        是社交产品的语气，不是信件的语气。条数就在下面，
        一眼能数清，不需要一句总结在前面再数一遍。
      */}
      <div className="flex flex-col gap-5">
        {data.introductions.map((intro) => (
          <IntroCard key={intro.id} intro={intro} />
        ))}
      </div>
    </div>
  )
}

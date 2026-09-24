import { useQuery } from '@tanstack/react-query'
import * as React from 'react'
import { redirect, useLoaderData, useNavigate } from 'react-router'

import { listPhotos } from '@/api/media'
import { fetchProfile } from '@/api/profile'
import type { Photo, Profile } from '@/api/types'
import { AvatarUploader, PhotoUploader } from '@/components/PhotoUpload'
import { BasicFields, EducationFields, FigureFields, MoreFields } from '@/components/profile/fields'
import { useProfileForm } from '@/components/profile/form'
import { firstIncompleteStep, STEPS } from '@/components/profile/steps'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { missingLabels } from '@/lib/dict'

/**
 * 建档向导。全屏不套壳 —— 这时候底部 tab 通向的页面一个都还不能用。
 *
 * 已经建完档的人不该再看到它：状态由服务端说了算，不看本地标记。
 */
export async function onboardingLoader() {
  const [profile, photos] = await Promise.all([fetchProfile(), listPhotos()])
  if (profile.next_step !== 'onboarding') throw redirect('/')
  return { profile, photos }
}

const STEP_HINT: Record<string, string> = {
  basic: '先让引荐卡上有个能称呼的名字。',
  figure: '让别人能判断要不要认识你。',
  education: '学历是硬条件过滤里最常用的两项之一。写上母校，别人更容易认出你。',
  // 「我们会自动检查」是 e2e-onboarding 判断「已进入照片步」的锚点，别删。
  photos: '一张就够，多传几张别人更容易记住你。头像要能看清正脸，我们会自动检查。',
  // 「补充」这一步 v1.7 起也是必填，所以不能再写「可以留到以后再说」——
  // 那句话会让用户以为点得动下一步，实际会撞上 11 条校验。
  more: '这几项是别人决定要不要认识你的依据，都填上才能进池子。',
}

export function OnboardingPage() {
  const initial = useLoaderData() as { profile: Profile; photos: Photo[] }
  const navigate = useNavigate()

  const { data: profile } = useQuery({
    queryKey: ['profile'],
    queryFn: () => fetchProfile(),
    initialData: initial.profile,
  })
  const { data: photos } = useQuery({
    queryKey: ['photos'],
    queryFn: () => listPhotos(),
    initialData: initial.photos,
  })

  const { form, save, saving } = useProfileForm(profile)
  const {
    control,
    formState: { errors },
  } = form

  // 19.6 的第一个洞：不做回填的话，中途刷新就得从第 1 步重来。
  // 这里按 missing_required 直接落到第一个没做完的步骤。
  const [step, setStep] = React.useState(() => firstIncompleteStep(initial.profile.missing_required))
  const [photoBlocked, setPhotoBlocked] = React.useState<string | null>(null)

  const current = STEPS[step]
  const isLast = step === STEPS.length - 1
  const done = profile.missing_required.length === 0

  async function onNext() {
    setPhotoBlocked(null)

    if (current.key === 'photos') {
      // 照片和头像走各自的接口，上传即生效，这里只是把门槛说清楚
      const left = missingLabels(
        profile.missing_required.filter((m) => m === 'avatar_key' || m === 'photos'),
      )
      if (left.length > 0) {
        setPhotoBlocked(`还差${left.join('、')}，补齐才能继续。`)
        return
      }
    } else if (!(await save(current.fields))) {
      return
    }

    if (isLast) {
      // 走完向导回首页。status 由服务端在照片足够时就翻成 active 了，
      // 这里不需要（也不该）在前端自己判一遍。
      navigate('/', { replace: true })
      return
    }
    setStep(step + 1)
    window.scrollTo({ top: 0 })
  }

  return (
    <div className="mx-auto flex min-h-dvh max-w-[640px] flex-col px-5">
      <header className="pt-[calc(env(safe-area-inset-top,0px)+28px)]">
        <div className="flex items-baseline justify-between">
          <span className="font-serif text-[19px] text-ink">建档</span>
          <span className="tnum font-mono text-[12px] tracking-[.08em] text-muted">
            第 {step + 1} 步 / 共 {STEPS.length} 步
          </span>
        </div>

        {/* 步骤进度用一根 2px 的线，不用圆点步骤条 —— 五步现在全是必填，
            圆点只会把注意力引到「走到第几步」而不是「这一屏要填什么」 */}
        <div className="mt-4 flex gap-1" aria-hidden>
          {STEPS.map((s, i) => (
            <span
              key={s.key}
              className={i <= step ? 'h-[2px] flex-1 bg-accent' : 'h-[2px] flex-1 bg-line'}
            />
          ))}
        </div>

        {/* 不报完成度、也不列「还差哪几项」：向导本来就是一步步往下走的，
            顶上再挂一张缺项清单，说的全是用户此刻够不着的东西 */}
        {done && (
          <p className="mt-6 text-[13px] leading-[1.8] text-muted">
            入池条件已满足，可以收到引荐了。
          </p>
        )}
      </header>

      <main className="flex-1 pb-40 pt-9">
        <h1 className="font-serif text-[18px] text-ink">{current.title}</h1>
        <p className="mt-2 text-[13px] leading-[1.8] text-muted">{STEP_HINT[current.key]}</p>

        <div className="mt-7">
          {current.key === 'basic' && <BasicFields control={control} errors={errors} />}
          {current.key === 'figure' && <FigureFields control={control} errors={errors} />}
          {current.key === 'education' && <EducationFields control={control} errors={errors} />}
          {current.key === 'more' && <MoreFields control={control} errors={errors} />}
          {current.key === 'photos' && (
            <div>
              <AvatarUploader avatarUrl={profile.avatar_url} />
              <div className="mt-8 border-t border-line-soft pt-7">
                <PhotoUploader photos={photos} />
              </div>
              {photoBlocked && (
                <p className="mt-4 text-[13px] leading-[1.8] text-muted">{photoBlocked}</p>
              )}
            </div>
          )}
        </div>
      </main>

      {/* 每一步提交即保存，所以这里只有「下一步」，没有全局的保存按钮 */}
      <div className="fixed inset-x-0 bottom-0 border-t border-line bg-paper pb-safe">
        <div className="mx-auto flex max-w-[640px] items-center gap-3 px-5 py-3">
          {step > 0 && (
            <Button
              variant="quiet"
              onClick={() => {
                setPhotoBlocked(null)
                setStep(step - 1)
                window.scrollTo({ top: 0 })
              }}
            >
              上一步
            </Button>
          )}
          <Button size="lg" className="flex-1" onClick={onNext} disabled={saving}>
            {saving ? <Spinner /> : isLast ? '完成，去首页' : '下一步'}
          </Button>
        </div>
      </div>
    </div>
  )
}

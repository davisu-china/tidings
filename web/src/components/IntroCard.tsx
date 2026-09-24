import * as React from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'

import {
  respondIntroduction,
  type Introduction,
  type IntroAction,
  type PassReason,
  type RespondResult,
} from '@/api/introductions'
import { PassReasons, SealButton, SealMark } from '@/components/SealButton'
import { Button } from '@/components/ui/button'
import { useToast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { ageFromBirthYM, cityName, educationLabel, incomeLabel } from '@/lib/dict'
import { messageOf } from '@/lib/errors'

/**
 * 引荐卡。不是一张带阴影的卡片，是一页信纸（§19.2）：
 * 顶部一行像信函的编号，中间是人和话，底部一条发丝线分隔的表态区。
 *
 * 同一个组件出两种密度：列表里是卡片，/intro/:id 上是引荐卡全文
 * （多一个职业，文字更舒展）。分成两个组件的话，表态区那套动画和
 * 状态机会被抄两份，而它们迟早会不一致。
 */

/** 剩余时间。按天给，不足一天给小时 —— 「还剩 0 天」是最糟的一种写法。 */
function remainingText(expiresAt: string): string {
  const ms = new Date(expiresAt).getTime() - Date.now()
  if (Number.isNaN(ms) || ms <= 0) return '已结束'
  const hours = ms / 3_600_000
  if (hours < 24) return `还剩 ${Math.max(1, Math.floor(hours))} 小时`
  return `还剩 ${Math.ceil(hours / 24)} 天`
}

/** 「1995 · 上海 · 硕士」。缺项就少一段，不留空档。 */
function factsLine(other: Introduction['other'], withAge: boolean): string {
  const parts: string[] = []
  if (withAge) {
    const age = ageFromBirthYM(other.birth_ym)
    if (age !== null) parts.push(`${age} 岁`)
  } else if (other.birth_ym) {
    parts.push(String(Math.floor(other.birth_ym / 100)))
  }
  const city = cityName(other.city_code)
  if (city) parts.push(city)
  const edu = educationLabel(other.education_level)
  if (edu) parts.push(edu)
  if (withAge && other.occupation) parts.push(other.occupation)
  return parts.join(' · ')
}

/** 表态的四个阶段。settled 之后卡片不再显示动作，只显示结果。 */
type Phase = 'idle' | 'picking' | 'stamping' | 'leaving' | 'settled'

/** 停留 500ms，让用户看清自己盖了什么，然后卡片才让位。 */
const DWELL_MS = 500
/** 与 index.css 里 .letter-leaving 的过渡时长一致。 */
const LEAVE_MS = 320

export function IntroCard({ intro, detail = false }: { intro: Introduction; detail?: boolean }) {
  const qc = useQueryClient()
  const toast = useToast()

  const [phase, setPhase] = React.useState<Phase>('idle')
  const [result, setResult] = React.useState<RespondResult | null>(null)

  const respond = useMutation({
    mutationFn: ({ action, reason }: { action: IntroAction; reason?: PassReason }) =>
      respondIntroduction(intro.id, action, reason),
    onSuccess: (r) => {
      setResult(r)
      // 「不合适」不带动画：全站只有盖章这一处动效。它直接落到结果态。
      if (r.my_action === 'pass') setPhase('settled')
    },
    onError: (err) => {
      // 回到可以重试的状态。已经开始的动画要收回去 —— 一枚停在空中
      // 的印章比没有印章更让人困惑。
      setPhase('idle')
      toast.show(messageOf(err))
    },
  })

  // 动画的两个节拍。放在 effect 里而不是嵌套 setTimeout，
  // 是因为组件在动画中途卸载（用户切页）时它会自动被清掉。
  React.useEffect(() => {
    if (phase === 'stamping') {
      const t = window.setTimeout(() => setPhase('leaving'), DWELL_MS)
      return () => window.clearTimeout(t)
    }
    if (phase === 'leaving') {
      const t = window.setTimeout(() => setPhase('settled'), LEAVE_MS)
      return () => window.clearTimeout(t)
    }
    return undefined
  }, [phase])

  // 缓存刷新放在动画走完之后。
  //
  // 不是为了好看：列表那条 query 一失效就会回源，回来的新数据里
  // my_action 已经有值，卡片会在动画中途换成结果态 —— 印章还没落定，
  // 底下的信纸先变了。让动画独占这一秒，之后再对齐。
  const settled = phase === 'settled'
  React.useEffect(() => {
    if (!settled) return
    void qc.invalidateQueries({ queryKey: ['introductions'] })
    void qc.invalidateQueries({ queryKey: ['intro', intro.id] })
    void qc.invalidateQueries({ queryKey: ['matches'] })
  }, [settled, qc, intro.id])

  const busy = respond.isPending || phase !== 'idle'
  const stamping = phase === 'stamping' || phase === 'leaving'

  function like() {
    setPhase('stamping')
    // 请求在点击的瞬间就发出去，不等动画。
    respond.mutate({ action: 'like' })
  }

  function pass(reason: PassReason) {
    respond.mutate({ action: 'pass', reason })
  }

  // ---------------------------------------------------------------- 已经封上的信

  // 已终结的两个状态长得完全一样，这是刻意的（§19.2）：措辞本来就相同，
  // 视觉上也不该让他看出「是被婉拒了」还是「是超时了」。
  //
  // 不给对方照片、不给昵称、不给回看入口 —— 引荐已经终结，一个回看入口
  // 等于给他一个反复点开的机会。
  if (intro.state === 'declined' || intro.state === 'expired') {
    return (
      <article className="rounded-card border border-line bg-surface">
        <header className="flex items-baseline justify-between px-5 py-3">
          <span className="font-mono text-[11px] tracking-[0.08em] text-muted">
            引荐 · 第 {intro.issue_no} 期
          </span>
        </header>
        <div className="border-t border-line" />
        <p className="px-5 py-9 text-center font-serif text-[15px] leading-[1.9] text-muted">
          这封信已经封上了。
        </p>
      </article>
    )
  }

  // ---------------------------------------------------------------- 信纸

  const { other } = intro
  const facts = factsLine(other, detail)

  return (
    <article
      className={cn(
        // relative 是给印章的落点用的
        'relative rounded-card border border-line bg-surface',
        phase === 'leaving' && 'letter-leaving',
      )}
    >
      <header className="flex items-baseline justify-between px-5 py-3">
        <span className="font-mono text-[11px] tracking-[0.08em] text-muted">
          引荐 · 第 {intro.issue_no} 期
        </span>
        <span className="tnum font-mono text-[11px] tracking-[0.08em] text-muted">
          {remainingText(intro.expires_at)}
        </span>
      </header>

      <div className="border-t border-line" />

      {/* 正文整块可点，进详情页看全文。列表里一眼看得到的是摘要，
          要决定的是「想不想认识这个人」，不是「读完这封信」。 */}
      <LetterBody intro={intro} detail={detail} facts={facts} linked={!detail} />

      <div className="border-t border-line" />

      <footer className="px-5 py-4">
        <ResponseArea
          intro={intro}
          result={result}
          phase={phase}
          busy={busy}
          onLike={like}
          onPickReason={() => setPhase('picking')}
          onCancelReason={() => setPhase('idle')}
          onPass={pass}
        />
      </footer>

      {/* 印章。落点固定在信纸右下角，压住表态区 —— 那 500ms 里
          它盖住的正是「想认识」自己，这恰好说明了这个动作收不回来。 */}
      <SealMark landed={stamping} />
    </article>
  )
}

function LetterBody({
  intro,
  detail,
  facts,
  linked,
}: {
  intro: Introduction
  detail: boolean
  facts: string
  linked: boolean
}) {
  const { other } = intro

  const body = (
    <div className={cn('flex gap-4 px-5', detail ? 'py-6' : 'py-5')}>
      {/*
        方图、1px 线框、无圆角。用 object-cover 让它撑满方框 ——
        用户传的多半是竖图，等比缩放会留出两条白边，看起来像没加载出来。
        详情页给得更大：这是这一页唯一的一个人。
      */}
      {other.cover_url ? (
        <img
          src={other.cover_url}
          alt=""
          width={detail ? 160 : 112}
          height={detail ? 160 : 112}
          loading="lazy"
          className={cn(
            'shrink-0 border border-line object-cover',
            detail ? 'h-40 w-40' : 'h-28 w-28',
          )}
        />
      ) : (
        <div
          className={cn('shrink-0 border border-line bg-surface-2', detail ? 'h-40 w-40' : 'h-28 w-28')}
        />
      )}

      <div className="min-w-0 flex-1">
        <h2
          className={cn(
            'font-serif leading-tight text-ink',
            detail ? 'text-[26px]' : 'text-[22px]',
          )}
        >
          {other.nickname ?? '这位朋友'}
        </h2>

        {facts && <p className="tnum mt-2 font-mono text-[13px] text-muted">{facts}</p>}
        {other.height_cm !== null && (
          <p className="tnum mt-1 font-mono text-[13px] text-muted">{other.height_cm}cm</p>
        )}

        {/* 年收入单独一行、用 accent。决策 11 要展示它，但它只是一个区间，
            不做成标签或徽章 —— 那会让它看起来像一种评级 */}
        {other.income_band !== null && (
          <p className="tnum mt-2 font-mono text-[13px] text-accent">
            年收入 {incomeLabel(other.income_band)}
          </p>
        )}
      </div>
    </div>
  )

  return (
    <>
      {linked ? (
        <Link to={`/intro/${intro.id}`} className="block hover:bg-surface-2/40">
          {body}
        </Link>
      ) : (
        body
      )}

      {other.intro && (
        <>
          <div className="border-t border-line-soft" />
          <p
            className={cn(
              'px-5 font-serif leading-[1.9] text-ink-2',
              detail ? 'py-5 text-[16px]' : 'py-4 text-[15px]',
            )}
          >
            {other.intro}
          </p>
        </>
      )}
    </>
  )
}

/**
 * 表态区。
 *
 * 它有五种面孔，按「库里是什么状态」和「这一刻正在发生什么」决定：
 *   已经表过态 / 已成匹配 / 已终结 → 只说明结果，不给动作
 *   正在选原因 → 三个原因
 *   其余 → 想认识 + 不合适
 */
function ResponseArea({
  intro,
  result,
  phase,
  busy,
  onLike,
  onPickReason,
  onCancelReason,
  onPass,
}: {
  intro: Introduction
  result: RespondResult | null
  phase: Phase
  busy: boolean
  onLike: () => void
  onPickReason: () => void
  onCancelReason: () => void
  onPass: (reason: PassReason) => void
}) {
  // 我刚表完态：手上的 result 比库里的那份新，优先用它。
  const matched = result?.matched ?? intro.state === 'matched'
  const myAction = result?.my_action ?? intro.my_action

  if (matched) {
    return (
      <div className="flex flex-col gap-4">
        <p className="font-serif text-[15px] leading-[1.9] text-accent">
          你们都点了「想认识」。
        </p>
        {result?.match_id !== undefined ? (
          <Button asChild variant="outline" className="self-start">
            <Link to={`/chat/${result.match_id}`}>去说话</Link>
          </Button>
        ) : (
          // 从列表点进来、且这条早就成了匹配：列表接口不下发 match_id，
          // 会话列表上那条是同一时刻建的，就在最上面。
          <Button asChild variant="outline" className="self-start">
            <Link to="/matches">去会话</Link>
          </Button>
        )}
      </div>
    )
  }

  if (myAction === 'like') {
    return (
      <p className="text-[14px] leading-[1.9] text-muted">
        已经递出去了。对方表态后，我们会立刻告诉你。
      </p>
    )
  }

  if (myAction === 'pass') {
    // §19.2：已被婉拒与已过期用同一套视觉，不加红、不加感叹号
    return <p className="text-[14px] leading-[1.9] text-muted">已经记下了。这封信到此为止。</p>
  }

  if (phase === 'picking') {
    return <PassReasons onPick={onPass} onCancel={onCancelReason} disabled={busy} />
  }

  return (
    <div className="flex items-center justify-between gap-4">
      <SealButton onClick={onLike} disabled={busy} />
      <button
        type="button"
        onClick={onPickReason}
        disabled={busy}
        className="h-11 px-2 text-[14px] text-muted transition-colors duration-150 hover:text-ink-2 disabled:opacity-45"
      >
        不合适
      </button>
    </div>
  )
}

import { cn } from '@/lib/cn'
import { PASS_REASONS } from '@/lib/dict'

/**
 * 印章（§19.3）。
 *
 * 「想认识」这个动作做成盖章，而不是按钮变蓝。除了隐喻合适，它还有一个
 * 真实的好处：「想认识」是不可撤销的，盖章这个动作本身就传达了这一点。
 *
 * 全站只有这一处动效。页面切换不做过渡、列表不做入场、卡片不做悬停位移 ——
 * 让印章成为唯一一个会动的东西，它才有分量。
 */

/**
 * 「想认识」的印章按钮。
 *
 * 红底、2px 圆角、方角字距。它和「不合适」的视觉权重差距很大，
 * 这是有意的：一个是要做的事，一个是要克制的事。
 */
export function SealButton({
  onClick,
  disabled,
  className,
}: {
  onClick: () => void
  disabled?: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'inline-flex h-11 items-center rounded-seal bg-seal font-serif text-[16px] font-semibold',
        'text-on-accent transition-colors duration-150 hover:bg-seal/90',
        // 「想 认 识」三个字要撑开。字距加在每字之后（含最后一个），
        // 所以右边少留 0.3em，否则字看着是偏左的。
        'pl-6 pr-[1.05rem] tracking-[0.3em]',
        'disabled:pointer-events-none disabled:opacity-45',
        className,
      )}
    >
      想认识
    </button>
  )
}

/**
 * 落下的那枚印。
 *
 * 绝对定位，落点由父元素（信纸）决定；父元素必须是 relative。
 * 动画本身在 index.css 的 .seal-mark 里 —— 它有四个属性在同一条
 * 时间轴上变，摊成工具类之后没人看得出那是一条时间轴。
 *
 * aria-hidden：它是动画，不是内容。读屏软件不该念出「信」。
 */
export function SealMark({ landed }: { landed: boolean }) {
  return (
    <span
      aria-hidden
      data-landed={landed}
      className={cn(
        'seal-mark pointer-events-none absolute bottom-6 right-6',
        'flex h-[76px] w-[76px] items-center justify-center',
        'rounded-seal border-2 border-seal',
      )}
    >
      <span className="font-serif text-[34px] leading-none font-semibold text-seal">信</span>
    </span>
  )
}

/**
 * 「不合适」的三个原因（决策 07：三选一必填，不要求输入文字）。
 *
 * 必填不是为了收集数据，是为了让这个动作有一点重量 —— 一个零成本的
 * 「不合适」，会让用户在没细看的情况下顺手点掉。但也不要求他写理由：
 * 写理由的门槛会把真的想拒绝的人逼成不表态。
 */
export function PassReasons({
  onPick,
  onCancel,
  disabled,
}: {
  onPick: (reason: 'mismatch' | 'vibe' | 'other') => void
  onCancel: () => void
  disabled?: boolean
}) {
  return (
    <div className="flex flex-col gap-3">
      <p className="text-[13px] text-muted">说说哪里不合适</p>
      <div className="flex flex-wrap gap-2">
        {PASS_REASONS.map((r) => (
          <button
            key={r.value}
            type="button"
            disabled={disabled}
            onClick={() => onPick(r.value)}
            className={cn(
              'h-11 rounded-card border border-line px-4 text-[14px] text-ink-2',
              'transition-colors duration-150 hover:border-ink-2',
              'disabled:pointer-events-none disabled:opacity-45',
            )}
          >
            {r.label}
          </button>
        ))}
      </div>
      <button
        type="button"
        onClick={onCancel}
        disabled={disabled}
        className="h-11 self-start text-[14px] text-muted transition-colors duration-150 hover:text-ink-2 disabled:opacity-45"
      >
        再想想
      </button>
    </div>
  )
}

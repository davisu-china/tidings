import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { MISSING_LABELS } from '@/lib/dict'

/** 信封线稿。内联 SVG 而不是 emoji —— emoji 在各系统上长得完全不一样。 */
function EnvelopeMark() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 48 34"
      className="h-[34px] w-12 text-muted"
      fill="none"
      stroke="currentColor"
      strokeWidth="1"
    >
      <rect x="0.5" y="0.5" width="47" height="33" />
      <path d="M0.5 1.5 L24 19 L47.5 1.5" />
    </svg>
  )
}

const COMPLETENESS_HINT = 60

interface QuietEmptyProps {
  completeness: number
  missing: readonly string[]
}

/**
 * 首页空状态 —— 这个产品里见得最多的一个界面。
 *
 * 决策 14 不设保底，所以「没有引荐」是常态而不是异常：用户可能连续几周
 * 打开都是空的。一个写得像故障的空状态会让人以为产品坏了，所以这里
 * 不写「暂无数据」、不放灰色插画、也不给「去逛逛」。
 *
 * 它像一封还没到的信：平静、有交代，并顺手把该做的事说了 ——
 * 只入池的用户能收引荐、不会被引荐（决策 16），把这句话放在他唯一
 * 反复看到的界面上，比在「我的」里放一个进度条有用得多。
 */
export function QuietEmpty({ completeness, missing }: QuietEmptyProps) {
  const showHint = completeness < COMPLETENESS_HINT

  return (
    <div className="mx-auto flex w-full max-w-[420px] flex-1 flex-col items-center justify-center px-6 py-20 text-center">
      <EnvelopeMark />

      <p className="mt-6 font-serif text-[17px] leading-[1.9] text-ink-2">
        暂时没有新的引荐。
        <br />
        我们在替你看着。
      </p>

      {showHint && (
        <div className="mt-10 w-full">
          <div className="flex items-baseline justify-between text-[13px] text-muted">
            {/* 别写「已完整」：这里是完成度，不是入池门槛。60% 的档案说「已完整」是假话 */}
            <span>档案完成度</span>
            <span className="tnum font-mono text-ink-2">{completeness}%</span>
          </div>

          <Progress value={completeness} className="mt-3" label="档案完成度" />

          <p className="mt-4 text-[13px] leading-[1.9] text-muted">
            补全后才会被引荐给别人。
            {missing.length > 0 && (
              <>
                <br />
                还差：
                {missing.map((f) => MISSING_LABELS[f] ?? f).join('、')}
              </>
            )}
          </p>

          <Button asChild variant="outline" className="mt-6">
            <Link to="/me/edit">去补全</Link>
          </Button>
        </div>
      )}
    </div>
  )
}

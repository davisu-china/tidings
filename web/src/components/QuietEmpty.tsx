import { Link } from 'react-router'

import { Button } from '@/components/ui/button'

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

interface QuietEmptyProps {
  missing: readonly string[]
}

/**
 * 首页空状态 —— 这个产品里见得最多的一个界面。
 *
 * 决策 14 不设保底，所以「没有引荐」是常态而不是异常：用户可能连续几周
 * 打开都是空的。一个写得像故障的空状态会让人以为产品坏了，所以这里
 * 不写「暂无数据」、不放灰色插画、也不给「去逛逛」。
 *
 * 它像一封还没到的信：平静、有交代。信上不夹催收单 —— 不报完成度，
 * 也不列「还差哪几项」，那些话在这一屏上只会把人推远。还没进池子的人
 * 只留一条出路，其余交给「我的」那一页去说。
 */
export function QuietEmpty({ missing }: QuietEmptyProps) {
  return (
    <div className="mx-auto flex w-full max-w-[420px] flex-1 flex-col items-center justify-center px-6 py-20 text-center">
      <EnvelopeMark />

      <p className="mt-6 font-serif text-[17px] leading-[1.9] text-ink-2">
        暂时没有新的引荐。
        <br />
        我们在替你看着。
      </p>

      {missing.length > 0 && (
        <Button asChild variant="outline" className="mt-10">
          <Link to="/me/edit">去补全</Link>
        </Button>
      )}
    </div>
  )
}

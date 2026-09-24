package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/metrics"
)

// 超时释放（§13.2 的「时限内无人表态」那一支）。
//
// 这是 M4 的核心：在它之前，一条没人搭理的引荐会永远停在 pending，
// 两个人都被它占着 —— 池子就这么一寸一寸地堵死。

// ExpireStats 是一轮扫描的结果，压成一行写进 job_runs.last_result。
type ExpireStats struct {
	// Scanned 是这一轮取到的候选数。
	Scanned int
	// Expired 是真的被推进到 expired 的条数。
	Expired int
	// Raced 是拿到行锁时已经不是未终结状态的条数 —— 在本次扫描
	// 读候选和改这一行之间，有人表态把它结算掉了。不是错误。
	Raced int
	// Notified 是这一轮入队的收尾通知条数。
	Notified int
	// Failed 是处理失败的条数。单条失败不影响其余。
	Failed int
}

// closingNotice 是一条该发出去的收尾通知。
type closingNotice struct {
	UserID   int64
	Template string
	// Reason 只进 payload，用于埋点把「超时释放」和「被拒绝」分开 ——
	// 两者的文案完全相同（§4.8），只有这一列能区分。
	Reason string
}

// closingNotices 决定一条引荐到期时该给谁发什么（§4.8 那张表）。
//
// 抽成纯函数是因为这是 M4 唯一一处有分支的业务判断，而 §22 的验收
// 判据恰恰是「四类通知都正确到达」—— 值得单独测，不必等到端到端。
//
// 规则：
//
//	成对引荐 —— 两边都收到过这封信，各自按自己的动作领通知：
//	    已表态「想认识」的 → intro_closed（「上封信没有等到回音」）
//	    从没表态的         → intro_missed（「你错过了 1 位合适的人」）
//
//	单向引荐 —— 一律不发。
//	    隐藏方从头到尾没收到过东西，给他发「你错过了」是凭空冒出来一句话。
//	    可见方按 §22 的验收判据也不发：判据写的是「单向引荐不产生错过提醒」，
//	    而 §4.8 给的理由只解释了隐藏方。这一处文档自相矛盾（§13.1 的状态机
//	    只把隐藏方排除在过期分支外），按验收判据从严处理，两边都不发。
//	    代价是可见方确实有过一次机会却石沉大海 —— 这是知情的取舍。
//
// 不可能出现「两边都是 like」：那种情况在表态那一刻就结算成 matched 了，
// 不会留到超时。所以成对引荐到期时，最多只有一方收到 intro_closed。
func closingNotices(in *model.Introduction) []closingNotice {
	if in.Kind != model.IntroPaired {
		return nil
	}
	out := make([]closingNotice, 0, 2)
	for _, side := range []string{model.SideLow, model.SideHigh} {
		uid := in.UserLow
		action := in.LowAction
		if side == model.SideHigh {
			uid, action = in.UserHigh, in.HighAction
		}
		switch {
		case action == nil:
			out = append(out, closingNotice{UserID: uid, Template: model.TplIntroMissed, Reason: "expired"})
		case *action == model.ActionLike:
			// 只有表过「想认识」的人才会收到这一条 ——
			// 他有过悬念，而悬念现在落地了。
			out = append(out, closingNotice{UserID: uid, Template: model.TplIntroClosed, Reason: "expired"})
		}
		// action == pass 走不到这里：任一方 pass 时这条引荐当场变成
		// declined，不再是未终结状态，不会进候选。
	}
	return out
}

// ExpireDue 跑一轮超时扫描，最多处理 limit 条。
//
// 一条一个事务，不是一批一个：一批 200 行共用一个事务，中间任何一行
// 出错都要整批回滚，而这一批里可能已经有十几条好好地终结掉了。
// 分开之后单条失败只丢这一条，下一轮还会把它捞起来（它的 state 没变，
// expires_at 也还在过去）。
func (s *Service) ExpireDue(ctx context.Context, limit int) (ExpireStats, error) {
	var st ExpireStats

	cands, err := s.Repo.ListExpiredIntros(ctx, limit)
	if err != nil {
		return st, err
	}
	st.Scanned = len(cands)
	if len(cands) == 0 {
		return st, nil
	}

	// now 取一次，整轮共用。一条一轮取一次的话，排在后面的行会用上
	// 更晚的时刻，而候选是按「更早那次 now」选出来的 —— 无关紧要，
	// 但同一次扫描里两行用两个不同的「现在」读起来像 bug。
	now := time.Now()

	for _, c := range cands {
		err := s.Repo.Tx(func(tx *gorm.DB) error {
			in, ok, err := s.Repo.ExpireIntro(tx, c.ID, now)
			if err != nil {
				return err
			}
			if !ok {
				st.Raced++
				return nil
			}
			st.Expired++
			metrics.IntroExpired.Inc()

			for _, n := range closingNotices(in) {
				side := sideOf(n.UserID, in.UserLow, in.UserHigh)
				err := s.EnqueueNotify(tx, n.UserID, n.Template,
					closingKey(in.ID, side, n.Template), map[string]any{
						"intro_id": in.ID,
						"reason":   n.Reason,
					})
				if err != nil {
					return err
				}
				st.Notified++
			}
			return nil
		})
		if err != nil {
			st.Failed++
			s.Log.ErrorContext(ctx, "终结引荐失败",
				slog.Int64("intro_id", c.ID), slog.Any("err", err))
		}
	}
	return st, nil
}

// closingKey 拼收尾通知的幂等键。
//
// 格式与 notifyDeclined 用的一致（intro:<id>:<side>:closed）：两条路径
// 不可能同时命中一条引荐（被拒绝的那条当场就终结了，不进超时候选），
// 但共用一个格式意味着万一将来有了交集，撞键而不是发两条。
func closingKey(introID int64, side, tpl string) string {
	switch tpl {
	case model.TplIntroMissed:
		return fmt.Sprintf("intro:%d:%s:missed", introID, side)
	default:
		return fmt.Sprintf("intro:%d:%s:closed", introID, side)
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/metrics"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/davisu-china/tidings/backend/internal/ws"
)

// 引荐的两段时限（§4.4 / §13.2）。
//
// 未查看的 72 小时在生成时就写进了 expires_at（introTTL，见
// introduction.go）。这两个是状态跃迁时重算的那两段：
// 打开了给 7 天考虑，表了态给 7 天等对方。
const (
	viewedTTL  = 7 * 24 * time.Hour
	respondTTL = 7 * 24 * time.Hour
)

// RespondResult 是一次表态的结果。
//
// 不回整张引荐卡：表态之后前端要的只有三件事 —— 这条现在什么状态、
// 成没成、成了的话会话在哪。把卡片再传一遍只会让「服务端算的和
// 前端手上那份哪个新」变成一个需要回答的问题。
type RespondResult struct {
	IntroID  int64  `json:"intro_id"`
	State    string `json:"state"`
	MyAction string `json:"my_action"`
	Matched  bool   `json:"matched"`
	MatchID  *int64 `json:"match_id,omitempty"`
}

// outcome 是表态的纯决策：给定「我这次的动作」与「对方已有的动作」，
// 返回这条引荐应当落到哪个状态。
//
// 抽成纯函数是为了能直接测 —— 这一段决定了「两个人都点想认识之后
// 会不会真的成匹配」，是整个 M3 的判据，不该只能靠起一个数据库来验。
//
// 三条分支对应 §13.1 的三条边：任一方 pass → declined；
// 双方 like → matched；一方 like 一方还没动 → responded。
func outcome(myAction string, otherAction *string) string {
	if myAction == model.ActionPass {
		return model.IntroDeclined
	}
	if otherAction != nil && *otherAction == model.ActionLike {
		return model.IntroMatched
	}
	return model.IntroResponded
}

// Respond 处理一次表态（§13.4）。
//
// 整个过程在一个事务里：写动作列 → 判双方是否都 like → 若成立则
// 建 match + 开会话 + 入队匹配通知。任何一步失败全回滚。
func (s *Service) Respond(
	ctx context.Context, user *model.User, introID int64, action, reason string,
) (*RespondResult, error) {
	if action != model.ActionLike && action != model.ActionPass {
		return nil, apierr.ErrBadRequest.WithMessage("action 只能是 like 或 pass")
	}

	// pass 必须带一个原因（决策 07：三选一，不要求输入文字）。
	// like 带了 reason 就丢掉 —— 「想认识」没有原因可言，
	// 为一个多余的字段拒掉用户的表态是没道理的。
	var reasonPtr *string
	if action == model.ActionPass {
		if !model.ValidReason(reason) {
			return nil, apierr.ErrReasonRequired
		}
		reasonPtr = &reason
	}

	now := time.Now()
	var out RespondResult
	// 事务提交之后才该做的事，先在这里记下来：
	//   matchedWith  有匹配成立时对方是谁，用来推实时事件
	//   counted      这一次是不是真的新增了一次表态（重放不算）
	var matchedWith int64
	var counted bool

	err := s.Repo.Tx(func(tx *gorm.DB) error {
		// 行锁：双方同时表态时串行化，避免都读到「对方还没表态」
		in, err := s.Repo.LockIntro(tx, introID)
		if err != nil {
			return err
		}
		// 不是当事人、或者是单向引荐的隐藏方，都当作不存在（§18.3）
		if in == nil || !in.VisibleTo(user.ID) {
			return apierr.ErrIntroNotFound
		}

		side := in.SideOf(user.ID)
		mine, theirs := sideActions(in, side)

		// 重放检查必须排在终结态检查前面。
		//
		// 「想认识」是盖下去就收不回的章（§19.3），前端在超时重试、
		// 用户连点两下、网络抖动重发这些情况下一定会重复提交同一个动作。
		// 先判终结态的话，双方表态后引荐变成 matched，先点的那个人
		// 一重试就会收到「你已经表过态了」—— 而他什么都没做错。
		if mine != nil {
			if *mine != action {
				return apierr.ErrIntroResponded
			}
			// 重放不推事件：什么都没变。真成了匹配的那一次已经把事件
			// 推过了，重试的这一次只欠前端一个「成了、会话在哪」的答案，
			// 而这个答案就在 HTTP 响应里。
			out = s.replayResult(tx, in, *mine)
			return nil
		}
		// 已经终结（matched / declined / expired），但这一侧没表过态
		if !model.IsOpenState(in.State) {
			return apierr.ErrIntroClosed
		}

		state := outcome(action, theirs)
		d := repoDecision(in, side, action, reasonPtr, now, state)
		if err := s.Repo.WriteDecision(tx, d); err != nil {
			return err
		}

		switch state {
		case model.IntroMatched:
			matchID, err := s.createMatch(tx, in, now)
			if err != nil {
				return err
			}
			matchedWith = in.Other(user.ID)
			out = RespondResult{
				IntroID: in.ID, State: state, MyAction: action,
				Matched: true, MatchID: &matchID,
			}
		case model.IntroDeclined:
			if err := s.notifyDeclined(tx, in, side); err != nil {
				return err
			}
			out = RespondResult{IntroID: in.ID, State: state, MyAction: action}
		default:
			out = RespondResult{IntroID: in.ID, State: state, MyAction: action}
		}

		counted = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 埋点与实时事件都放在提交之后。
	//
	// 埋点：记在事务里的话，回滚掉的那次表态会被算进去，而
	// 「表态数 / 引荐数」正是 §1.1 的主判据，多算一次就是判据失真。
	// 事件：回滚掉的那次表态已经推到对方浏览器上了，他的列表会
	// 显示一条并不存在的匹配。
	if counted {
		metrics.RecordResponse(action)
	}
	if matchedWith != 0 {
		s.publishMatch(ctx, user.ID, matchedWith, out)
	}
	return &out, nil
}

// sideActions 取这条引荐上「我」和「对方」各自的动作。
func sideActions(in *model.Introduction, side string) (mine, theirs *string) {
	if side == model.SideHigh {
		return in.HighAction, in.LowAction
	}
	return in.LowAction, in.HighAction
}

// repoDecision 把一次表态翻成一行待写入的 Decision。
//
// responded 要把时限推到「现在 + 7 天」；matched / declined 是终结态，
// 时限不再有读者，保持原值即可（写一个零值进去反而会让它变成一个
// 看起来已经过期的时刻）。
func repoDecision(
	in *model.Introduction, side, action string, reason *string, now time.Time, state string,
) repo.Decision {
	d := repo.Decision{
		IntroID:   in.ID,
		Side:      side,
		Action:    action,
		Reason:    reason,
		At:        now,
		State:     state,
		ExpiresAt: in.ExpiresAt,
	}
	if state == model.IntroResponded {
		d.ExpiresAt = now.Add(respondTTL)
	} else {
		d.ClosedAt = &now
	}
	return d
}

// replayResult 处理重放：同一个动作再来一次，按幂等成功返回。
//
// 不写任何东西、不发任何通知 —— 这是「用户重试」与「用户改主意」
// 的分界：同一个动作重试应当静默成功，换个动作才是错误。
//
// state 取自库里当前的值，不是「按这次动作重算一遍」：重放这一侧
// 没做任何事，引荐现在是什么状态它就是什么状态。
func (s *Service) replayResult(tx *gorm.DB, in *model.Introduction, action string) RespondResult {
	out := RespondResult{
		IntroID:  in.ID,
		State:    in.State,
		MyAction: action,
		Matched:  in.State == model.IntroMatched,
	}
	if out.Matched {
		// 重试的那一次也要拿到 match_id：前端要靠它跳进会话，
		// 而它手里那一份响应可能是丢在路上的那一份。
		if id := s.Repo.FindMatchID(tx, in.UserLow, in.UserHigh); id != 0 {
			out.MatchID = &id
		}
	}
	return out
}

// sideOwner 返回某一侧的 uid。
func sideOwner(in *model.Introduction, side string) int64 {
	if side == model.SideHigh {
		return in.UserHigh
	}
	return in.UserLow
}

// createMatch 建匹配、通知双方（§13.4 的「建 match + 开会话 + 入队匹配通知」）。
//
// 「开会话」没有单独的步骤：matches 那一行就是会话本身，消息挂在
// match_id 上。建完这一行，会话就开了。
func (s *Service) createMatch(tx *gorm.DB, in *model.Introduction, now time.Time) (int64, error) {
	matchID, created, err := s.Repo.CreateMatch(tx, in.UserLow, in.UserHigh)
	if err != nil {
		return 0, err
	}
	if matchID == 0 {
		// CreateMatch 只在「查回来了但仍然没有」时返回 0，
		// 也就是唯一约束与随后的查询之间被人删了行。不可能发生，
		// 但真发生时不该把一个 0 当成会话 id 发出去。
		return 0, errors.New("建匹配失败：唯一冲突后查不到匹配行")
	}
	if created {
		metrics.MatchesCreated.Inc()
	}

	// 双方都通知（§4.8：matched → 两边各一条）。
	// dedup key 带上侧别，同一条引荐对两个人各有一行。
	for _, uid := range []int64{in.UserLow, in.UserHigh} {
		key := fmt.Sprintf("intro:%d:%s:matched", in.ID, sideOf(uid, in.UserLow, in.UserHigh))
		err := s.EnqueueNotify(tx, uid, model.TplMatched, key, map[string]any{
			"intro_id": in.ID,
			"match_id": matchID,
		})
		if err != nil {
			return 0, err
		}
	}
	return matchID, nil
}

// notifyDeclined 处理「被明确拒绝」（§4.8）。
//
// 只有一种人收到：已经表过「想认识」、还在等回音的那一方，措辞与
// 超时释放的收尾通知完全相同（不区分原因 —— 被拒绝的人不需要知道
// 对方是嫌什么）。
//
// 「不合适」的一方不发：他做了一个决定，不需要被通知。
// 单向引荐的隐藏方永远不发：他从头到尾没收到过东西（§4.3）。
func (s *Service) notifyDeclined(tx *gorm.DB, in *model.Introduction, passSide string) error {
	other := model.SideLow
	if passSide == model.SideLow {
		other = model.SideHigh
	}

	otherAction := in.LowAction
	if other == model.SideHigh {
		otherAction = in.HighAction
	}
	// 对方没表过态、或者他也点了不合适：这条引荐对他从来没有过悬念，
	// 发一条「对方没有接受」只会让他莫名其妙。
	if otherAction == nil || *otherAction != model.ActionLike {
		return nil
	}

	uid := sideOwner(in, other)
	if !in.VisibleTo(uid) {
		return nil
	}

	key := fmt.Sprintf("intro:%d:%s:closed", in.ID, other)
	return s.EnqueueNotify(tx, uid, model.TplIntroClosed, key, map[string]any{
		"intro_id": in.ID,
		// reason 区分「被拒绝」与 M4 的「超时释放」。文案相同
		// （§4.8：不区分原因，被拒绝的人不需要知道对方嫌什么），
		// 但埋点要能拆开这两件事。
		"reason": "declined",
	})
}

// publishMatch 往双方的实时连接推一条「成匹配了」。
//
// 我这一侧同样收到：另一个标签页、另一台设备都在等这条。
// 前端对同一个匹配重复收到只是在重复失效一次缓存，不会出问题。
func (s *Service) publishMatch(ctx context.Context, meID, otherID int64, out RespondResult) {
	if s.Events == nil {
		return
	}
	payload := map[string]any{
		"intro_id": out.IntroID,
		"match_id": out.MatchID,
	}
	for _, uid := range []int64{meID, otherID} {
		if err := s.Events.Publish(ctx, uid, ws.Event{Type: ws.EventMatch, Data: payload}); err != nil {
			// 推不到不该影响表态本身：这条引荐已经在库里定下来了，
			// 对方下次打开列表就能看到
			s.Log.WarnContext(ctx, "推送匹配事件失败",
				slog.Int64("user_id", uid), slog.Any("err", err))
		}
	}
}

// ---------------------------------------------------------------- 详情

// GetIntroduction 取一条引荐的详情，并把「打开」这件事实落下来。
//
// 「打开」就在这里发生，不在列表接口里（§13.1 的「任一方打开」）。
// 放在列表里会有两个后果：一是列表在每个页面都会被取（底部导航的
// 未读角标就挂着同一条 query），任何一次后台刷新都会把信标成已读；
// 二是一屏之内所有引荐一起被标已读，包括那些根本没滚到的。
//
// 详情页是用户明确点进来的，一次点击对应一封信。
func (s *Service) GetIntroduction(ctx context.Context, user *model.User, introID int64) (*IntroView, error) {
	row, err := s.Repo.GetIntroRow(ctx, user.ID, introID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, apierr.ErrIntroNotFound
	}

	// 「打开」的跃迁。MarkViewed 只在 pending 时推时限，所以
	// 第二个打开的人不会把先看那个人的七天重新算一遍（§13.2）。
	//
	// 已终结的引荐不写这一笔：那是一封已经封上的信，打开它不该
	// 在库里留下任何痕迹。
	if row.MyViewedAt == nil && model.IsOpenState(row.State) {
		if _, err := s.Repo.MarkViewed(ctx, introID, row.MySide, time.Now().Add(viewedTTL)); err != nil {
			return nil, err
		}
		// 重新取一次：状态、时限、未读都变了
		row, err = s.Repo.GetIntroRow(ctx, user.ID, introID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, apierr.ErrIntroNotFound
		}
	}

	view := s.toIntroView(*row)
	// 详情页是「引荐卡全文」（§4.6 的展示清单里有职业，卡片草图上没有）
	view.Other.Occupation = row.Occupation
	return &view, nil
}

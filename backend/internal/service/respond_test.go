package service

import (
	"testing"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// 表态的决策表。这是 M3 的判据本身 —— 「两个人都点了想认识之后
// 到底会不会成匹配」全靠这几行，而它不该只能靠起一个数据库来验。

func TestOutcome(t *testing.T) {
	like := model.ActionLike
	pass := model.ActionPass

	cases := []struct {
		name  string
		mine  string
		their *string
		want  string
	}{
		{"双方都想认识 → 成匹配", like, &like, model.IntroMatched},
		{"我先点了想认识，对方还没动 → 等对方", like, nil, model.IntroResponded},
		{"我点了不合适 → 直接结束", pass, nil, model.IntroDeclined},
		{
			// 这个组合到不了这里：对方先 pass 时引荐已经终结，
			// 我这一侧的表态会被「引荐已经结束了」挡掉。
			// 留着它是为了让这个函数是全函数 —— 少一条分支的
			// switch 迟早会有人补错。
			"对方已不合适，我才点想认识 → 不成立匹配", like, &pass, model.IntroResponded,
		},
		{"对方已想认识，我点不合适 → 结束而不是匹配", pass, &like, model.IntroDeclined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(tc.mine, tc.their); got != tc.want {
				t.Fatalf("outcome(%q, %v) = %q，期望 %q", tc.mine, ptrStr(tc.their), got, tc.want)
			}
		})
	}
}

// 时限与终结时刻是 §13.2 的计时器，错了不会报错，只会让人早几天
// 或晚几天收到收尾通知。
func TestRepoDecisionDeadlines(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	origExpires := now.Add(48 * time.Hour)
	reason := model.ReasonVibe

	in := &model.Introduction{ID: 7, UserLow: 1, UserHigh: 2, ExpiresAt: origExpires}

	t.Run("等对方回音：时限推到七天后", func(t *testing.T) {
		d := repoDecision(in, model.SideLow, model.ActionLike, nil, now, model.IntroResponded)
		if !d.ExpiresAt.Equal(now.Add(respondTTL)) {
			t.Fatalf("expires_at = %v，期望 %v", d.ExpiresAt, now.Add(respondTTL))
		}
		if d.ClosedAt != nil {
			t.Fatal("还没终结，closed_at 不该有值")
		}
		if d.At != now {
			t.Fatalf("action_at = %v，期望 %v", d.At, now)
		}
	})

	t.Run("终结态：时限不动，写上终结时刻", func(t *testing.T) {
		for _, state := range []string{model.IntroMatched, model.IntroDeclined} {
			d := repoDecision(in, model.SideHigh, model.ActionPass, &reason, now, state)
			if !d.ExpiresAt.Equal(origExpires) {
				t.Fatalf("%s：expires_at = %v，终结之后它没有读者，不该被改写", state, d.ExpiresAt)
			}
			if d.ClosedAt == nil || !d.ClosedAt.Equal(now) {
				t.Fatalf("%s：closed_at 应该是 %v", state, now)
			}
		}
	})

	t.Run("不合适的原因只跟着 pass 走", func(t *testing.T) {
		d := repoDecision(in, model.SideLow, model.ActionPass, &reason, now, model.IntroDeclined)
		if d.Reason == nil || *d.Reason != model.ReasonVibe {
			t.Fatalf("reason = %v，期望 %q", d.Reason, model.ReasonVibe)
		}
		if d.Action != model.ActionPass || d.Side != model.SideLow {
			t.Fatalf("动作列写错了：side=%q action=%q", d.Side, d.Action)
		}
	})
}

// 三个原因取值必须与前端字典一致，且互相不等。
func TestValidReason(t *testing.T) {
	all := []string{model.ReasonMismatch, model.ReasonVibe, model.ReasonOther}
	seen := map[string]bool{}
	for _, r := range all {
		if !model.ValidReason(r) {
			t.Fatalf("%q 应该是合法原因", r)
		}
		if seen[r] {
			t.Fatalf("原因 %q 重复", r)
		}
		seen[r] = true
	}
	if model.ValidReason("") || model.ValidReason("条件不符") {
		t.Fatal("空串和中文文案都不该被当成合法原因 —— 存的是 code")
	}
}

// 未终结的三个状态是 §13.1 状态机的分界，候选集 SQL、超时扫描
// 和这里都按它判断。
func TestIsOpenState(t *testing.T) {
	for _, s := range []string{model.IntroPending, model.IntroViewed, model.IntroResponded} {
		if !model.IsOpenState(s) {
			t.Fatalf("%q 应该算未终结", s)
		}
	}
	for _, s := range []string{model.IntroMatched, model.IntroDeclined, model.IntroExpired, ""} {
		if model.IsOpenState(s) {
			t.Fatalf("%q 不该算未终结", s)
		}
	}
}

func ptrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

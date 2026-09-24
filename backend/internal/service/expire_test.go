package service

import (
	"testing"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// 超时释放的收件人决策表。§22 的 M4 判据是「四类通知都正确到达」，
// 而「到达」的对错全在这一个函数里 —— 它不该只能靠起一个数据库来验。

// noticeMap 把通知列表压成 uid -> 模板，方便断言。
// 同一个 uid 收到两条会互相覆盖，而那本身就是错的，所以下面有单独的用例
// 断言条数。
func noticeMap(ns []closingNotice) map[int64]string {
	m := make(map[int64]string, len(ns))
	for _, n := range ns {
		m[n.UserID] = n.Template
	}
	return m
}

func strPtr(s string) *string { return &s }

func TestClosingNotices(t *testing.T) {
	low, high := int64(1), int64(2)
	like := model.ActionLike
	pass := model.ActionPass

	cases := []struct {
		name string
		in   *model.Introduction
		want map[int64]string
	}{
		{
			name: "成对·两边都没表态 → 两边都收到「你错过了」",
			in: &model.Introduction{
				Kind: model.IntroPaired, UserLow: low, UserHigh: high,
			},
			want: map[int64]string{low: model.TplIntroMissed, high: model.TplIntroMissed},
		},
		{
			name: "成对·低侧表过想认识 → 他收到收尾，对面收到错过",
			in: &model.Introduction{
				Kind: model.IntroPaired, UserLow: low, UserHigh: high,
				LowAction: &like,
			},
			want: map[int64]string{low: model.TplIntroClosed, high: model.TplIntroMissed},
		},
		{
			name: "成对·高侧表过想认识 → 镜像",
			in: &model.Introduction{
				Kind: model.IntroPaired, UserLow: low, UserHigh: high,
				HighAction: &like,
			},
			want: map[int64]string{low: model.TplIntroMissed, high: model.TplIntroClosed},
		},
		{
			// 可见方（低侧）收到过这封信，但按 §22 的判据一律不发。
			// 见 closingNotices 的注释：这一处文档自相矛盾，
			// 取的是从严的那一支。
			name: "单向·隐藏方是高侧 → 两边都不发",
			in: &model.Introduction{
				Kind: model.IntroOneway, UserLow: low, UserHigh: high,
				HiddenSide: strPtr(model.SideHigh),
			},
			want: map[int64]string{},
		},
		{
			name: "单向·隐藏方是低侧，且可见方已表过想认识 → 仍然两边都不发",
			in: &model.Introduction{
				Kind: model.IntroOneway, UserLow: low, UserHigh: high,
				HiddenSide: strPtr(model.SideLow), HighAction: &like,
			},
			want: map[int64]string{},
		},
		{
			// 到不了这里：任一方 pass 时这条引荐当场变成 declined，
			// 不再进超时候选。留着是为了让函数是全函数 —— 万一将来
			// 有了别的进入 expired 的路径，也不会给一个已经表过态
			// 「不合适」的人发一条莫名其妙的信。
			name: "成对·某侧表过不合适 → 不给他发",
			in: &model.Introduction{
				Kind: model.IntroPaired, UserLow: low, UserHigh: high,
				LowAction: &pass,
			},
			want: map[int64]string{high: model.TplIntroMissed},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := closingNotices(tc.in)

			// 先断言「有没有重复发给同一个人」—— 上面那个 map 会把它吃掉，
			// 而重复发是这条链路上最像正确行为的错误（dedup_key 会挡住
			// 重复的那条，于是线上表现为「少发了一条」，很难查）。
			seen := map[int64]bool{}
			for _, n := range got {
				if seen[n.UserID] {
					t.Fatalf("uid %d 收到了两条收尾通知", n.UserID)
				}
				seen[n.UserID] = true
			}

			if len(got) != len(tc.want) {
				t.Fatalf("发出 %d 条，期望 %d 条：%v", len(got), len(tc.want), noticeMap(got))
			}
			for uid, tpl := range tc.want {
				if m := noticeMap(got); m[uid] != tpl {
					t.Fatalf("uid %d 收到 %q，期望 %q", uid, m[uid], tpl)
				}
			}
		})
	}
}

// 收尾通知的幂等键必须按侧别分开。
//
// 同一条引荐对两个人各有一行 outbox，键里不带侧别的话第二行会撞唯一约束
// 而被静默丢掉 —— 表现为「只发给了其中一个人」，没有任何报错。
func TestClosingKey(t *testing.T) {
	lowKey := closingKey(7, model.SideLow, model.TplIntroMissed)
	highKey := closingKey(7, model.SideHigh, model.TplIntroMissed)
	if lowKey == highKey {
		t.Fatalf("两个侧别的幂等键相同：%q", lowKey)
	}

	// 「错过」与「收尾」也是两件事，不能共键：一个人完全可能
	// 先收到「你错过了」，同一封信的这一侧不该再被「收尾」覆盖掉。
	if closingKey(7, model.SideLow, model.TplIntroMissed) ==
		closingKey(7, model.SideLow, model.TplIntroClosed) {
		t.Fatal("错过与收尾用了同一个幂等键")
	}

	// 与 notifyDeclined 的键格式对齐（intro:<id>:<side>:closed）。
	// 两条路径不会同时命中一条引荐，但格式一致意味着万一有了交集，
	// 结果是撞键去重而不是给同一个人发两条。
	if got := closingKey(7, model.SideLow, model.TplIntroClosed); got != "intro:7:low:closed" {
		t.Fatalf("收尾的幂等键 = %q，期望 intro:7:low:closed", got)
	}
}

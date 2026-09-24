package repo

import (
	"testing"
	"time"
)

// 未读的语义（§19.2 的角标）。这一段是纯函数，而它判错的表现
// 是一个点不掉的红点、或者一个该亮却不亮的角标 —— 两种都不会
// 报错，只会让人以为产品坏了。
func TestIsUnread(t *testing.T) {
	viewed := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		at     *time.Time
		state  string
		unread bool
	}{
		{"没看过、还没人动过 → 未读", nil, "pending", true},
		{
			// 这一条是修复本身：成对引荐里先看的往往是对方，
			// 他一看 state 就翻成 viewed，而另一个人从没打开过。
			// 按 state 判会把他的角标一起弄没。
			"没看过、但对方已经看过了 → 仍然是未读", nil, "viewed", true,
		},
		{"没看过、我已经表过态 → 未读（极少见，但不该漏）", nil, "responded", true},
		{"没看过、已经成匹配 → 未读（列表上要给入口）", nil, "matched", true},
		{"没看过、已经不合适 → 不是未读（封上的信）", nil, "declined", false},
		{"没看过、已经超时 → 不是未读", nil, "expired", false},
		{"看过 → 不是未读", &viewed, "pending", false},
		{"看过、之后成匹配 → 不是未读", &viewed, "matched", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnread(tc.at, tc.state); got != tc.unread {
				t.Fatalf("isUnread(%v, %q) = %v，期望 %v", tc.at, tc.state, got, tc.unread)
			}
		})
	}
}

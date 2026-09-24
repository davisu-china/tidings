package service

import (
	"context"
	"log/slog"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/repo"
)

// 用户设置（§18.2 的 GET PUT /me/settings、§4.5 的三道闸）。
//
// 三个字段对应三道闸里的两道：intros_paused 是用户自己按的暂停，
// quiet_start/quiet_end 是静默时段。第三道「未响应冻结」不给用户直接改 ——
// 它是系统按行为判定的，只在响应里报出去让人知道「推送被暂停了、为什么」。

// SettingsView 是 GET /me/settings 的响应。
//
// 带上 PushFrozen 和 UnopenedStreak 这两个用户改不了的字段：
// 推送被系统暂停却不说，用户只会以为推送坏了，然后去把通知权限关掉 ——
// 那一步之后就再也回不来了。
type SettingsView struct {
	IntrosPaused bool  `json:"intros_paused"`
	QuietStart   int16 `json:"quiet_start"`
	QuietEnd     int16 `json:"quiet_end"`

	PushFrozen     bool  `json:"push_frozen"`
	UnopenedStreak int16 `json:"unopened_streak"`
}

// SettingsInput 是 PUT /me/settings 的请求体。
//
// 三个字段都是指针，缺一个就报错而不是当默认值用。PUT 的语义是整体替换，
// 而 quiet_start 的零值 0 是一个合法取值（零点）—— 分不清「他没传」
// 和「他要零点」的话，前端漏传一个字段就会把静默时段悄悄挪到半夜，
// 而这件事不会有任何报错。
type SettingsInput struct {
	IntrosPaused *bool  `json:"intros_paused"`
	QuietStart   *int16 `json:"quiet_start"`
	QuietEnd     *int16 `json:"quiet_end"`
}

// GetSettings 读本人的设置。
func (s *Service) GetSettings(ctx context.Context, user *model.User) (*SettingsView, error) {
	row, err := s.Repo.GetUserSettings(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &SettingsView{
		IntrosPaused:   row.IntrosPaused,
		QuietStart:     row.QuietStart,
		QuietEnd:       row.QuietEnd,
		PushFrozen:     row.PushFrozen,
		UnopenedStreak: row.UnopenedStreak,
	}, nil
}

// SaveSettings 全量替换本人的设置。
//
// 恢复接收引荐时，暂停期间被冻住的时限会在同一个事务里顺延回来
// （§13.2 的计时冻结，实现见 repo.SaveUserSettings）—— 用户按一下开关，
// 等他回音的那些人不会因为这段暂停而被判超时。
func (s *Service) SaveSettings(ctx context.Context, user *model.User, in *SettingsInput) (*SettingsView, error) {
	if in.IntrosPaused == nil || in.QuietStart == nil || in.QuietEnd == nil {
		return nil, apierr.ErrBadRequest.WithMessage(
			"intros_paused / quiet_start / quiet_end 三个字段都要传")
	}
	if *in.QuietStart < 0 || *in.QuietStart > 23 || *in.QuietEnd < 0 || *in.QuietEnd > 23 {
		// 与表上的 settings_quiet_chk 同一把尺子。在这里先拦一道是为了
		// 给出人能看懂的错，而不是让约束冲突折叠成 INTERNAL。
		return nil, apierr.ErrBadRequest.WithMessage("静默时段的小时数要在 0–23 之间")
	}

	_, err := s.Repo.SaveUserSettings(ctx, user.ID, repo.SettingsWrite{
		IntrosPaused: *in.IntrosPaused,
		QuietStart:   *in.QuietStart,
		QuietEnd:     *in.QuietEnd,
	})
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, user)
}

// TouchVisit 记一次「他来了」，解除未响应冻结（§4.5）。
//
// 挂在 GET /me 上：那是每次打开应用都会走的那一条（前端 rootLoader
// 拿它决定落地页与身份）。「直到用户主动访问一次」里的「访问一次」
// 就是这一次，没有更贴切的信号了。
//
// 失败只记日志不往上抛：这是打开应用路上的一个副作用，让它在
// 这个人打开应用时弹一个 500 是荒谬的。解冻本来也会在下一次访问时重试。
func (s *Service) TouchVisit(ctx context.Context, user *model.User) {
	if err := s.Repo.ClearPushFreeze(ctx, user.ID); err != nil {
		s.Log.WarnContext(ctx, "解除推送冻结失败",
			slog.Int64("user_id", user.ID), slog.Any("err", err))
	}
}

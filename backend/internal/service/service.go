// Package service 是业务层：所有规则、状态机、事务边界都在这里。
// handler 只做参数解析与响应封装，repo 只做数据访问。
package service

import (
	"context"
	"log/slog"

	"github.com/davisu-china/tidings/backend/internal/config"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
	"github.com/davisu-china/tidings/backend/internal/pkg/password"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/davisu-china/tidings/backend/internal/ws"
)

// Service 是各领域服务的载体，同时也是依赖的装配点。
//
// M0 只有基建依赖。后续里程碑往这里加字段，不从别处取：
// M1 加了 JWT / 密码哈希，M2 加 push，M4 加收尾通知的模板表。
type Service struct {
	Cfg     *config.Config
	Repo    *repo.Repo
	Storage *storage.Client
	Log     *slog.Logger

	// JWT 签发与校验 access / refresh token。
	JWT *jwtutil.Manager
	// Equalizer 在「邮箱不存在」的分支上烧掉一次等量的 bcrypt 时间，
	// 让登录失败的三条路径耗时一致（见 pkg/password）。
	Equalizer *password.Equalizer

	// Events 是站内实时通道（M3 的 WebSocket）。
	//
	// 声明成接口而不是 *ws.Hub：service 只需要「把一条事件推给某人」
	// 这一件事，而接口让单测里可以塞一个记录用的假实现。
	// worker 进程不装配它（nil），那里的事件推送会被静默跳过 ——
	// 实时推送是锦上添花，不是真相，缺了它表里的状态一样是对的。
	Events EventPublisher
}

// EventPublisher 是实时通道对外的那一个方法。
type EventPublisher interface {
	Publish(ctx context.Context, uid int64, ev ws.Event) error
}

func New(
	cfg *config.Config,
	r *repo.Repo,
	st *storage.Client,
	jwtMgr *jwtutil.Manager,
	eq *password.Equalizer,
	log *slog.Logger,
) *Service {
	return &Service{
		Cfg:       cfg,
		Repo:      r,
		Storage:   st,
		JWT:       jwtMgr,
		Equalizer: eq,
		Log:       log,
	}
}

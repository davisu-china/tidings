// Command worker 是后台常驻任务的入口。
//
// 与 api 分开部署，但共用同一套 config / repo / service —— 业务规则
// 一份都不复制。这样 worker 里发生的每一次写入，走的都是 api 用的
// 那套校验和事务边界，不会出现「后台任务绕过了某条规则」这类问题。
//
// 三个循环都在这里：outbox（2 秒）、generate（5 分钟）、expire（1 分钟）。
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/davisu-china/tidings/backend/internal/config"
	"github.com/davisu-china/tidings/backend/internal/pkg/database"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
	"github.com/davisu-china/tidings/backend/internal/pkg/logger"
	"github.com/davisu-china/tidings/backend/internal/pkg/password"
	"github.com/davisu-china/tidings/backend/internal/pkg/push"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/davisu-china/tidings/backend/internal/service"
	"github.com/davisu-china/tidings/backend/internal/worker"
	"golang.org/x/sync/errgroup"
)

// outbox 的循环周期与批大小（§15 的表）。这两个值与 stubborn-love
// 保持一致，不另调。
const (
	outboxInterval = 2 * time.Second
	outboxLease    = 2 * time.Minute
	outboxBatch    = 50
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		os.Stderr.WriteString("加载配置失败: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := logger.New(cfg.Env)
	log.Info("启动 worker",
		"env", cfg.Env,
		"intro_interval", cfg.Intro.GenInterval,
		"intro_lease", cfg.Intro.GenLease,
		"push_configured", cfg.Push.Configured(),
	)

	// 迁移不在 worker 里跑：api 已经在启动时跑过了。两处都跑会引入
	// 一个没必要处理的并发迁移场景。
	//
	// 第三个参数是「要不要打印每条 SQL」。这里固定传 false，即使
	// 开发环境也不打：outbox 每 2 秒查一次库，开着它一分钟就是上百行
	// SELECT，worker 自己那几行关键日志（投递失败、生成结果）会被淹没。
	// 需要看 SQL 时把这里改成 true，或者直接去 api 那边看 ——
	// 同一个库、同一套查询。
	db, err := database.NewPostgres(cfg.Postgres, false, log)
	if err != nil {
		log.Error("连接 postgres 失败", "err", err)
		os.Exit(1)
	}

	rdb, err := database.NewRedis(cfg.Redis, log)
	if err != nil {
		log.Error("连接 redis 失败", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// MinIO 与密码比对器是 service.New 的必需依赖，但 worker 一个都
	// 用不到。仍然照常构造：让 service 的装配点在两个入口保持一致，
	// 比省下这一次连接重要 —— 哪天 worker 要用到派生图，
	// 不会出现「api 有、worker 没有」的意外。
	st, err := storage.New(cfg.MinIO, log)
	if err != nil {
		log.Error("初始化 minio 失败", "err", err)
		os.Exit(1)
	}
	eq, err := password.NewEqualizer(cfg.Auth.BcryptCost)
	if err != nil {
		log.Error("初始化密码比对器失败", "err", err)
		os.Exit(1)
	}

	svc := service.New(cfg, repo.New(db, rdb), st, jwtutil.NewManager(cfg.JWT), eq, log)

	gen, err := worker.NewGenerateWorker(svc, log, cfg.Intro.GenInterval, cfg.Intro.GenLease)
	if err != nil {
		// 租约 ≥ 周期时这里就会失败。宁可启动不了，
		// 也不要带着一个会静默停摆的配置跑起来。
		log.Error("初始化引荐生成任务失败", "err", err)
		os.Exit(1)
	}

	exp, err := worker.NewExpireWorker(svc, log,
		cfg.Intro.ExpireInterval, cfg.Intro.ExpireLease, cfg.Intro.ExpireBatch)
	if err != nil {
		log.Error("初始化超时扫描任务失败", "err", err)
		os.Exit(1)
	}

	// 没配 VAPID 时不阻断启动，只是推送发不出去：开发环境跑通
	// 生成链路（引荐落库、outbox 有行）本身就是有价值的，
	// 而生产环境缺密钥在 config 校验里已经硬失败了。
	var outbox *worker.OutboxWorker
	if cfg.Push.Configured() {
		outbox = &worker.OutboxWorker{
			Repo:     svc.Repo,
			Sender:   push.NewSender(cfg.Push.PublicKey, cfg.Push.PrivateKey, cfg.Push.Subject),
			Log:      log,
			Interval: outboxInterval,
			Lease:    outboxLease,
			Batch:    outboxBatch,
			// 静默时段按用户那边的钟点算，不是按容器里的钟点。
			// 容器默认 UTC，不显式给的话 22:00–09:00 会落到国内白天。
			Loc: cfg.Location(),
		}
	} else {
		log.Warn("未配置 VAPID 密钥，outbox 投递循环不会启动；通知会留在队列里")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return gen.Run(ctx) })
	// 超时扫描与推送无关：它只改库里的状态、往 outbox 入队。
	// 没配 VAPID 时它照跑 —— 开发环境正是靠它把引荐推到终结状态。
	g.Go(func() error { return exp.Run(ctx) })
	if outbox != nil {
		g.Go(func() error { return outbox.Run(ctx) })
	}

	// errgroup 在第一个循环返回非 nil 时取消 ctx，另一个循环随之退出。
	// 所以任意一个循环意外死掉都会让整个进程退出并被重启拉起，
	// 而不是留着一个半死的 worker 静默不干活。
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("worker 退出", "err", err)
		os.Exit(1)
	}
	log.Info("worker 已退出")
}

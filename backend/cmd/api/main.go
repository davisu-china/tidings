// Command api 是 HTTP 服务入口。
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/davisu-china/tidings/backend/internal/config"
	"github.com/davisu-china/tidings/backend/internal/handler"
	"github.com/davisu-china/tidings/backend/internal/pkg/database"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
	"github.com/davisu-china/tidings/backend/internal/pkg/logger"
	"github.com/davisu-china/tidings/backend/internal/pkg/password"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/davisu-china/tidings/backend/internal/service"
	"github.com/davisu-china/tidings/backend/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// 配置还没加载出来，logger 也还没建，只能用标准错误
		os.Stderr.WriteString("加载配置失败: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := logger.New(cfg.Env)
	log.Info("启动 api",
		"env", cfg.Env,
		"addr", cfg.HTTPAddr,
		"match_threshold", cfg.Match.Threshold,
		"intro_interval", cfg.Intro.GenInterval,
	)

	// 迁移先于一切：schema 不对，后面全是错的
	if err := database.Migrate(cfg.Postgres.MigrateURL(), log); err != nil {
		log.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	db, err := database.NewPostgres(cfg.Postgres, cfg.Env == "development", log)
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

	st, err := storage.New(cfg.MinIO, log)
	if err != nil {
		log.Error("初始化 minio 失败", "err", err)
		os.Exit(1)
	}

	// 假哈希在这里生成一次，用配置里的 cost —— 它的唯一作用是让
	// 「邮箱不存在」和「密码错误」耗时一致，因此必须和真实哈希同价。
	// 生成失败说明 cost 有问题，直接退出比带着时序泄露继续跑好。
	eq, err := password.NewEqualizer(cfg.Auth.BcryptCost)
	if err != nil {
		log.Error("初始化密码比对器失败", "err", err)
		os.Exit(1)
	}

	svc := service.New(cfg, repo.New(db, rdb), st, jwtutil.NewManager(cfg.JWT), eq, log)

	// 实时通道（§4.7 的「实时收发」）。只下行：发消息走 HTTP POST，
	// 见 internal/ws 顶部的说明。service 通过这个接口推事件，
	// 它不知道连接是怎么管的。
	//
	// 退出时先取消它再关 HTTP：订阅循环和推送都是后台协程，
	// 让它们在请求收尾之前停下来，日志的先后顺序才对得上。
	bgCtx, stopBg := context.WithCancel(context.Background())
	defer stopBg()

	hub := ws.NewHub(rdb, log)
	svc.Events = hub
	go hub.Run(bgCtx)

	// 等订阅确认再开始服务：Redis 的 SUBSCRIBE 是「发出去就返回」，
	// 不等确认的话，进程刚起来那一瞬间的事件会静默丢掉。
	// 等不到也照常启动 —— 实时通道不是必需品，业务不靠它。
	select {
	case <-hub.Ready():
	case <-time.After(3 * time.Second):
		log.Warn("实时通道订阅未确认，继续启动")
	}

	h := handler.New(svc, hub, log)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Router(cfg.Env),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http 服务异常退出", "err", err)
			os.Exit(1)
		}
	}()
	log.Info("api 已就绪", "addr", cfg.HTTPAddr)

	// 优雅退出：先停止接收新请求，再给在途请求 15 秒收尾
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("收到退出信号，开始优雅关闭")

	stopBg()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("优雅关闭超时", "err", err)
	}
	log.Info("api 已退出")
}

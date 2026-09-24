package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/service"
)

// GenerateWorker 定期为用户生成引荐（§15.2）。
//
// 它是唯一一个带租约的任务：两个实例同时跑会重复推人。
// 租约是「条件 UPDATE 抢一行」，语义与 FOR UPDATE SKIP LOCKED 同源，
// 只是持有时长从「一个事务」变成了「几分钟」。
//
// 但租约只防重复劳动，不保证正确性 —— 见 §15.1 的说明。
// 真正的正确性是 uniq_intro_open_pair：即使两个实例同时跑完全程，
// 唯一索引也会挡住第二行。
type GenerateWorker struct {
	Svc *service.Service
	Log *slog.Logger

	Interval time.Duration
	Lease    time.Duration
}

// NewGenerateWorker 建生成任务，并在启动时断言租约短于循环周期。
//
// 这个关系是硬性的：租约 ≥ 周期时，上一轮的租约还没过期，
// 下一轮永远抢不到锁 —— 任务静默停摆，日志里一行错都没有。
// 所以宁可启动就失败，也不要带着这个配置跑起来。
//
// 池子下限（INTRO_POOL_MIN）不在这里传：它是生成策略的一部分，
// 由 service 从配置里读，与阈值折扣一起决定分档（§12.4）。
func NewGenerateWorker(svc *service.Service, log *slog.Logger, interval, lease time.Duration) (*GenerateWorker, error) {
	if lease >= interval {
		return nil, fmt.Errorf(
			"引荐生成任务的租约（%s）必须短于循环周期（%s），否则任务会静默停摆",
			lease, interval)
	}
	if interval <= 0 {
		return nil, errors.New("引荐生成任务的循环周期必须大于 0")
	}
	return &GenerateWorker{Svc: svc, Log: log, Interval: interval, Lease: lease}, nil
}

func (w *GenerateWorker) Run(ctx context.Context) error {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.Log.InfoContext(ctx, "引荐生成 worker 启动",
		slog.Duration("interval", w.Interval),
		slog.Duration("lease", w.Lease))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 单轮失败不退出：数据库抖一下就让 worker 死掉，
				// 等于停止所有引荐生成，而没有任何东西会报警
				w.Log.ErrorContext(ctx, "引荐生成轮次失败", slog.Any("err", err))
			}
		}
	}
}

// RunOnce 跑一轮：抢租约 → 生成 → 放开租约。
//
// 抢不到租约时直接返回 nil（不是错误）：另一个实例正在跑，
// 这是设计内的正常情况，不是故障。
func (w *GenerateWorker) RunOnce(ctx context.Context) error {
	ok, err := w.Svc.Repo.ClaimLease(ctx, model.JobIntroGenerate, w.Lease)
	if err != nil {
		return err
	}
	if !ok {
		w.Log.DebugContext(ctx, "引荐生成：租约在别处，本轮跳过")
		return nil
	}

	// 无论成功失败都要放开租约。不放开的话，跑完还要空等到租约到期，
	// 下一轮才可能开始 —— 白白损失一个周期。
	var result string
	stats, err := w.Svc.GenerateIntroductions(ctx)
	if err != nil {
		result = "error: " + err.Error()
	} else {
		result = summarize(stats)
	}
	if e := w.Svc.Repo.FinishLease(ctx, model.JobIntroGenerate, result); e != nil {
		// 放开租约失败不该把这一轮的成果也算作失败：引荐已经生成好了，
		// 代价只是下一轮要等到租约自然过期
		w.Log.ErrorContext(ctx, "放开租约失败", slog.Any("err", e))
	}
	return err
}

// summarize 把一轮结果压成一行存进 job_runs.last_result。
// 运维靠它回答「生成任务还在干活吗」，所以数字要在一行里看全。
func summarize(s service.GenStats) string {
	return fmt.Sprintf(
		"cities=%d skipped_towns=%d users=%d paired=%d oneway=%d upgraded=%d duplicates=%d skipped=%d",
		s.Cities, s.SkippedTowns, s.Users, s.Paired, s.Oneway, s.Upgraded, s.Duplicates, s.Skipped)
}

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

// ExpireWorker 是 §15 的第三个常驻循环：扫描到期引荐、推进状态、
// 入队收尾通知。M4 之前这个循环不存在 —— 引荐只生成、不终结。
type ExpireWorker struct {
	Svc *service.Service
	Log *slog.Logger

	// Interval 是循环周期（§15：1 分钟）。
	Interval time.Duration
	// Lease 是这一轮的租约，必须严格小于 Interval（启动时断言）。
	Lease time.Duration
	// Batch 是一轮最多终结几条（§15.3：200 行一轮，避免长事务）。
	//
	// 注意这不像 outbox 那样是「每轮配额」：一轮处理不完的候选会在
	// 下一轮继续，因为它们的 state 没变、expires_at 还在过去，
	// 下一轮的查询照样能捞到。所以积压会自己消化，
	// 不需要把 Batch 调大到能一次吞下全部。
	Batch int
}

// NewExpireWorker 构造时就把「租约必须短于周期」拦下来。
//
// 配置校验里已经有一道同样的检查，这里再来一次是因为这个不变式
// 一旦破了，表现是任务静默停摆 —— 没有任何报错，只是引荐再也不终结。
// 宁可启动不了。
func NewExpireWorker(svc *service.Service, log *slog.Logger, interval, lease time.Duration, batch int) (*ExpireWorker, error) {
	if lease >= interval {
		return nil, fmt.Errorf("超时扫描的租约(%s)必须小于周期(%s)", lease, interval)
	}
	if batch < 1 {
		return nil, fmt.Errorf("超时扫描的批大小至少为 1，当前 %d", batch)
	}
	return &ExpireWorker{Svc: svc, Log: log, Interval: interval, Lease: lease, Batch: batch}, nil
}

func (w *ExpireWorker) Run(ctx context.Context) error {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.Log.InfoContext(ctx, "超时扫描 worker 启动",
		slog.Duration("interval", w.Interval),
		slog.Duration("lease", w.Lease),
		slog.Int("batch", w.Batch))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 单轮失败不退出：数据库抖一下就让 worker 死掉，
				// 等于把所有到期引荐压到重启为止。
				w.Log.ErrorContext(ctx, "超时扫描轮次失败", slog.Any("err", err))
			}
		}
	}
}

func (w *ExpireWorker) RunOnce(ctx context.Context) error {
	ok, err := w.Svc.Repo.ClaimLease(ctx, model.JobIntroExpire, w.Lease)
	if err != nil {
		return err
	}
	if !ok {
		// 另一个实例正在跑，本轮跳过。租约不保证正确性，
		// 只避免重复劳动 —— 重复跑也不会发两条通知（dedup_key 兜底）。
		return nil
	}

	st, err := w.Svc.ExpireDue(ctx, w.Batch)
	if err != nil {
		// 连候选都查不出来。把租约放开，让下一轮立刻重试，
		// 而不是空等一个租约周期。
		if e := w.Svc.Repo.FinishLease(ctx, model.JobIntroExpire, "err: "+err.Error()); e != nil {
			w.Log.ErrorContext(ctx, "释放租约失败", slog.Any("err", e))
		}
		return err
	}

	result := summarizeExpire(st)
	if e := w.Svc.Repo.FinishLease(ctx, model.JobIntroExpire, result); e != nil {
		w.Log.ErrorContext(ctx, "记录扫描结果失败", slog.Any("err", e))
	}

	// 一条都没扫到是常态：候选为空说明没有到期的信。
	// 那种轮次不值得占用一行日志 —— 一分钟一行，一天就是 1440 行噪音。
	if st.Scanned > 0 {
		w.Log.InfoContext(ctx, "超时扫描完成",
			slog.Int("scanned", st.Scanned),
			slog.Int("expired", st.Expired),
			slog.Int("notified", st.Notified),
			slog.Int("raced", st.Raced),
			slog.Int("failed", st.Failed))
	}
	return nil
}

// summarizeExpire 把一轮结果压成一行存进 job_runs.last_result。
//
// 存字符串不存 JSON：这一列是给人看一眼「上次跑得怎么样」的，
// 不是给程序读的。有 failed 才写进去，没有就只报数字 ——
// 这一列在 job_runs 里是唯一能事后回答「昨天有没有正常终结」的东西。
func summarizeExpire(st service.ExpireStats) string {
	s := fmt.Sprintf("scanned=%d expired=%d notified=%d", st.Scanned, st.Expired, st.Notified)
	if st.Raced > 0 {
		s += fmt.Sprintf(" raced=%d", st.Raced)
	}
	if st.Failed > 0 {
		s += fmt.Sprintf(" FAILED=%d", st.Failed)
	}
	return s
}

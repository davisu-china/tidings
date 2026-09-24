// Package worker 是三个常驻循环的宿主（§15）。
//
// 不引 cron 库、不加消息队列：三个 for + time.Ticker 就够，
// 而少一个组件就少一处运维面。进程内状态一概不留 ——
// 所有进度都在数据库里（outbox 行、job_runs 租约），
// 所以重启 worker 不会丢任何正在进行的工作。
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/push"
	"github.com/davisu-china/tidings/backend/internal/repo"
)

// OutboxWorker 投递通知（§14.2）。
type OutboxWorker struct {
	Repo   *repo.Repo
	Sender *push.Sender
	Log    *slog.Logger

	// Interval 是循环周期（2 秒）。
	Interval time.Duration
	// Lease 是领取一批之后的独占时长。必须是分钟级而不是秒级：
	// 真正的推送是在事务外做的网络调用，慢的时候要几秒到几十秒。
	Lease time.Duration
	// Batch 是每轮最多领几条。
	Batch int

	// Loc 是静默时段与日界判断用的时区，来自配置里的 TIMEZONE。
	//
	// 必须显式注入，不能用进程本地时区：容器里默认是 UTC，那样算出来的
	// 22:00–09:00 落在国内的白天 —— 通知整个白天不发、半夜发，
	// 而日志里一切正常，没有任何东西会报错。
	Loc *time.Location
}

// now 取当前时刻。Loc 没注入时退回进程本地时区，那只是给测试用的退路 ——
// 生产路径在 cmd/worker/main.go 里注入了配置校验过的时区。
func (w *OutboxWorker) now() time.Time {
	if w.Loc == nil {
		return time.Now()
	}
	return time.Now().In(w.Loc)
}

// 重试退避：1 分钟、5 分钟、30 分钟，超过 3 次置 failed。
//
// 是「按尝试次数取下标」而不是「按时间算」，所以顺序就是这三档；
// 第 3 次之后 MarkOutboxRetry 直接把状态置成 failed，不再看这个表。
var outboxBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

const outboxMaxAttempts = 3

// 订阅连续失败到这个次数就停用（§14.3）。
const pushMaxFails = 5

func (w *OutboxWorker) Run(ctx context.Context) error {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.Log.InfoContext(ctx, "outbox worker 启动", slog.Duration("interval", w.Interval))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 单轮失败不退出：数据库抖一下就让 worker 死掉，
				// 等于把所有通知压到重启为止
				w.Log.ErrorContext(ctx, "outbox 轮次失败", slog.Any("err", err))
			}
		}
	}
}

func (w *OutboxWorker) RunOnce(ctx context.Context) error {
	rows, err := w.Repo.ClaimOutboxBatch(ctx, w.Batch, w.Lease)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.deliver(ctx, row); err != nil {
			w.Log.ErrorContext(ctx, "投递失败",
				slog.Int64("outbox_id", row.ID),
				slog.String("template", row.Template),
				slog.Any("err", err))
		}
	}
	return nil
}

// deliver 处理一条通知。
//
// 顺序是：先过三道闸（可能要推迟或丢弃），再查订阅，最后才发。
// 把闸门放在最前面是必要的 —— 静默时段里连「这个人有没有订阅」
// 都不该查，那是白费一次数据库往返。
func (w *OutboxWorker) deliver(ctx context.Context, row repo.OutboxRow) error {
	now := w.now()

	settings, err := w.Repo.GetUserSettings(ctx, row.UserID)
	if err != nil {
		return w.retry(ctx, row, err)
	}

	switch w.gate(settings, row.Template, now) {
	case gateDrop:
		// 有意不发：用户开了暂停或推送已冻结，站内角标会告诉他。
		// 记成 failed 而不是留着 pending —— 留着会被反复领取，
		// 把每轮 50 条的配额吃光，而它永远不会变得可发。
		// last_error 用 skipped: 前缀，好把这种情况和真正的投递失败分开。
		return w.Repo.MarkOutboxSkipped(ctx, row.ID, "skipped:"+dropReason(settings))
	case gateDefer:
		until := quietEndsAt(settings, now)
		return w.Repo.DeferOutbox(ctx, row.ID, until)
	}

	subs, err := w.Repo.LivePushSubs(ctx, row.UserID)
	if err != nil {
		return w.retry(ctx, row, err)
	}
	if len(subs) == 0 {
		// 没有订阅：这个人没授权过推送（iOS 上多半是没装到主屏幕）。
		// 不是错误，也不是失败重试的理由 —— 永远不会有订阅凭空出现。
		return w.Repo.MarkOutboxSkipped(ctx, row.ID, "skipped:no_subscription")
	}

	payload := buildPushPayload(row)
	var lastErr error
	for _, sub := range subs {
		err := w.Sender.Send(ctx, push.Subscription{
			Endpoint: sub.Endpoint,
			P256DH:   sub.P256DH,
			Auth:     sub.Auth,
		}, payload)
		switch {
		case err == nil:
			// 这一条成功。继续下一条订阅 —— 手机和电脑都要收到。
		case errors.Is(err, push.ErrGone):
			// 404/410：endpoint 永久失效，直接停用，不重试
			if e := w.Repo.DisablePushSub(ctx, sub.ID, err.Error()); e != nil {
				w.Log.ErrorContext(ctx, "停用订阅失败", slog.Int64("sub_id", sub.ID), slog.Any("err", e))
			}
		default:
			lastErr = err
			if e := w.Repo.FailPushSub(ctx, sub.ID, err.Error(), pushMaxFails); e != nil {
				w.Log.ErrorContext(ctx, "记录推送失败失败", slog.Int64("sub_id", sub.ID), slog.Any("err", e))
			}
		}
	}

	// 只要有一条订阅送达就算这条通知送达了。全部失败才算失败 ——
	// 否则一个人手机收到、电脑失败，会被判成整条失败再发一次。
	if lastErr != nil {
		return w.retry(ctx, row, lastErr)
	}
	return w.Repo.MarkOutboxSent(ctx, row.ID)
}

func (w *OutboxWorker) retry(ctx context.Context, row repo.OutboxRow, cause error) error {
	attempt := int(row.Attempts) // 本次之前的失败次数
	if attempt >= len(outboxBackoff) {
		attempt = len(outboxBackoff) - 1
	}
	err := w.Repo.MarkOutboxRetry(ctx, row.ID, cause.Error(), outboxBackoff[attempt], outboxMaxAttempts)
	if err != nil {
		return err
	}
	w.Log.WarnContext(ctx, "通知投递失败，已重排",
		slog.Int64("outbox_id", row.ID),
		slog.Int("attempts", int(row.Attempts)+1),
		slog.Any("err", cause))
	return nil
}

// ------------------------------------------------------------ 三道闸

type gateDecision int

const (
	gateDeliver gateDecision = iota
	gateDefer
	gateDrop
)

// gate 是 §14.2 的投递前检查。
//
// 收尾通知（intro_missed / intro_closed）不受「未响应冻结」和
// 「暂停接收引荐」限制 —— 决策 17：它恰恰要发给不常来的人，
// 而它只报一次、不做累计，所以穿透闸门的代价是有界的。
// 但它仍然受静默时段限制：半夜震一下手机的投诉，比晚一天知道
// 「你错过了谁」严重得多。
func (w *OutboxWorker) gate(s *repo.UserSettingsRow, tpl string, now time.Time) gateDecision {
	isClosing := model.IsClosingTpl(tpl)

	if inQuietHours(s, now) {
		return gateDefer
	}
	if !isClosing && s.PushFrozen {
		return gateDrop
	}
	if !isClosing && s.IntrosPaused {
		return gateDrop
	}
	return gateDeliver
}

// dropReason 给「有意不发」留一个可查的原因，用于区分两种丢法。
//
// 收尾通知不会走到这里 —— gate 里两个 drop 分支都带 !isClosing。
func dropReason(s *repo.UserSettingsRow) string {
	if s.PushFrozen {
		return "push_frozen"
	}
	return "intros_paused"
}

// inQuietHours 判当前时刻是否落在静默时段内。
// 区间跨零点是常态（22 → 9），所以不能简单比大小。
//
// now 必须已经带上配置的时区（调用处传的是 w.now()）：
// 静默时段是「用户那边的 22 点」，拿 UTC 的小时数去比毫无意义。
func inQuietHours(s *repo.UserSettingsRow, now time.Time) bool {
	start, end := int(s.QuietStart), int(s.QuietEnd)
	if start == end {
		return false // 起止相同视为不设静默
	}
	h := now.Hour()
	if start < end {
		return h >= start && h < end
	}
	return h >= start || h < end
}

// quietEndsAt 算静默时段结束的时刻，用于把它重新排到那之后。
//
// 落在静默时段里时，结束时刻一定在今天或明天的 end 点 ——
// 取「下一个 end 点」而不是「今天/明天」的分支判断，
// 跨零点与不跨零点两种情况就都不用写了。
//
// 时区跟着传进来的 now 走（now.Location()），所以上面那句「今天还是
// 明天」是在用户那边的日历上数的，不是在服务器上。
func quietEndsAt(s *repo.UserSettingsRow, now time.Time) time.Time {
	end := int(s.QuietEnd)
	at := time.Date(now.Year(), now.Month(), now.Day(), end, 0, 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// ------------------------------------------------------------ 文案

// buildPushPayload 把一条 outbox 行翻成推送内容。
//
// 正文里不带对方的昵称、照片或任何身份信息：推送体要经由推送服务商
// 中转，而它在站外。真正的内容在站内，点进来才加载 ——
// 这也是 payload 能在投递成功后直接清空的原因（§14.2）。
func buildPushPayload(row repo.OutboxRow) push.Payload {
	var p struct {
		BatchID string `json:"batch_id"`
		Kind    string `json:"kind"`
		IntroID int64  `json:"intro_id"`
		MatchID int64  `json:"match_id"`
	}
	// 解析失败不该拦住投递：文案是通用的，payload 只是锦上添花
	_ = json.Unmarshal(row.Payload, &p)

	title, body := copyFor(row.Template)

	// 点进来落在哪一页。
	//
	// 成匹配的通知直接落进会话：这一条的全部意义就是「去说话」，
	// 让人先落在引荐列表上再自己找那条会话是白费一步 ——
	// 而这一步往往就是他不再回来的原因。
	//
	// 其余模板落引荐列表：收尾通知点进去没有可做的事，
	// 而列表页上有最新的一封。
	url := "/"
	if row.Template == model.TplMatched && p.MatchID != 0 {
		url = "/chat/" + strconv.FormatInt(p.MatchID, 10)
	}

	// 同一批的几条共用一个 tag，浏览器会让它们互相覆盖 ——
	// 库里是每条一行（幂等要按条算），用户那里只看到一条
	// （打扰要按批算）。
	//
	// matched / closed 不带 batch_id：它们不是一批里的某一条。
	// 没有 batch_id 就得按引荐拼一个，否则这些通知会一起挤在
	// 「tidings:」这一个 tag 上互相覆盖 —— 同一个人连着收到两条
	// 收尾通知，只会看见后一条。
	tag := "tidings:" + p.BatchID
	if p.BatchID == "" {
		tag = "tidings:intro:" + strconv.FormatInt(p.IntroID, 10)
	}

	return push.Payload{
		Title: title,
		Body:  body,
		URL:   url,
		Tag:   tag,
	}
}

func copyFor(tpl string) (title, body string) {
	switch tpl {
	case model.TplIntroDelivered:
		return "有信", "有新的引荐等你打开"
	case model.TplIntroMissed:
		// §4.8：只报这一次，不做累计，不写「你已错过 N 位」
		return "有信", "你错过了 1 位合适的人"
	case model.TplIntroClosed:
		return "有信", "上封信没有等到回音，这位已经回到池子里了"
	case model.TplMatched:
		return "有信", "你们都点了「想认识」"
	default:
		return "有信", "去看看"
	}
}

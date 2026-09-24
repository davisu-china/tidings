package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// outbox 的写入与投递。表的语义见迁移文件：dedup_key 唯一，
// payload 在投递成功后清空。

// OutboxInsert 是入队一行通知所需的字段。
type OutboxInsert struct {
	Channel  string
	UserID   int64
	Template string
	Payload  json.RawMessage
	DedupKey string
}

// EnqueueOutbox 写一行 pending。跑在调用方的事务里。
//
// 撞 dedup_key 时静默跳过而不是报错：那说明同一个人对同一件事的通知
// 已经在队列里了，这是一次重放，不是故障。让调用方为此处理一个错误，
// 只会诱使它在不该回滚的时候回滚。
func (r *Repo) EnqueueOutbox(tx *gorm.DB, in OutboxInsert) error {
	return tx.Exec(`
		INSERT INTO outbox (channel, user_id, template, payload, dedup_key)
		VALUES (?, ?, ?, ?::jsonb, ?)
		ON CONFLICT (dedup_key) DO NOTHING`,
		in.Channel, in.UserID, in.Template, string(in.Payload), in.DedupKey,
	).Error
}

// ClaimOutboxBatch 领一批待投递的通知（§14.2）。
//
// 两步必须在同一事务里：
//  1. FOR UPDATE SKIP LOCKED 挑出待投的，锁住它们 —— SKIP LOCKED 让
//     多个 worker 各领各的，不会互相等待；
//  2. 把 next_retry_at 推到 now() + lease 当作租约。
//
// 租约是必需的：第 1 步的锁在事务结束时就释放了，而真正的推送是在
// 事务外做的（网络调用不能占着数据库事务）。没有第 2 步，
// 两个 worker 会在锁释放后领到同一批。
func (r *Repo) ClaimOutboxBatch(ctx context.Context, limit int, lease time.Duration) ([]OutboxRow, error) {
	var rows []OutboxRow
	err := r.Tx(func(tx *gorm.DB) error {
		if err := tx.Raw(`
			SELECT id, channel, user_id, template, payload, attempts, dedup_key
			FROM outbox
			WHERE status = 'pending' AND next_retry_at <= now()
			ORDER BY next_retry_at
			LIMIT ?
			FOR UPDATE SKIP LOCKED`, limit).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(rows))
		for _, o := range rows {
			ids = append(ids, o.ID)
		}
		// id IN (?) 而不是 ANY(?)：GORM 把切片摊成多个占位符，
		// 摊开的形式正是 IN 要的，而 ANY 拿到它会直接是语法错误。
		return tx.Exec(
			`UPDATE outbox SET next_retry_at = now() + ?::interval WHERE id IN (?)`,
			lease.String(), ids).Error
	})
	return rows, err
}

// OutboxRow 是投递时要用的字段。payload 不进 struct 的 JSON 形态，
// 因为它在成功投递后会被清空，读的时候也只需要透传。
type OutboxRow struct {
	ID       int64  `gorm:"column:id"`
	Channel  string `gorm:"column:channel"`
	UserID   int64  `gorm:"column:user_id"`
	Template string `gorm:"column:template"`
	Payload  []byte `gorm:"column:payload"`
	Attempts int16  `gorm:"column:attempts"`
	DedupKey string `gorm:"column:dedup_key"`
}

// MarkOutboxSent 标记投递成功。
//
// payload 清空为 '{}'：推送内容不该长期留在库里 —— 它含有另一个人的
// 昵称与照片信息，留着只是白白扩大泄露面。诊断「他到底收到没有」
// 靠 template + sent_at 就够了，不需要正文。
func (r *Repo) MarkOutboxSent(ctx context.Context, id int64) error {
	return r.DB.WithContext(ctx).Exec(
		`UPDATE outbox SET status = 'sent', sent_at = now(), payload = '{}'::jsonb, last_error = ''
		 WHERE id = ?`, id).Error
}

// MarkOutboxSkipped 把一条通知标记为「有意不投递」并就此终结。
//
// 状态用的是 failed，因为表上只有 pending / sent / failed 三个取值。
// 语义上它确实不是「投递失败」，而是「决定不投」—— 靠 last_error 的
// skipped: 前缀区分，统计失败率时按前缀过滤掉。
//
// 不能留着 pending：它会被每一轮反复领取，把每轮 50 条的配额吃光，
// 而它在用户关掉暂停或解冻之前永远不会变得可发。
func (r *Repo) MarkOutboxSkipped(ctx context.Context, id int64, reason string) error {
	return r.DB.WithContext(ctx).Exec(
		`UPDATE outbox SET status = 'failed', last_error = ? WHERE id = ?`,
		reason, id).Error
}

// MarkOutboxRetry 记一次失败并按退避重排。
//
// 超过 maxAttempts 置 failed，不再重试：一条永远发不出去的通知
// 留在 pending 里会被反复领取，把每轮的 50 条配额吃光。
func (r *Repo) MarkOutboxRetry(ctx context.Context, id int64, errMsg string, backoff time.Duration, maxAttempts int16) error {
	return r.DB.WithContext(ctx).Exec(`
		UPDATE outbox
		SET attempts   = attempts + 1,
		    last_error = ?,
		    status     = CASE WHEN attempts + 1 >= ? THEN 'failed' ELSE 'pending' END,
		    next_retry_at = now() + ?::interval
		WHERE id = ?`, errMsg, maxAttempts, backoff.String(), id).Error
}

// DeferOutbox 把一条通知推迟到某个时刻，不消耗重试次数（§14.2 的静默时段）。
//
// 与 MarkOutboxRetry 分开是必要的：静默时段是「现在不该吵醒他」，
// 不是「投递失败」。混在一起会让一条通知在静默一夜之后就被判定
// 失败 —— 而那正是收尾通知最需要送达的时刻。
func (r *Repo) DeferOutbox(ctx context.Context, id int64, until time.Time) error {
	return r.DB.WithContext(ctx).Exec(
		`UPDATE outbox SET next_retry_at = ? WHERE id = ?`, until, id).Error
}

// ------------------------------------------------------------ 推送订阅

// PushSub 是投递一条推送所需的订阅信息。
type PushSub struct {
	ID       int64  `gorm:"column:id"`
	Endpoint string `gorm:"column:endpoint"`
	P256DH   string `gorm:"column:p256dh"`
	Auth     string `gorm:"column:auth"`
}

// LivePushSubs 取一个人所有可用的订阅。
//
// 一个人可能有多条（手机 + 电脑），全部要发 —— 只发最新的一条
// 会让换了设备的用户在旧设备上再也收不到。endpoint 全局唯一，
// 所以不存在同一浏览器重复发的可能。
func (r *Repo) LivePushSubs(ctx context.Context, uid int64) ([]PushSub, error) {
	var rows []PushSub
	err := r.DB.WithContext(ctx).Raw(`
		SELECT id, endpoint, p256dh, auth
		FROM push_subscriptions
		WHERE user_id = ? AND disabled_at IS NULL
		ORDER BY id`, uid).Scan(&rows).Error
	return rows, err
}

// DisablePushSub 永久停用一条订阅（§14.3）。
// 404 / 410 表示 endpoint 已经失效，重试没有意义。
func (r *Repo) DisablePushSub(ctx context.Context, id int64, reason string) error {
	return r.DB.WithContext(ctx).Exec(
		`UPDATE push_subscriptions SET disabled_at = now(), last_error = ? WHERE id = ?`,
		reason, id).Error
}

// FailPushSub 记一次可重试的失败，连续失败到上限就停用。
//
// 与 DisablePushSub 分开：4xx/5xx 可能只是对端抖了一下，
// 直接停用会让用户在网络恢复后再也收不到推送，而他毫不知情。
func (r *Repo) FailPushSub(ctx context.Context, id int64, reason string, maxFails int16) error {
	return r.DB.WithContext(ctx).Exec(`
		UPDATE push_subscriptions
		SET fail_count = fail_count + 1,
		    last_error = ?,
		    disabled_at = CASE WHEN fail_count + 1 >= ? THEN now() ELSE disabled_at END
		WHERE id = ?`, reason, maxFails, id).Error
}

// UpsertPushSub 注册一条订阅。
//
// endpoint 唯一 → 这天然是一次 upsert。同一个浏览器换账号登录时，
// 旧订阅会归到新用户名下 —— 这是唯一键带来的正确行为，不是副作用：
// 那台设备确实已经属于新账号了。
//
// 重新注册会清掉 fail_count 和 disabled_at：用户主动授权了一次，
// 说明这个 endpoint 现在是活的，之前累积的失败记录不该继续压着它。
func (r *Repo) UpsertPushSub(ctx context.Context, uid int64, endpoint, p256dh, auth, ua string) error {
	return r.DB.WithContext(ctx).Exec(`
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET
		    user_id     = EXCLUDED.user_id,
		    p256dh      = EXCLUDED.p256dh,
		    auth        = EXCLUDED.auth,
		    user_agent  = EXCLUDED.user_agent,
		    fail_count  = 0,
		    last_error  = '',
		    disabled_at = NULL`,
		uid, endpoint, p256dh, auth, ua).Error
}

// DeletePushSub 退订。只删自己的 —— 带上 user_id 是为了让
// 拿着别人 endpoint 的请求删不掉别人的订阅。
func (r *Repo) DeletePushSub(ctx context.Context, uid int64, endpoint string) error {
	return r.DB.WithContext(ctx).Exec(
		`DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`,
		uid, endpoint).Error
}

// GetUserSettings 读防打扰三道闸的持久化部分。
// 没有行时返回默认值（22:00–09:00，不暂停，未冻结）。
func (r *Repo) GetUserSettings(ctx context.Context, uid int64) (*UserSettingsRow, error) {
	var rows []UserSettingsRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT user_id, intros_paused, paused_at, quiet_start, quiet_end,
		       push_frozen, unopened_streak
		FROM user_settings WHERE user_id = ?`, uid).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		if err != nil {
			return nil, err
		}
		// 没建过设置行 = 全默认
		return &UserSettingsRow{UserID: uid, QuietStart: 22, QuietEnd: 9}, nil
	}
	return &rows[0], nil
}

// UserSettingsRow 与 user_settings 表对应。
type UserSettingsRow struct {
	UserID int64 `gorm:"column:user_id"`
	// IntrosPaused 用户主动暂停接收引荐。暂停期间他仍会被别人看到 ——
	// 所以一条引荐可能在生成时对方正在暂停，那时计时冻结（§13.2）。
	IntrosPaused   bool       `gorm:"column:intros_paused"`
	PauseStartedAt *time.Time `gorm:"column:paused_at"`
	QuietStart     int16      `gorm:"column:quiet_start"`
	QuietEnd       int16      `gorm:"column:quiet_end"`
	PushFrozen     bool       `gorm:"column:push_frozen"`
	UnopenedStreak int16      `gorm:"column:unopened_streak"`
}

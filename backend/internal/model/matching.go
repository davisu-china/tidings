// M2 的表：偏好、引荐、推送订阅、outbox、用户设置、任务租约。
//
// 与 models.go 一样：schema 的唯一事实来源是 migrations/000001_init.up.sql，
// 这里的 struct 只是映射。改字段先改 SQL，不要用 AutoMigrate。
package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------- 偏好

// Preference 是匹配用的双层偏好。两层的行为完全不同，别混：
// 硬条件一票否决（不合就淘汰，不打分），软偏好只参与打分（不合也照推，只是分数低）。
//
// 全是 NULL 表示「不限」。这一条要在三处保持一致：存储为空、
// 打分时该维度不计入分母、界面上显示「不限」而不是空白。
// 任何一处漏掉，用户都会以为自己的偏好被无视了。
type Preference struct {
	UserID int64 `gorm:"primaryKey"`

	// 硬条件 5 项
	BirthYMMin     *int
	BirthYMMax     *int
	CityCodes      []int64 `gorm:"type:integer[];serializer:array"`
	WantChild      *int16
	AcceptDivorced *int16
	AcceptRemote   *int16

	// 软偏好 3 项（收入是区间，算一组）
	EduMin    *int16
	HeightMin *int16
	HeightMax *int16
	IncomeMin *int16
	IncomeMax *int16

	UpdatedAt time.Time
}

func (Preference) TableName() string { return "preferences" }

// ---------------------------------------------------------------- 引荐

// 引荐类型。oneway 表示只有一方收到，hidden_side 那一方从不知道它存在。
const (
	IntroPaired = "paired"
	IntroOneway = "oneway"
)

// 引荐状态。只前进不后退，所以 expires_at 在每次跃迁时重算。
const (
	IntroPending   = "pending"
	IntroViewed    = "viewed"
	IntroResponded = "responded"
	IntroMatched   = "matched"
	IntroDeclined  = "declined"
	IntroExpired   = "expired"
)

// IntroOpenStates 是「未终结」的三个状态。这个集合出现在三个地方，
// 必须一致：uniq_intro_open_pair 的部分索引、超时扫描的 idx_intro_expire、
// 以及候选集里排重用的 NOT EXISTS。改这里就要连 SQL 一起改。
var IntroOpenStates = []string{IntroPending, IntroViewed, IntroResponded}

const (
	SideLow  = "low"
	SideHigh = "high"
)

// 表态动作。
const (
	ActionLike = "like"
	ActionPass = "pass"
)

// 「不合适」的原因（决策 07：三选一必填，不要求输入文字）。
//
// 存 code 不存中文：文案还会改（「感觉不合适」也许某天要换个说法），
// 而这一列是要拿来统计的 —— §1.1 的「表态里不合适的占比」按原因拆。
// 中文文案在前端字典里，和 education_level 那批枚举一样。
const (
	ReasonMismatch = "mismatch" // 条件不符
	ReasonVibe     = "vibe"     // 感觉不合适
	ReasonOther    = "other"    // 其他
)

// ValidReason 判一个原因是否为三个取值之一。
func ValidReason(r string) bool {
	switch r {
	case ReasonMismatch, ReasonVibe, ReasonOther:
		return true
	}
	return false
}

// IsOpenState 判一条引荐是否还没终结。
// 与 IntroOpenStates 是同一件事的两种表达 —— 那个给 SQL 用，这个给 Go 用。
func IsOpenState(state string) bool {
	for _, s := range IntroOpenStates {
		if s == state {
			return true
		}
	}
	return false
}

// Introduction 一条引荐。
//
// 一对用户只占一行，用 user_low < user_high 归一 —— 与 matches 同一手法。
// 这样「双方同时表态」只需要锁一行，不用锁两行再防死锁。
type Introduction struct {
	ID      int64  `gorm:"primaryKey"`
	BatchID string `gorm:"type:uuid"`

	UserLow  int64 `gorm:"not null"`
	UserHigh int64 `gorm:"not null"`

	Kind  string `gorm:"not null"`
	State string `gorm:"not null;default:pending"`
	// HiddenSide 非空时，那一方从未收到过这条引荐，也永远不参与终结通知。
	HiddenSide *string

	LowViewedAt  *time.Time
	HighViewedAt *time.Time
	LowAction    *string
	HighAction   *string
	LowActionAt  *time.Time
	HighActionAt *time.Time
	LowReason    *string
	HighReason   *string

	// 打分留痕，供后续调参与模型训练。
	ScoreLowToHigh *float64
	ScoreHighToLow *float64

	CreatedAt time.Time
	ExpiresAt time.Time
	ClosedAt  *time.Time
}

func (Introduction) TableName() string { return "introductions" }

// Other 返回对面那方的 id。
func (i *Introduction) Other(uid int64) int64 {
	if uid == i.UserLow {
		return i.UserHigh
	}
	return i.UserLow
}

// SideOf 返回 uid 在这条引荐里是 low 还是 high。不是当事人则返回空串。
func (i *Introduction) SideOf(uid int64) string {
	switch uid {
	case i.UserLow:
		return SideLow
	case i.UserHigh:
		return SideHigh
	}
	return ""
}

// VisibleTo 判断 uid 是否应该看到这条引荐。单向引荐的隐藏方返回 false ——
// 它不知道这条引荐存在，所有查询与接口都必须先过这一关。
func (i *Introduction) VisibleTo(uid int64) bool {
	if i.HiddenSide != nil && *i.HiddenSide == i.SideOf(uid) {
		return false
	}
	return i.SideOf(uid) != ""
}

// ---------------------------------------------------------------- 匹配

const (
	MatchActive  = "active"
	MatchBlocked = "blocked"
	MatchClosed  = "closed"
)

type Match struct {
	ID        int64 `gorm:"primaryKey"`
	UserLow   int64 `gorm:"not null"`
	UserHigh  int64 `gorm:"not null"`
	Status    string
	CreatedAt time.Time
	LastMsgAt *time.Time
}

func (Match) TableName() string { return "matches" }

// Other 返回对面那方的 id。与 Introduction.Other 同一手法。
func (m *Match) Other(uid int64) int64 {
	if uid == m.UserLow {
		return m.UserHigh
	}
	return m.UserLow
}

// Has 判断 uid 是不是这条匹配的当事人。会话接口全靠它挡人 ——
// 不是当事人就该拿到 404 而不是这个人的聊天记录。
func (m *Match) Has(uid int64) bool {
	return uid == m.UserLow || uid == m.UserHigh
}

// ---------------------------------------------------------------- 会话

// Message 一条站内消息（§4.7：纯文字，1–1000 字）。
type Message struct {
	ID      int64 `gorm:"primaryKey"`
	MatchID int64 `gorm:"not null"`
	// SenderID 只有两个可能取值，取自这条匹配的 user_low / user_high。
	SenderID int64 `gorm:"not null"`
	Content  string
	// ClientMsgID 由客户端生成，(match_id, client_msg_id) 唯一 ——
	// 重发同一条不会产生第二条消息，这是发送幂等的全部实现。
	ClientMsgID string
	CreatedAt   time.Time
}

func (Message) TableName() string { return "messages" }

// MatchRead 是已读水位。没有行 = 从来没读过。
//
// 用「最后读到的消息 id」而不是「读过的条数」：条数会被并发和重发搞乱，
// 而 id 是单调的，新消息一定比它大。
type MatchRead struct {
	MatchID int64 `gorm:"primaryKey"`
	UserID  int64 `gorm:"primaryKey"`
	// LastReadMsgID 默认 0，语义是「一条都没读过」。
	LastReadMsgID int64
	UpdatedAt     time.Time
}

func (MatchRead) TableName() string { return "match_reads" }

// ---------------------------------------------------------------- 推送

type PushSubscription struct {
	ID        int64  `gorm:"primaryKey"`
	UserID    int64  `gorm:"not null"`
	Endpoint  string `gorm:"not null"`
	P256DH    string `gorm:"column:p256dh;not null"`
	Auth      string `gorm:"not null"`
	UserAgent string `gorm:"not null;default:''"`
	FailCount int16  `gorm:"not null;default:0"`
	LastError string `gorm:"not null;default:''"`
	// DisabledAt 是软禁用而非删除：留着行才能统计「多少用户的通知实际上是坏的」。
	DisabledAt *time.Time
	CreatedAt  time.Time
}

func (PushSubscription) TableName() string { return "push_subscriptions" }

// --------------------------------------------------------------- outbox

const (
	ChannelPush = "push"
	// ChannelEmail 留到 V1.1。表结构已经留好这个取值，补通道时不用改表。
	ChannelEmail = "email"
)

// 通知模板。前两个是「有新引荐」，后两个是收尾通知。
//
// 收尾通知（missed / closed）不受「未响应冻结」和「暂停接收」限制 ——
// 决策 17：它恰恰要发给不常来的人。但它仍然受静默时段限制。
const (
	TplIntroDelivered = "intro_delivered"
	TplIntroMissed    = "intro_missed"
	TplIntroClosed    = "intro_closed"
	TplMatched        = "matched"
)

const (
	OutboxPending = "pending"
	OutboxSent    = "sent"
	OutboxFailed  = "failed"
)

// IsClosingTpl 判断是不是收尾通知。投递前的三道闸要按这个分叉。
func IsClosingTpl(tpl string) bool {
	return tpl == TplIntroMissed || tpl == TplIntroClosed
}

// Outbox 所有外发通知的唯一出口。
//
// DedupKey 是幂等的全部实现：'intro:123:low:delivered' 保证同一条引荐
// 给同一个人的「已递出」通知只会存在一行 —— 重试、并发、worker 重启
// 都不会让人收到重复推送。
type Outbox struct {
	ID        int64  `gorm:"primaryKey"`
	Channel   string `gorm:"not null"`
	UserID    int64  `gorm:"not null"`
	Template  string `gorm:"not null"`
	Payload   JSONB  `gorm:"type:jsonb"`
	DedupKey  string `gorm:"not null"`
	Status    string `gorm:"not null;default:pending"`
	Attempts  int16  `gorm:"not null;default:0"`
	LastError string `gorm:"not null;default:''"`

	NextRetryAt time.Time `gorm:"not null"`
	SentAt      *time.Time
	CreatedAt   time.Time
}

func (Outbox) TableName() string { return "outbox" }

// JSONB 让 payload 能直接扫进 map，不用在每处 Scan 时手工 Unmarshal。
type JSONB map[string]any

func (j *JSONB) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	}
	if len(b) == 0 {
		*j = nil
		return nil
	}
	return json.Unmarshal(b, j)
}

func (j JSONB) Value() (any, error) {
	if j == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(j)
}

// ---------------------------------------------------------- 用户设置

// UserSettings 是防打扰三道闸的持久化部分。
//
// 这三列必须在 PG 里而不是 Redis：「连续 2 次未打开」是跨天的语义，
// 放 Redis 会因为过期丢失计数，用户永远等不到冻结。
type UserSettings struct {
	UserID int64 `gorm:"primaryKey"`

	// IntrosPaused 是用户主动暂停接收引荐。注意：暂停期间他仍会被别人看到，
	// 所以一条引荐可能在生成时对方正在暂停 —— 那时计时冻结（见 §13.2）。
	IntrosPaused bool
	PausedAt     *time.Time

	QuietStart int16 `gorm:"not null;default:22"`
	QuietEnd   int16 `gorm:"not null;default:9"`

	PushFrozen     bool
	UnopenedStreak int16 `gorm:"not null;default:0"`

	UpdatedAt time.Time
}

func (UserSettings) TableName() string { return "user_settings" }

// ------------------------------------------------------------- 任务租约

const (
	JobIntroGenerate = "intro_generate"
	JobIntroExpire   = "intro_expire"
)

// JobRun 是定时任务的租约行。抢租约是一条条件 UPDATE（leased_until < now()），
// 返回行数 1 表示抢到。租约只防重复劳动，不保证正确性 ——
// 正确性来自 uniq_intro_open_pair。
type JobRun struct {
	Name        string `gorm:"primaryKey"`
	LeasedUntil time.Time
	LastRunAt   *time.Time
	LastResult  string
}

func (JobRun) TableName() string { return "job_runs" }

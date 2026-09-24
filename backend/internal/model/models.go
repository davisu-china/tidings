// Package model 定义 GORM 模型，与 migrations/000001_init.up.sql 一一对应。
//
// 注意：schema 的唯一事实来源是 migrations 下的 SQL 文件，不是这些 struct。
// 改字段先改 SQL 迁移，再同步这里。不要用 AutoMigrate。
//
// 本文件只收录已经有代码在用的表。引荐、会话、举报等表在 M2–M5
// 各自落地时再补，避免造出一堆没人读的字段映射。
package model

import (
	"encoding/json"
	"time"
)

// 用户状态。运营在后台扭转 status 即可让违规用户立刻从所有候选集消失。
const (
	StatusOnboarding  = "onboarding"
	StatusActive      = "active"
	StatusUnderReview = "under_review"
	StatusBanned      = "banned"
	StatusDeactivated = "deactivated"
)

const (
	GenderMale   = "M"
	GenderFemale = "F"
)

// 硬条件三项的取值。命名用「自己的情况」这一侧，
// preferences 侧的同名列（我要求对方怎样）在 service 里单独判。
const (
	WantChildYes = 1 // 想要孩子
	WantChildNo  = 2 // 不要孩子
	WantChildTBD = 3 // 再说

	MaritalSingle   = 1 // 未婚
	MaritalDivorced = 2 // 离异
)

// 异地接受度的取值。这一项只存在于 preferences 侧 ——
// 它是「我对关系形态的要求」，不是「关于我的事实」，见 000002 迁移的注释。
const (
	RemoteAccept    = 1 // 接受异地
	RemoteNotAccept = 2 // 不接受异地
)

// 头像与资料的审核状态。注意它们与 photos.review_state 不是一套：
// 这里的 pending 只表示「占用了人工巡检的名额」，不阻塞任何功能，
// 因为人工队列要到 M5 才有（见迁移文件里 photos 的注释）。
const (
	ReviewPending  = "pending"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

type User struct {
	ID           int64  `gorm:"primaryKey"`
	Email        string `gorm:"not null"`
	PasswordHash string `gorm:"not null;default:''"`
	Status       string `gorm:"not null;default:onboarding"`
	TokenVersion int    `gorm:"not null;default:0"`
	LastActiveAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (User) TableName() string { return "users" }

// Profile 与 User 一比一。必填字段在库里可空，
// 完整性由 service 层在 status 翻成 active 前校验（见迁移文件顶部注释）。
type Profile struct {
	UserID int64 `gorm:"primaryKey"`

	// 必填 8 项
	Nickname       *string
	Gender         *string // M / F，设定后不可改；匹配一律异性，故无 seeking 字段
	BirthYM        *int    `gorm:"column:birth_ym"` // 如 199505
	CityCode       *int
	HeightCM       *int16 `gorm:"column:height_cm"`
	WeightKG       *int16 `gorm:"column:weight_kg"`
	EducationLevel *int16
	SchoolName     string `gorm:"not null;default:''"`

	// BirthDay 是出生日（1–31），000003 迁移补的。NULL = 只知道年月。
	//
	// 它与别的必填项不一样：**不进 requiredMissing、不进完整度、不进
	// 引荐对象那侧的下发**。年龄始终按月算（见 ageFromBirthYM），日对
	// 匹配毫无用处；而 completeness 决定能不能进候选集，动它会让老用户
	// 静默掉出池子。它只用来在自己的资料页上把生日显示完整。
	BirthDay *int16 `gorm:"column:birth_day"`

	// 选填 10 项
	HometownCode *int
	// SchoolTier 仅由服务端按院校库归一，不接受前端传值（M2 接 schools 包）
	SchoolTier  *int16
	Occupation  string `gorm:"not null;default:''"`
	Company     string `gorm:"not null;default:''"`
	IncomeBand  *int16
	Chronotype  *int16 // 1 早睡早起 / 2 夜猫子 / 3 不规律
	Smoking     *int16 // 0 不 / 1 偶尔 / 2 经常
	Drinking    *int16
	Hobbies     string `gorm:"not null;default:''"` // 顿号分隔，≤6 个
	Intro       string `gorm:"not null;default:''"`
	Expectation string `gorm:"not null;default:''"`

	// 硬条件两项（000002 迁移补的）。它们是「我自己是什么情况」，
	// 与 preferences 侧的同名/对应列（「我要求对方怎样」）是两回事，
	// 过滤时要把两边对着比。NULL = 未填，按「不限」处理。
	//
	// 这里没有 accept_remote：异地接受度是对关系形态的要求，
	// 已经存在 preferences.accept_remote，不重复存。
	WantChild     *int16
	MaritalStatus *int16 // 1 未婚 / 2 离异

	AvatarKey string `gorm:"not null;default:''"`
	// Completeness 0..100。不参与匹配打分，只做「可被引荐」的门槛。
	Completeness int16 `gorm:"not null;default:0"`

	// 内容审核：文本签名比对命中才标记待审核；被拒时从快照恢复。
	// M1 只维护状态与快照，审核动作本身在 M5。
	ProfileReviewState string          `gorm:"not null;default:approved"`
	ProfileSnapshot    json.RawMessage `gorm:"column:profile_snapshot;type:jsonb"`
	AvatarReviewState  string          `gorm:"not null;default:approved"`
	AvatarPrevKey      string          `gorm:"not null;default:''"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Profile) TableName() string { return "profiles" }

// Photo 引荐卡上的照片。position = 0 即封面，不另设 is_main。
//
// ReviewState 只记录人工巡检进度，不是展示的前置条件：
// 图片上传即生效，不阻塞登录与匹配。这与入池门槛（≥1 张）是两件事。
type Photo struct {
	ID          int64 `gorm:"primaryKey"`
	UserID      int64 `gorm:"not null;index"`
	ObjectKey   string
	Position    int16
	ReviewState string `gorm:"not null;default:unreviewed"`
	ReviewedAt  *time.Time
	CreatedAt   time.Time
}

func (Photo) TableName() string { return "photos" }

// 照片的人工巡检状态，与上面的审核状态不是一套。
const (
	PhotoUnreviewed = "unreviewed"
	PhotoOK         = "ok"
	PhotoViolation  = "violation"
)

// AdminAction 运营操作留痕。人工审核必须可追溯，否则误封无法复盘。
// M1 只写「重置密码」一种 action。
type AdminAction struct {
	ID           int64  `gorm:"primaryKey"`
	Operator     string `gorm:"not null"`
	TargetUserID int64  `gorm:"not null"`
	Action       string `gorm:"not null"`
	Reason       string `gorm:"not null;default:''"`
	CreatedAt    time.Time
}

func (AdminAction) TableName() string { return "admin_actions" }

// 运营操作类型。M5 会补上 ban / approve / reject。
const (
	ActionResetPassword = "reset_password"
)

// Package apierr 定义带错误码的业务错误。
//
// 错误码是前后端契约的一部分，前端按 Code 分支处理，Message 只用于兜底展示。
// 前端不应解析 Message。
package apierr

import "net/http"

type Error struct {
	HTTPStatus int
	Code       string
	Message    string
	// RetryAfter 为大于 0 时，handler 会写入同名响应头（秒）。
	RetryAfter int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, message string) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: message}
}

// WithRetryAfter 复制一份并带上冷却秒数，避免改到包级的共享实例。
func (e *Error) WithRetryAfter(seconds int) *Error {
	c := *e
	c.RetryAfter = seconds
	return &c
}

// WithMessage 复制一份并替换提示文案。
func (e *Error) WithMessage(msg string) *Error {
	c := *e
	c.Message = msg
	return &c
}

// 通用
var (
	ErrInternal     = New(http.StatusInternalServerError, "INTERNAL", "服务开小差了，请稍后再试")
	ErrBadRequest   = New(http.StatusBadRequest, "BAD_REQUEST", "请求参数有误")
	ErrUnauthorized = New(http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
	ErrForbidden    = New(http.StatusForbidden, "FORBIDDEN", "没有权限")
	// ErrNotFound 同时承担「不存在」与「不属于你」两种语义。
	// 这是刻意的反枚举规则：返回 403 等于确认「这个 id 存在」。
	ErrNotFound    = New(http.StatusNotFound, "NOT_FOUND", "内容不存在")
	ErrRateLimited = New(http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后再试")
)

// 认证（邮箱 + 密码）
var (
	ErrTokenExpired = New(http.StatusUnauthorized, "TOKEN_EXPIRED", "登录已过期，请重新登录")
	ErrInvalidEmail = New(http.StatusBadRequest, "INVALID_EMAIL", "邮箱格式不正确")
	// ErrEmailRejected 是一次性邮箱域名。邮箱不验证之后，这是注册侧
	// 唯一还挡得住批量刷号的东西 —— 见 §4.1 与风险清单。
	ErrEmailRejected = New(http.StatusBadRequest, "EMAIL_REJECTED", "请使用常用邮箱注册")
	ErrWeakPassword  = New(http.StatusBadRequest, "WEAK_PASSWORD", "密码强度不够")
	// ErrPasswordTooLong 单独一条：bcrypt 只取前 72 字节，超出部分被静默
	// 截断。不报错的话，用户以为自己设置了更长的密码，其实没有。
	ErrPasswordTooLong = New(http.StatusBadRequest, "PASSWORD_TOO_LONG", "密码不能超过 72 字节")
	// ErrEmailTaken 是注册接口绕不开的泄露：不告诉用户邮箱被占了，
	// 他就不知道该怎么办。用 IP 限流压住枚举速度，见风险清单。
	ErrEmailTaken = New(http.StatusConflict, "EMAIL_TAKEN", "这个邮箱已经注册过了")
	// ErrInvalidCredentials 对「邮箱不存在」和「密码错误」返回同一句话，
	// 登录接口不泄露某个邮箱是否注册过。
	ErrInvalidCredentials = New(http.StatusUnauthorized, "INVALID_CREDENTIALS", "邮箱或密码不正确")
	// ErrAccountLocked 的文案由 service 层按实际锁定时长替换，
	// 这里的兜底文案不带具体分钟数
	ErrAccountLocked = New(http.StatusTooManyRequests, "ACCOUNT_LOCKED", "密码错误次数过多，请稍后再试")
	ErrAccountBanned = New(http.StatusForbidden, "ACCOUNT_BANNED", "账号已被封禁，如有疑问请联系客服")
)

// 建档与偏好
var (
	ErrProfileIncomplete = New(http.StatusForbidden, "PROFILE_INCOMPLETE", "请先完成建档")
	ErrGenderImmutable   = New(http.StatusBadRequest, "GENDER_IMMUTABLE", "性别设定后不可修改")
	ErrFieldRejected     = New(http.StatusBadRequest, "FIELD_REJECTED", "内容包含不适宜的信息，请修改后重试")
)

// 媒体
var (
	ErrUploadTooLarge = New(http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "图片超出大小限制")
	// 收 JPEG 与 PNG 两种输入，输出永远是 JPEG 三档（见 service/media.go）。
	// 收 PNG 是顺手的事：image/png 是标准库，不引入 cgo，
	// 而截图、微信另存出来的图有相当一部分就是 PNG。
	ErrUploadBadType = New(http.StatusUnsupportedMediaType, "UPLOAD_BAD_TYPE", "只支持 JPEG 和 PNG 图片")
	ErrPhotoLimit    = New(http.StatusBadRequest, "PHOTO_LIMIT", "照片数量已达上限")
)

// 引荐
var (
	ErrIntroNotFound  = New(http.StatusNotFound, "INTRO_NOT_FOUND", "这条引荐不存在")
	ErrIntroClosed    = New(http.StatusConflict, "INTRO_CLOSED", "这条引荐已经结束了")
	ErrIntroResponded = New(http.StatusConflict, "INTRO_RESPONDED", "你已经表过态了")
	// 决策 07：「不合适」的原因必填，三个一键选项。
	ErrReasonRequired = New(http.StatusBadRequest, "REASON_REQUIRED", "请选择不合适的原因")
)

// 匹配与会话
var (
	ErrMatchNotFound = New(http.StatusNotFound, "MATCH_NOT_FOUND", "会话不存在")
	ErrBlocked       = New(http.StatusForbidden, "BLOCKED", "对话已结束")

	// 消息内容（§4.7：纯文字，1–1000 字）。分成两个码而不是一个
	// 「内容不合法」：前端对「空」和「太长」的处理完全不同 ——
	// 前者什么都不该提示（发送按钮本来就该是灰的），后者要在输入框
	// 下面挂一行字数提示。
	ErrMessageEmpty   = New(http.StatusBadRequest, "MESSAGE_EMPTY", "消息不能为空")
	ErrMessageTooLong = New(http.StatusBadRequest, "MESSAGE_TOO_LONG", "消息不能超过 1000 字")

	// 实时通道的入场券。60 秒有效、只能用一次（§18.3）。
	ErrWSTicketInvalid = New(http.StatusUnauthorized, "WS_TICKET_INVALID", "连接凭证无效或已过期")
)

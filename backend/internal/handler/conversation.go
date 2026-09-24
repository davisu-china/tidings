package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

// 引荐的两个写接口与会话接口（§18.3）。

// RespondInput 是一次表态的请求体。
//
// Reason 用 string 而不是三个常量字符串的类型别名：它只在 pass 时
// 有意义，而「有没有填」和「填的是什么」两件事 service 都要判。
type RespondInput struct {
	Action string `json:"action" binding:"required"`
	Reason string `json:"reason"`
}

// GetIntroduction 读一条引荐的详情。
//
// 「打开」的跃迁（§13.1 的 pending → viewed）发生在 service 里，
// 不在这个函数里 —— 放在 handler 里的话，将来多一个调用点
// （比如分享出去的落地页）就会漏掉一次跃迁。
func (h *Handler) GetIntroduction(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		return
	}

	view, err := h.svc.GetIntroduction(c.Request.Context(), user, id)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// RespondIntroduction 回复一条引荐：想认识，或者不合适。
//
// 这是一个不可撤销的动作（§19.3 的盖章），所以它没有对应的
// 「取消」。重复提交同一个动作会幂等成功，见 service.Respond。
func (h *Handler) RespondIntroduction(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		return
	}

	var in RespondInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	out, err := h.svc.Respond(c.Request.Context(), user, id, in.Action, in.Reason)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

// ---------------------------------------------------------------- 会话

// ListMatches 列出本人的会话。
func (h *Handler) ListMatches(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	view, err := h.svc.ListMatches(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// ListMessages 翻一页消息。before 是游标（更早的那一头），
// 不传就是最新的一页（§18.3）。
func (h *Handler) ListMessages(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	matchID, ok := pathID(c, "id")
	if !ok {
		return
	}

	// 游标和 limit 解析失败都当没传：它们只影响翻到哪一页，
	// 为一个可选参数把整个请求打回去是没道理的。
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))

	view, err := h.svc.ListMessages(c.Request.Context(), user, matchID, before, limit)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// SendMessageInput 是发消息的请求体。
//
// ClientMsgID 是必填的：发送幂等全靠它，而幂等不是一个「可选优化」——
// 用户点了发送、请求超时、前端重试，没有它就会发出两条一样的消息，
// 而这两条在数据库里长得一模一样，事后无法分辨哪条是重发。
type SendMessageInput struct {
	ClientMsgID string `json:"client_msg_id" binding:"required"`
	Content     string `json:"content" binding:"required"`
}

// SendMessage 发一条消息。走 HTTP 而不是 WebSocket ——
// 见 internal/ws 顶部关于「只下行」的说明。
func (h *Handler) SendMessage(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	matchID, ok := pathID(c, "id")
	if !ok {
		return
	}

	var in SendMessageInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	view, err := h.svc.SendMessage(c.Request.Context(), user, matchID, in.ClientMsgID, in.Content)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// MarkReadInput 是上报已读的请求体。
//
// LastMsgID 可以为空：那是「我读到最新了」的意思，由服务端填上
// 当前的最大 id。让客户端去猜这个数字，它就得先知道最新一条是什么，
// 而它可能正是刚被实时推送叫醒的那一个。
type MarkReadInput struct {
	LastMsgID int64 `json:"last_msg_id"`
}

// MarkRead 上报已读水位。
//
// §18.3 没有列这个接口，但 §19.7 要求会话列表上有未读角标，
// 而角标要的数字只能由「读到哪了」算出来。放在 GET 里做会有副作用
// （任何一次预取都会把消息标成已读），所以单独一个 POST。
func (h *Handler) MarkRead(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	matchID, ok := pathID(c, "id")
	if !ok {
		return
	}

	var in MarkReadInput
	// 空 body 是合法的：等同于「读到最新」
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&in); err != nil {
			FailErr(c, apierr.ErrBadRequest)
			return
		}
	}

	watermark, err := h.svc.MarkRead(c.Request.Context(), user, matchID, in.LastMsgID)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"last_read_msg_id": watermark})
}

// ---------------------------------------------------------------- 实时凭据

// CreateWSTicket 签发一张 60 秒、一次性的实时通道入场券（§18.3）。
//
// 用 POST 而不是 GET：它每次都产生一个新的、能用的凭据，
// 而 GET 是可以被预取、被缓存的。
func (h *Handler) CreateWSTicket(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}
	view, err := h.svc.CreateWSTicket(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// pathID 解析路径里的 :id。
//
// 解析不出来时直接写响应并返回 false —— 这三个接口里它都表示
// 「客户端拼了一个不存在的地址」，那和「这个 id 不存在」是同一件事，
// 都给 404。
func pathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		FailErr(c, apierr.ErrNotFound)
		return 0, false
	}
	return id, true
}

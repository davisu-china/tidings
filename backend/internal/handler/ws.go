package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

// WSConnect 是实时通道的入口：GET /api/v1/ws?ticket=...
//
// 它不走 RequireAuth，也不套统一的 JSON 响应体 —— 浏览器连
// WebSocket 时没法自定义请求头，凭据只能出现在 query 里；
// 而握手成功之后这个连接上跑的就只有 WebSocket 帧，
// 再包一层 {code,message,data} 毫无意义。
//
// 凭据是一次性的入场券（§18.3 的 60 秒票据），不是 access token：
// query 会进代理日志和浏览器历史，把长期凭据放进去等于到处留一份。
func (h *Handler) WSConnect(c *gin.Context) {
	ticket := c.Query("ticket")
	if ticket == "" {
		FailErr(c, apierr.ErrWSTicketInvalid)
		return
	}

	user, err := h.svc.ConsumeWSTicket(c.Request.Context(), ticket)
	if err != nil {
		FailErr(c, err)
		return
	}
	if h.ws == nil {
		// 实时通道没装配（比如只跑 API 的某个环境）。
		// 明确地拒绝，不要让前端以为连上了却在等永远不会来的事件。
		FailErr(c, apierr.ErrInternal.WithMessage("实时通道未启用"))
		return
	}

	// 从这里开始这条连接归 Hub 管，它会一直阻塞到连接断开。
	h.ws.Serve(c.Writer, c.Request, user.ID)
}

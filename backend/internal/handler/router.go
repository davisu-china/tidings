// Package handler 是 HTTP 层：参数解析、鉴权、响应封装。
// 业务规则不写在这里，全部下沉到 service。
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
	"github.com/davisu-china/tidings/backend/internal/ws"
)

type Handler struct {
	svc *service.Service
	log *slog.Logger

	// ws 是长连接那一侧。它不是 service：service 只往外推事件，
	// 而握手、连接生命周期是 HTTP 层的事。
	// 为 nil 时实时通道不可用（见 WSConnect）。
	ws *ws.Hub
}

func New(svc *service.Service, hub *ws.Hub, log *slog.Logger) *Handler {
	return &Handler{svc: svc, ws: hub, log: log}
}

// Response 是全站统一响应体。Code 为 "OK" 表示成功，
// 失败时是 apierr 里定义的错误码字符串，前端按它分支处理。
type Response struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const CodeOK = "OK"

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeOK, Message: "ok", Data: data})
}

// FailErr 把 error 翻译成响应。已知的 *apierr.Error 原样输出，
// 其余一律折叠成 INTERNAL —— 不能把内部错误细节漏给客户端。
func FailErr(c *gin.Context, err error) {
	var e *apierr.Error
	if !errors.As(err, &e) {
		c.Error(err) //nolint:errcheck // 交给日志中间件记录
		e = apierr.ErrInternal
	}
	if e.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	c.AbortWithStatusJSON(e.HTTPStatus, Response{Code: e.Code, Message: e.Message})
}

// Router 装配路由。
//
// 分组结构见文档 §18：公开的 auth、需要登录的 me、需要已入池的
// 业务接口、以及独立口令鉴权的 admin。
func (h *Handler) Router(env string) *gin.Engine {
	if env != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(h.log), MetricsMiddleware())
	r.GET("/healthz", h.Healthz)
	r.GET("/metrics", h.Metrics())

	v1 := r.Group("/api/v1")

	// 公开：注册、登录、刷新、登出。
	// 登出放在这里是有意的 —— access token 过期后仍要能登出，
	// 否则用户会遇到「点了退出但退不出去」。
	auth := v1.Group("/auth")
	{
		auth.POST("/register", h.Register)
		auth.POST("/login", h.Login)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", h.Logout)

		// 这一条是例外：路径在 auth 下（§18.3），但它要登录 ——
		// 它签发的正是「已登录」这件事的另一张凭证。所以鉴权
		// 加在路由上而不是分组上。
		auth.POST("/ws-ticket", h.RequireAuth(), h.CreateWSTicket)
	}

	// 需要登录，但不要求已建档：建档流程本身就在这一组里
	me := v1.Group("", h.RequireAuth())
	{
		me.GET("/me", h.Me)
		me.GET("/me/profile", h.GetProfile)
		me.PATCH("/me/profile", h.UpdateProfile)

		me.POST("/media/upload-url", h.CreateUploadURL)
		me.PUT("/me/avatar", h.SetAvatar)
		me.GET("/me/photos", h.ListPhotos)
		me.POST("/me/photos", h.AddPhoto)
		me.PUT("/me/photos/order", h.ReorderPhotos)
		me.DELETE("/me/photos/:id", h.DeletePhoto)

		// 偏好是建档的一部分（向导最后一步就问它），所以和资料同组，
		// 不放进「已入池」那一组。
		me.GET("/me/preferences", h.GetPreference)
		me.PUT("/me/preferences", h.SavePreference)

		// 订阅推送也不要求已建档：安装引导出现在建档完成之后、
		// 请求授权之前（§19.5），那个时刻 status 刚变成 active，
		// 但把它挡在这里只会多一个「刚好卡在中间」的失败窗口。
		me.POST("/push/subscribe", h.SubscribePush)
		me.DELETE("/push/subscribe", h.UnsubscribePush)
	}

	// 已入池才有的业务接口：池子之外没有引荐可言。
	active := v1.Group("", h.RequireAuth(), h.RequireActive())
	{
		active.GET("/introductions", h.ListIntroductions)
		active.GET("/introductions/:id", h.GetIntroduction)
		active.POST("/introductions/:id/respond", h.RespondIntroduction)

		active.GET("/matches", h.ListMatches)
		active.GET("/matches/:id/messages", h.ListMessages)
		active.POST("/matches/:id/messages", h.SendMessage)
		// §18.3 没列这一条，但 §19.7 的未读角标需要一个写已读的地方，
		// 而它不能挂在 GET 上（见 handler.MarkRead）。
		active.POST("/matches/:id/read", h.MarkRead)
	}

	// 推送公钥不需要鉴权：它是公钥，而且要在安装引导里被用到。
	// 不放进 me 组是为了让「哪些接口不需要登录」在这一眼能数清。
	v1.GET("/push/public-key", h.GetPushPublicKey)

	// 实时通道。和推送公钥同理放在顶层：它既不套 JSON 信封、
	// 也不走 RequireAuth（凭据是一次性票据，在 query 里）。
	v1.GET("/ws", h.WSConnect)

	// 后台。共享口令鉴权，账号体系要等运营侧有多个角色时才值得做。
	admin := v1.Group("/admin", h.RequireAdmin())
	{
		admin.POST("/users/:id/password", h.AdminResetPassword)
	}

	return r
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		// 健康检查会被探针高频调用，成功时不记日志避免刷屏
		if c.Request.URL.Path == "/healthz" && c.Writer.Status() == http.StatusOK {
			return
		}

		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "err", c.Errors.String())
		}
		log.Info("http", attrs...)
	}
}

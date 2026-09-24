package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
)

type registerReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 邮箱 + 密码注册。
//
// 不校验邮箱真实性、不发验证码：注册成功即签发凭据直接进入建档流程。
// 代价是确认不了邮箱归属，收益是少两个外部依赖和一整轮往返。
func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	res, err := h.svc.Register(c.Request.Context(), req.Email, req.Password, c.ClientIP())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, res)
}

type loginReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	res, err := h.svc.Login(c.Request.Context(), req.Email, req.Password, c.ClientIP())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, res)
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Refresh 用 refresh token 换新凭据。它不走 RequireAuth：
// access token 过期正是调用它的原因，要求有效 access token 会死锁。
func (h *Handler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	pair, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, pair)
}

func (h *Handler) Logout(c *gin.Context) {
	// 解析失败也返回成功：客户端的目标是清掉本地状态，
	// 这里报错只会让它卡在登不出去的状态里
	var req refreshReq
	_ = c.ShouldBindJSON(&req)

	if err := h.svc.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"ok": true})
}

type meResp struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Status   string `json:"status"`
	NextStep string `json:"next_step"`
	IsAdmin  bool   `json:"is_admin"`
	// AdminToken 只在管理员本人请求时下发。前端不因此获得任何额外权限 ——
	// 后台接口仍然独立校验同一个口令，这里只是省掉再手输一次。
	AdminToken string `json:"admin_token,omitempty"`
}

// Me 返回当前登录态。前端启动时用它决定去建档向导还是首页。
func (h *Handler) Me(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	// 「他来了」就记在这里（§4.5：未响应冻结直到用户主动访问一次）。
	// 挂在 /me 上是因为这是每次打开应用必然走的一条 —— 前端 rootLoader
	// 拿它决定身份与落地页。放在引荐列表上不行：那个页面可能被
	// 后台刷新反复取，而「访问一次」指的是人打开了应用。
	h.svc.TouchVisit(c.Request.Context(), user)

	res := meResp{
		UserID:   user.ID,
		Email:    user.Email,
		Status:   user.Status,
		NextStep: service.NextStep(user.Status),
		IsAdmin:  h.svc.Cfg.IsAdmin(user.Email),
	}
	if res.IsAdmin {
		res.AdminToken = h.svc.Cfg.AdminToken
	}
	OK(c, res)
}

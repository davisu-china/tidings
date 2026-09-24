package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

type resetPasswordReq struct {
	NewPassword string `json:"new_password" binding:"required"`
	Reason      string `json:"reason"`
}

// ResetPassword 是 MVP 唯一的找回密码途径（无邮箱验证、无短信验证码）。
// 运营在后台替用户设一个新密码，然后通过电话或工单告知。
//
// 它同时递增 token_version，所以重置之后该用户所有旧登录态立即失效 ——
// 找回密码的常见诉求就是「号可能被盗了」，只换密码不踢下线等于没修。
func (h *Handler) AdminResetPassword(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		FailErr(c, apierr.ErrNotFound.WithMessage("用户不存在"))
		return
	}

	var req resetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	if err := h.svc.ResetPassword(c.Request.Context(), AdminOperator(c), userID, req.NewPassword, req.Reason); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"ok": true})
}

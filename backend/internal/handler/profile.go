package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
)

// GetProfile 读本人资料。onboarding 用户也要能读 ——
// 建档向导第一步就是拿它回填已填过的字段。
func (h *Handler) GetProfile(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	view, err := h.svc.GetProfile(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// UpdateProfile 保存资料。建档向导每一步都调它，字段可以分批提交。
//
// 用 PATCH 语义（字段缺省即不改）而不是 PUT：向导是分步的，
// 每步只带自己那几个字段，PUT 的「整体替换」会让上一步填的内容被清空。
func (h *Handler) UpdateProfile(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var in service.ProfileInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	view, err := h.svc.UpdateProfile(c.Request.Context(), user, &in)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

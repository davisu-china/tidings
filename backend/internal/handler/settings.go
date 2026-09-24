package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
)

// GetSettings 读本人的通知与引荐设置。
//
// 没建过设置行时返回默认值（不暂停、22:00–09:00），与 GetPreference
// 同一套取舍：对界面来说「没设置过」和「全默认」是同一件事。
func (h *Handler) GetSettings(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	view, err := h.svc.GetSettings(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// SaveSettings 全量替换通知与引荐设置。
//
// 用 PUT 不用 PATCH，与偏好一致：这是「我当前的设置」，没有历史价值。
// 但请求体里的三个字段都用指针接 —— 这里与偏好有一处要紧的不同：
// 偏好的「没传」和「清空」是同一件事（都是不限），而静默时段的小时数
// 有一个合法的零值。分不清的话，前端漏传 quiet_start 就会把静默时段
// 悄悄挪到零点，且不会有任何报错。
func (h *Handler) SaveSettings(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var in service.SettingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	view, err := h.svc.SaveSettings(c.Request.Context(), user, &in)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

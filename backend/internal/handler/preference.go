package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
)

// GetPreference 读期望条件。
//
// 没填过返回一份全「不限」的视图而不是 404 —— 对界面来说
// 「没填过」和「全不限」是同一件事，多一个分支只会多一处出错。
func (h *Handler) GetPreference(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	view, err := h.svc.GetPreference(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

// SavePreference 全量替换期望条件。
//
// 用 PUT 而不是 PATCH：偏好是「我当前的要求」，没有历史价值，
// 也没有「没传这个字段」和「显式清空」的区别 —— 两者都是不限。
// 这让「清空硬条件里的一项」不需要额外的哨兵值。
func (h *Handler) SavePreference(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var in service.PreferenceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	view, err := h.svc.SavePreference(c.Request.Context(), user, &in)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

// ListIntroductions 列出本人的引荐。
//
// 隐藏方的过滤在 repo 的查询里，不在这里 —— 一次查询拿到的就是
// 「这个人该看到的全部」。放在 handler 里过滤的话，将来多一个
// 调用点（比如导出、比如推送正文）就会漏一次。
func (h *Handler) ListIntroductions(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	view, err := h.svc.ListIntroductions(c.Request.Context(), user)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, view)
}

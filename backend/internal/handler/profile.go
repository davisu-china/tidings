package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/schools"
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

// SearchSchools GET /api/v1/schools?q=&limit=
//
// 建档页「毕业院校」的联想数据源：输入即查，只能从返回的项里选，
// 不再让人自由输入校名。查的是内存里的院校库（3141 条，启动时载入），
// 不打数据库，所以每次按键都查一次也扛得住。
//
// 挂在这一组（要登录）而不是像推送公钥那样公开：它只在建档与改档时用，
// 而这两个场景必然已经登录 —— 少一个对匿名请求开放的接口。
func (h *Handler) SearchSchools(c *gin.Context) {
	limit := 10
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 30 {
		limit = v
	}
	OK(c, gin.H{
		"items": schools.Search(c.Query("q"), limit),
		"total": schools.Count(),
	})
}

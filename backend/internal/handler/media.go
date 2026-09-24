package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
)

type uploadURLReq struct {
	// Ext 只要扩展名，不要整个文件名：key 一律由服务端生成，
	// 接受前端传 key 就等于允许覆盖别人的对象。
	Ext string `json:"ext" binding:"required"`
}

// CreateUploadURL 发一张直传票据。图片字节不经过 API 服务。
func (h *Handler) CreateUploadURL(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var req uploadURLReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	ticket, err := h.svc.CreateUploadTicket(c.Request.Context(), user, req.Ext)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, ticket)
}

type setAvatarReq struct {
	ObjectKey string `json:"object_key" binding:"required"`
}

func (h *Handler) SetAvatar(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var req setAvatarReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	key, err := h.svc.SetAvatar(c.Request.Context(), user, req.ObjectKey)
	if err != nil {
		FailErr(c, err)
		return
	}
	// 头像给 card 档：「我的」页面和引荐卡都用这一档，
	// 单独再取一次 thumb 没有意义
	OK(c, gin.H{"avatar_key": key, "avatar_url": h.svc.ImgURL(key, storage.VariantCard)})
}

func (h *Handler) ListPhotos(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	photos, err := h.svc.ListPhotos(c.Request.Context(), user.ID)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"photos": photos})
}

type addPhotoReq struct {
	ObjectKey string `json:"object_key" binding:"required"`
}

func (h *Handler) AddPhoto(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var req addPhotoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	photo, err := h.svc.AddPhoto(c.Request.Context(), user, req.ObjectKey)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, photo)
}

type reorderReq struct {
	// IDs 是全部照片的新顺序（position = 下标）。
	IDs []int64 `json:"ids" binding:"required"`
}

func (h *Handler) ReorderPhotos(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var req reorderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	photos, err := h.svc.ReorderPhotos(c.Request.Context(), user, req.IDs)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"photos": photos})
}

func (h *Handler) DeletePhoto(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		FailErr(c, apierr.ErrNotFound)
		return
	}

	if err := h.svc.DeletePhoto(c.Request.Context(), user, id); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"ok": true})
}

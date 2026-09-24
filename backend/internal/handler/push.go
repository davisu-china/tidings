package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/service"
)

// GetPushPublicKey 下发 VAPID 公钥。
//
// 不需要鉴权，也确实不该需要：它是公钥，前端要在登录之前就能拿到 ——
// 更实际的理由是，它会被 Service Worker 和安装引导用到，
// 而在那些时机带上 token 只会多一处过期判断。
//
// 未配置密钥时返回空串而不是报错：前端据此知道「这个环境没有推送」，
// 可以安静地跳过订阅步骤，而不是弹一个他无法解决的错误。
// 生产环境缺密钥在 config 校验里已经硬失败了。
func (h *Handler) GetPushPublicKey(c *gin.Context) {
	OK(c, gin.H{"public_key": h.svc.Cfg.Push.PublicKey})
}

// SubscribePush 登记一条推送订阅。
func (h *Handler) SubscribePush(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var in service.PushSubscribeInput
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	err := h.svc.SubscribePush(c.Request.Context(), user, &in, c.Request.UserAgent())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"subscribed": true})
}

// UnsubscribePush 退订。
//
// endpoint 走请求体而不是路径参数：它是一条最长 1024 字符、含有
// 大量 / 与 = 的完整 URL，塞进路径要先转义，转义规则两边还得对齐。
// DELETE 带 body 是允许的，浏览器与 fetch 都支持。
func (h *Handler) UnsubscribePush(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		FailErr(c, apierr.ErrUnauthorized)
		return
	}

	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		FailErr(c, apierr.ErrBadRequest)
		return
	}

	if err := h.svc.UnsubscribePush(c.Request.Context(), user, in.Endpoint); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"subscribed": false})
}

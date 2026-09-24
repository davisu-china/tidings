package handler

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
)

const ctxUserKey = "current_user"

// RequireAuth 校验 access token 并加载用户。
//
// 每次请求都回查 users 行，不做缓存：需要 status 与 token_version
// 的最新值。缓存它们会让封禁和多端下线延迟生效，而这两件事
// 恰恰是要求「立刻」的。
func (h *Handler) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			FailErr(c, apierr.ErrUnauthorized)
			return
		}

		claims, err := h.svc.JWT.Parse(token, jwtutil.TypeAccess)
		if err != nil {
			if errors.Is(err, jwtutil.ErrExpiredToken) {
				FailErr(c, apierr.ErrTokenExpired)
				return
			}
			FailErr(c, apierr.ErrUnauthorized)
			return
		}

		user, err := h.svc.LoadUser(c.Request.Context(), claims.UserID)
		if err != nil {
			FailErr(c, err)
			return
		}
		// token_version 对不上说明该用户被重置密码、封禁或全端下线过，
		// 手里这张 token 已经作废
		if user.TokenVersion != claims.TokenVersion {
			FailErr(c, apierr.ErrUnauthorized.WithMessage("登录状态已失效，请重新登录"))
			return
		}
		if user.Status == model.StatusBanned {
			FailErr(c, apierr.ErrAccountBanned)
			return
		}
		if user.Status == model.StatusDeactivated {
			FailErr(c, apierr.ErrForbidden.WithMessage("账号已注销"))
			return
		}

		c.Set(ctxUserKey, user)
		c.Next()
	}
}

// RequireActive 要求已入池（status = active）。
//
// 用在「池子之外无意义」的接口上：没建档的人没有引荐可言，
// 让他拿到一个空列表和让他拿到 PROFILE_INCOMPLETE 是两件事 ——
// 前者看起来像产品坏了，后者前端可以直接把人送回建档向导。
// 这是 §19.6 说的「强制建档拦截的正确落点」：拦在数据接口上，
// 而不是拦在路由跳转上（跳转拦不住直接敲地址和刷新）。
//
// under_review 也放行：审核期间资料已经完整，把他的列表清空
// 只会让他以为被封了。真正该拦住的是还没建档的 onboarding。
func (h *Handler) RequireActive() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		if user == nil {
			FailErr(c, apierr.ErrUnauthorized)
			return
		}
		if user.Status == model.StatusOnboarding {
			FailErr(c, apierr.ErrProfileIncomplete)
			return
		}
		c.Next()
	}
}

// RequireAdmin 校验运营身份。
//
// MVP 用共享口令而不是账号体系：真正的后台用户体系要等运营侧
// 有多个角色时才值得做。口令从配置来，且必须显式设置 ——
// IsAdmin 在白名单为空时返回 false，不会出现「没配就等于人人都是管理员」。
func (h *Handler) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.svc.Cfg.AdminToken == "" {
			FailErr(c, apierr.ErrForbidden.WithMessage("后台未启用"))
			return
		}
		if c.GetHeader("X-Admin-Token") != h.svc.Cfg.AdminToken {
			FailErr(c, apierr.ErrForbidden)
			return
		}
		c.Next()
	}
}

// AdminOperator 取操作人标识，用于 admin_actions 留痕。
// 后台还没做登录，先允许调用方用 X-Operator 自报，默认记为 admin。
func AdminOperator(c *gin.Context) string {
	if op := strings.TrimSpace(c.GetHeader("X-Operator")); op != "" {
		return op
	}
	return "admin"
}

func bearerToken(c *gin.Context) (string, bool) {
	raw := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(raw[len(prefix):])
	return token, token != ""
}

// CurrentUser 取当前登录用户。链路正确时不会返回 nil。
func CurrentUser(c *gin.Context) *model.User {
	v, ok := c.Get(ctxUserKey)
	if !ok {
		return nil
	}
	user, ok := v.(*model.User)
	if !ok {
		return nil
	}
	return user
}

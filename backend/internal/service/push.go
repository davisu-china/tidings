package service

import (
	"context"
	"net/url"
	"strings"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

// 推送订阅的登记。发送在 worker 里（§14.2），这里只管「谁订阅了」。

// PushSubscribeInput 是 POST /push/subscribe 的请求体。
//
// 形状就是浏览器 pushManager.subscribe() 的返回值的子集：
// { endpoint, keys: { p256dh, auth } }。不重命名、不摊平 ——
// 前端可以直接把订阅对象整个 POST 过来，中间少一层映射就少一处
// 「字段名写错了但没人发现」的机会。
type PushSubscribeInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// endpoint 的长度上限与表上的 push_endpoint_len_chk 一致。
// 在这里先拦一道，是为了给出「地址太长」而不是一个约束名报错。
const maxPushEndpointLen = 1024

// SubscribePush 登记一条订阅。
//
// userAgent 由 handler 从请求头取，不由请求体传：那是浏览器自己说的话，
// 让客户端自报就失去了诊断价值 —— 而它唯一的用途就是诊断
// 「这个人到底是哪台设备收不到推送」。
func (s *Service) SubscribePush(ctx context.Context, user *model.User, in *PushSubscribeInput, userAgent string) error {
	if err := validatePushSub(in); err != nil {
		return err
	}
	// UA 截断：它只是一个诊断字段，不该成为一条写不进去的记录。
	// 表上没给它加长度约束，所以截在这里。
	const maxUA = 256
	if len(userAgent) > maxUA {
		userAgent = userAgent[:maxUA]
	}
	return s.Repo.UpsertPushSub(ctx, user.ID, in.Endpoint, in.Keys.P256DH, in.Keys.Auth, userAgent)
}

// UnsubscribePush 退订。
//
// 幂等：删一条不存在的订阅不算失败。退订是用户想摆脱推送，
// 让他因为「这条订阅早就没了」而看到一个错误，只会让他以为没退成。
func (s *Service) UnsubscribePush(ctx context.Context, user *model.User, endpoint string) error {
	if endpoint == "" {
		return apierr.ErrBadRequest.WithMessage("缺少订阅地址")
	}
	return s.Repo.DeletePushSub(ctx, user.ID, endpoint)
}

// validatePushSub 校验订阅三要素。
//
// endpoint 只校验「是不是一个带 host 的 http(s) 地址」，不去连它：
// 地址是否真的能发，只有发的那一刻才知道（§14.3 的 404/410 处理），
// 在这里做一次探测只会把注册拖慢，而且探通了也不代表待会儿还通。
func validatePushSub(in *PushSubscribeInput) error {
	if in.Endpoint == "" {
		return apierr.ErrBadRequest.WithMessage("缺少订阅地址")
	}
	if len(in.Endpoint) > maxPushEndpointLen {
		return apierr.ErrBadRequest.WithMessage("订阅地址过长")
	}
	u, err := url.Parse(in.Endpoint)
	if err != nil || u.Host == "" {
		return apierr.ErrBadRequest.WithMessage("订阅地址格式不正确")
	}
	// 本地开发走 http://localhost，所以只拦「既不是 http 也不是 https」。
	// 公网推送服务的 endpoint 一律是 https，这个口子开不到线上。
	if u.Scheme != "https" && u.Scheme != "http" {
		return apierr.ErrBadRequest.WithMessage("订阅地址格式不正确")
	}

	// 两个密钥是 Web Push 加密的全部材料，缺一条这条订阅就永远发不出去。
	// 表上它们是 NOT NULL，但空串能过 NOT NULL —— 所以这里必须显式拦。
	if strings.TrimSpace(in.Keys.P256DH) == "" {
		return apierr.ErrBadRequest.WithMessage("缺少推送密钥")
	}
	if strings.TrimSpace(in.Keys.Auth) == "" {
		return apierr.ErrBadRequest.WithMessage("缺少推送密钥")
	}
	return nil
}

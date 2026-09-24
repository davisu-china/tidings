// Package push 封装 Web Push（VAPID）投递。
//
// 这一层只做一件事：把一条通知发给一个订阅。它不认识 outbox、
// 不认识引荐，也不决定「该不该打扰这个人」—— 那是调用方在投递前
// 查完三道闸之后的事（§14.2）。
//
// 关键区分是「这条订阅还能不能用」：
//
//	404 / 410  → endpoint 已永久失效，重试没有意义，直接停用
//	其它失败    → 可能只是对端抖了一下，记一次失败继续重试
//
// 混为一谈的代价是单向的：把可重试的当永久失效，用户在网络恢复后
// 再也收不到推送，而他毫不知情 —— 这个产品没有邮件兜底。
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Subscription 是一条浏览器推送订阅。
type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}

// Payload 是推送给浏览器的内容。
//
// 保持极小：推送体经由推送服务商中转，只放「有个新引荐，去 App 看」
// 这种索引信息，不放进对方的昵称、照片、分数。真正的内容在站内，
// 用户点进来才加载 —— 这既是隐私考虑，也让 payload 清空（§14.2）
// 不至于损失任何东西。
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL 是点击后打开的站内路径。
	URL string `json:"url"`
	// Tag 让同一条引荐的多次通知互相覆盖，而不是堆成一串。
	Tag string `json:"tag,omitempty"`
}

// ErrGone 表示这条订阅已永久失效（HTTP 404 / 410）。
// 调用方收到它就停用订阅，不要重试。
var ErrGone = errors.New("push: 订阅已永久失效")

// Sender 用一对 VAPID 密钥发推送。
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	// client 可替换，方便测试里指向一个假推送服务。
	client *http.Client
}

func NewSender(publicKey, privateKey, subject string) *Sender {
	return &Sender{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		client:     &http.Client{},
	}
}

// Send 发一条推送。
//
// 返回 ErrGone 之外的非 nil 错误都视为可重试。这里的判断不看错误文本，
// 只看状态码 —— 推送服务商的错误措辞会变，状态码不会。
func (s *Sender) Send(ctx context.Context, sub Subscription, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256DH,
			Auth:   sub.Auth,
		},
	}, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             ttlSeconds,
		Urgency:         webpush.UrgencyNormal,
		HTTPClient:      s.client,
	})
	if err != nil {
		// 网络层就失败了（DNS、连接、超时），一律可重试
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// 浏览器把订阅丢了（清缓存、卸载 Web App、换设备）
		return ErrGone
	default:
		// 把响应体带上前 512 字节：推送服务商的诊断信息基本都在里面，
		// 而不带的话只能看到一个状态码，排查时等于没有信息。
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("push: 推送服务返回 %d: %s", resp.StatusCode, snippet)
	}
}

// TTL：推送服务商为离线设备保留这条消息的秒数。
//
// 12 小时而不是更长：引荐本身 72 小时就过期（§4.4），一条过了半天的
// 「有新引荐」推到达，用户点进去看到的可能已经是收尾状态。
// 短 TTL 让它自然消失，比送达一条已经过时的通知好。
const ttlSeconds = 12 * 60 * 60

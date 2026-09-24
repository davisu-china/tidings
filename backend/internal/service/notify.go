package service

import (
	"encoding/json"
	"fmt"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"gorm.io/gorm"
)

// 通知入队。所有外发消息（推送、以后的邮件）都从这里进 outbox，
// 没有任何地方直接调推送服务 —— 那会让「谁被通知过」散在多个文件里，
// 重试和幂等也就无从谈起。
//
// 这里只负责「写一行 pending」，投递前的三道闸（未响应冻结、静默时段、
// 暂停接收）由 outbox worker 在真正要发的时候查（§14.2）。
// 分两处的理由：闸门查的是「此刻该不该打扰他」，而此刻是投递那一刻，
// 不是入队那一刻 —— 中间可能隔了静默时段的一整夜。
//
// 必须在调用方的事务里执行：通知和它描述的那件事要么一起成功，
// 要么一起没有。否则会出现「引荐存在但永远不会被通知」
// 或者反过来「通知一条不存在的引荐」。

// EnqueueNotify 往 outbox 写一行。
//
// dedupKey 是幂等的全部实现，形状形如 intro:123:low:delivered。
// 同一个 key 只可能有一行 —— 重试、并发、worker 重启都不会让
// 同一个人收到重复推送。key 由调用方给，因为它携带业务含义。
func (s *Service) EnqueueNotify(tx *gorm.DB, userID int64, tpl string, dedupKey string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.Repo.EnqueueOutbox(tx, repo.OutboxInsert{
		Channel:  model.ChannelPush,
		UserID:   userID,
		Template: tpl,
		Payload:  raw,
		DedupKey: dedupKey,
	})
}

// introDeliveredKey 拼「新引荐」通知的幂等键。
//
// side 用 low / high 而不是「我 / 他」：同一条引荐对两个人各有一行，
// 用侧别拼键天然不会重复，而用「当前处理者的 id」拼会在换个人重跑时
// 生成第二个键。
func introDeliveredKey(introID int64, side string) string {
	return fmt.Sprintf("intro:%d:%s:delivered", introID, side)
}

// sideOf 返回 uid 在这一对里是 low 还是 high。
// 调用方保证 uid 一定是其中之一。
func sideOf(uid, low, high int64) string {
	if uid == low {
		return model.SideLow
	}
	return model.SideHigh
}

// hiddenSideOf 返回「另一方」的侧别。单向引荐藏的就是他。
func hiddenSideOf(uid, low, high int64) string {
	if uid == low {
		return model.SideHigh
	}
	return model.SideLow
}

package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// 用户设置的写侧（§18.2 的 GET PUT /me/settings）。读侧在 outbox.go ——
// 那边是投递前读三道闸，这边是用户自己改。

// SettingsWrite 是一次设置全量写入的内容。
type SettingsWrite struct {
	IntrosPaused bool
	QuietStart   int16
	QuietEnd     int16
}

// SaveUserSettings 写一次设置，返回「这次是不是一次暂停恢复」。
//
// 暂停与恢复不是简单地把一列改掉（§13.2）：
//
//	开始暂停 → 记下 paused_at，那是冻结的起点
//	恢复     → 把这个人所有未终结引荐的时限按暂停时长顺延，清掉 paused_at
//
// 两件事必须在同一个事务里跟那次 UPDATE 一起做完。分开做的话，
// 中间挂掉会留下「已经不暂停了、但时限还停在过去」的行 ——
// 下一轮扫描立刻把它们整批判成超时，而用户一封都没来得及看。
//
// 先把行锁住再读旧值：两个并发请求（比如两个标签页各点一次）
// 都读到「之前在暂停」，就会各自顺延一次，时限凭空翻倍。
func (r *Repo) SaveUserSettings(ctx context.Context, uid int64, in SettingsWrite) (resumed bool, err error) {
	err = r.Tx(func(tx *gorm.DB) error {
		// 先保证行存在，FOR UPDATE 才锁得住东西。新用户从没碰过设置，
		// 第一次进来时这里就是插入。
		if err := tx.Exec(
			`INSERT INTO user_settings (user_id) VALUES (?) ON CONFLICT (user_id) DO NOTHING`,
			uid).Error; err != nil {
			return err
		}

		var cur []struct {
			Paused   bool       `gorm:"column:intros_paused"`
			PausedAt *time.Time `gorm:"column:paused_at"`
		}
		if err := tx.Raw(
			`SELECT intros_paused, paused_at FROM user_settings WHERE user_id = ? FOR UPDATE`,
			uid).Scan(&cur).Error; err != nil {
			return err
		}
		if len(cur) == 0 {
			// 上面那句刚插过，这里为空只可能是有人同时删了行。
			return nil
		}
		was, since := cur[0].Paused, cur[0].PausedAt

		// paused_at 只在「这一次真的处于暂停中」时有值。恢复时清空，
		// 留着它会让下一次恢复把同一段时间重复顺延一遍。
		var pausedAt *time.Time
		if in.IntrosPaused {
			if was && since != nil {
				pausedAt = since // 一直在暂停，起点不动
			} else {
				now := time.Now()
				pausedAt = &now // 刚开始暂停
			}
		}

		if err := tx.Exec(`
			UPDATE user_settings
			SET intros_paused = ?, paused_at = ?, quiet_start = ?, quiet_end = ?, updated_at = now()
			WHERE user_id = ?`,
			in.IntrosPaused, pausedAt, in.QuietStart, in.QuietEnd, uid).Error; err != nil {
			return err
		}

		// 恢复：把暂停期间被冻住的时限还回去。
		//
		// 顺延量取 (now - GREATEST(paused_at, created_at))，不是文档里
		// 那一句 (now - paused_at)。两者在文档描述的场景下完全等价
		// （引荐在暂停前就存在 → created_at 更早 → GREATEST 取 paused_at），
		// 但文档那句漏了另一种行：暂停期间新生成的引荐，created_at
		// 晚于 paused_at；照文档写会把它的时限算成「72 小时 + 暂停时长」，
		// 平白多给。GREATEST 让它在恢复时拿到完整的一轮时限，
		// 而那正是「他的钟从恢复那一刻才开始走」的意思。
		if was && !in.IntrosPaused && since != nil {
			if err := tx.Exec(`
				UPDATE introductions
				SET expires_at = expires_at + (now() - GREATEST(?, created_at))
				WHERE (user_low = ? OR user_high = ?)
				  AND state IN ('pending', 'viewed', 'responded')`,
				*since, uid, uid).Error; err != nil {
				return err
			}
			resumed = true
		}
		return nil
	})
	return resumed, err
}

// BumpUnopenedStreak 记一次「推了但没打开」，连续两次就冻结（§4.5）。
//
// 放在投递成功之后而不是投递之前：推失败的通知不该算进「未打开」——
// 用户根本没机会打开它，把他冻上是不讲理的。
//
// unopened_streak 与 push_frozen 两列必须在 PG 里，「连续」是跨天的
// 语义，放 Redis 会因过期丢失计数，用户永远等不到冻结（§11）。
func (r *Repo) BumpUnopenedStreak(ctx context.Context, uid int64) error {
	return r.DB.WithContext(ctx).Exec(`
		INSERT INTO user_settings (user_id, unopened_streak, push_frozen)
		VALUES (?, 1, false)
		ON CONFLICT (user_id) DO UPDATE SET
		    unopened_streak = user_settings.unopened_streak + 1,
		    push_frozen     = (user_settings.unopened_streak + 1) >= 2,
		    updated_at      = now()`,
		uid).Error
}

// ClearPushFreeze 记一次「他回来了」，解冻并清空计数（§4.5：
// 「直到用户主动访问一次」）。
//
// WHERE 上带两个条件不是优化，是这件事能挂在 /me 上的前提 ——
// /me 是每次打开应用都会走的路，绝大多数时候这个人既没被冻也没计数。
// 带上条件之后那些请求改到 0 行，是一次货真价实的空操作；
// 不带的话，每次打开应用都写一行 WAL，为了一件没发生的事。
func (r *Repo) ClearPushFreeze(ctx context.Context, uid int64) error {
	return r.DB.WithContext(ctx).Exec(`
		UPDATE user_settings
		SET unopened_streak = 0, push_frozen = false, updated_at = now()
		WHERE user_id = ? AND (unopened_streak <> 0 OR push_frozen)`,
		uid).Error
}

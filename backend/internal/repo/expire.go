package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// 超时扫描的读写（§15.3）。M4 之前引荐只生成、不终结 ——
// 这是「有结尾」的那一半。

// ExpireCandidate 是一条到期该终结的引荐。
//
// 只带 id，不带双方的暂停状态：暂停的判断在 SQL 里做掉了。
// 取回来再过滤会让冻结的行被反复白扫 —— 那些行可能挂几周，
// 而一轮只有 200 个名额。
type ExpireCandidate struct {
	ID int64 `gorm:"column:id"`
}

// ListExpiredIntros 取一批已到期、且两边都没在暂停中的未终结引荐。
//
// 走 idx_intro_expire（WHERE state IN ('pending','viewed','responded')
// 的部分索引），只扫未终结的行，不会随历史数据增长而变慢。
//
// 「任一方在暂停中就跳过」是 §13.2 的计时冻结：要求一个不在线的人
// 按时回应是不合理的，所以这一行原地不动 —— 既不推进状态，
// 也不发收尾通知。它会在对方恢复时被重算时限（见 SaveUserSettings），
// 然后自然进入下一轮的候选。
//
// 另一方全程不知道对方处于暂停状态，他看到的始终是「等待回应」。
func (r *Repo) ListExpiredIntros(ctx context.Context, limit int) ([]ExpireCandidate, error) {
	var rows []ExpireCandidate
	err := r.DB.WithContext(ctx).Raw(`
		SELECT i.id
		FROM introductions i
		WHERE i.state IN ('pending', 'viewed', 'responded')
		  AND i.expires_at <= now()
		  AND NOT EXISTS (
		      SELECT 1 FROM user_settings s
		      WHERE s.user_id IN (i.user_low, i.user_high)
		        AND s.intros_paused
		  )
		ORDER BY i.expires_at
		LIMIT ?`, limit).Scan(&rows).Error
	return rows, err
}

// ExpireIntro 把一条引荐推进到 expired，返回推进后的那一行。
//
// 第二个返回值是「这次真的推进了吗」。false 表示不用管：行没了，
// 或者在这一瞬间被表态结算掉了。
//
// 回的是写入侧的模型而不是读侧的 IntroRow：收尾通知要按 kind、
// hidden_side 和双方的 action 决定发给谁（§4.8），这几列都在这一侧，
// 而 IntroRow 是「以某个人为视角」的形状，本来就没有 hidden_side。
//
// 行锁是必须的，不是保险。候选是几百毫秒前查出来的，而一批有 200 行、
// 一行一行处理 —— 排在第 200 位的行，从被选中到真正被改之间隔着
// 前面 199 行的往返。这中间对方完全可能点了「想认识」而把它变成
// matched。没有锁的话，我们会把一条已经成匹配的引荐改成 expired，
// 两个人刚建立的会话会凭空消失，而且没有任何东西会报错。
//
// 所以拿到锁之后要把「还是不是 open」「是不是真的到期了」重新判一次。
func (r *Repo) ExpireIntro(tx *gorm.DB, id int64, now time.Time) (*model.Introduction, bool, error) {
	in, err := r.LockIntro(tx, id)
	if err != nil || in == nil {
		return nil, false, err
	}
	if !model.IsOpenState(in.State) || in.ExpiresAt.After(now) {
		return nil, false, nil
	}

	if err := tx.Exec(
		`UPDATE introductions SET state = 'expired', closed_at = now() WHERE id = ?`, id).Error; err != nil {
		return nil, false, err
	}
	in.State = model.IntroExpired
	in.ClosedAt = &now
	return in, true, nil
}

package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
)

// 会话的读写（§4.7）。matches 那一行就是会话本身 —— 没有单独的
// conversations 表：一条匹配对应一条会话，多一张表只会多一处
// 「它们俩对不上」的可能。
//
// 消息的幂等靠 UNIQUE (match_id, client_msg_id)，已读靠 match_reads
// 的水位列。两件事都不放在 Redis 里：Redis 里的东西会过期，
// 而「这条读没读过」过期之后就再也算不回来了。

// MatchRow 是匹配列表里的一行：会话本身 + 对方的展示信息 + 最后一条消息。
type MatchRow struct {
	ID        int64  `gorm:"column:id"`
	Status    string `gorm:"column:status"`
	CreatedAt time.Time
	LastMsgAt *time.Time `gorm:"column:last_msg_at"`

	OtherID  int64   `gorm:"column:other_id"`
	Nickname *string `gorm:"column:nickname"`
	CoverKey *string `gorm:"column:cover_key"`

	// 最后一条消息。三个字段一起为空表示还没人说过话。
	LastContent    *string    `gorm:"column:last_content"`
	LastSenderID   *int64     `gorm:"column:last_sender_id"`
	LastMsgCreated *time.Time `gorm:"column:last_msg_created"`

	Unread int `gorm:"column:unread"`
}

// ListMatches 列出一个人参与的会话，最近有消息的排前面。
//
// 未读数是这条查询里的一个标量子查询，不是每条再查一次 ——
// 那会变成 N+1（stubborn-love 那边就是这么写的，是它被点名的
// 唯一一处性能问题）。会话列表是有上限的一屏，一条 SQL 出得完。
//
// 只列 active：blocked / closed 的会话在列表上留着，等于给用户
// 一个点进去发不出消息的入口。M5 做拉黑时这里再按需要放开。
func (r *Repo) ListMatches(ctx context.Context, uid int64) ([]MatchRow, error) {
	var rows []MatchRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT m.id, m.status, m.created_at, m.last_msg_at,
		       o.user_id AS other_id, o.nickname,
		       ph.object_key AS cover_key,
		       lm.content     AS last_content,
		       lm.sender_id   AS last_sender_id,
		       lm.created_at  AS last_msg_created,
		       (SELECT count(*) FROM messages msg
		         WHERE msg.match_id = m.id
		           AND msg.sender_id <> ?
		           AND msg.id > coalesce(mr.last_read_msg_id, 0)) AS unread
		FROM matches m
		JOIN profiles o
		  ON o.user_id = CASE WHEN m.user_low = ? THEN m.user_high ELSE m.user_low END
		LEFT JOIN photos ph ON ph.user_id = o.user_id AND ph.position = 0
		LEFT JOIN match_reads mr ON mr.match_id = m.id AND mr.user_id = ?
		LEFT JOIN LATERAL (
		    SELECT content, sender_id, created_at
		    FROM messages
		    WHERE match_id = m.id
		    ORDER BY id DESC
		    LIMIT 1
		) lm ON true
		WHERE (m.user_low = ? OR m.user_high = ?)
		  AND m.status = 'active'
		ORDER BY m.last_msg_at DESC NULLS LAST, m.created_at DESC`,
		uid, uid, uid, uid, uid).Scan(&rows).Error
	return rows, err
}

// GetMatch 取一条会话。不是当事人返回 (nil, nil) —— 与引荐同一套
// 反枚举规则：拿别人的 match_id 来问，答案只有「不存在」。
//
// 未读数与 ListMatches 用同一套算法（同一段标量子查询）。这里不能
// 图省事写 0：消息接口把它一并返回，而会话页正是靠它决定要不要上报
// 已读水位 —— 恒为 0 的话，进了会话角标也不会清。
//
// 最后一条消息那三列仍然是空的：它们是列表页要的东西，而调这个接口
// 的人手里就有整个消息列表，不需要再算一遍。
func (r *Repo) GetMatch(ctx context.Context, uid, matchID int64) (*MatchRow, error) {
	var rows []MatchRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT m.id, m.status, m.created_at, m.last_msg_at,
		       o.user_id AS other_id, o.nickname,
		       ph.object_key AS cover_key,
		       NULL::text        AS last_content,
		       NULL::bigint      AS last_sender_id,
		       NULL::timestamptz AS last_msg_created,
		       (SELECT count(*) FROM messages msg
		         WHERE msg.match_id = m.id
		           AND msg.sender_id <> ?
		           AND msg.id > coalesce(mr.last_read_msg_id, 0)) AS unread
		FROM matches m
		JOIN profiles o
		  ON o.user_id = CASE WHEN m.user_low = ? THEN m.user_high ELSE m.user_low END
		LEFT JOIN photos ph ON ph.user_id = o.user_id AND ph.position = 0
		LEFT JOIN match_reads mr ON mr.match_id = m.id AND mr.user_id = ?
		WHERE m.id = ? AND (m.user_low = ? OR m.user_high = ?)`,
		uid, uid, uid, matchID, uid, uid).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// ---------------------------------------------------------------- 消息

// MessageRow 是一条消息。方向由 SenderID 与查询者比对得出，
// 不在这里翻成 is_mine —— repo 不知道「谁在问」以外的业务含义。
type MessageRow struct {
	ID          int64     `gorm:"column:id"`
	MatchID     int64     `gorm:"column:match_id"`
	SenderID    int64     `gorm:"column:sender_id"`
	Content     string    `gorm:"column:content"`
	ClientMsgID string    `gorm:"column:client_msg_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

// ListMessages 按游标往前翻一页消息。
//
// 游标分页而不是页码（§18.3 后的那行说明）：消息只会往后加，
// 页码分页在翻页时必然重复或漏。
//
// beforeID 为 0 表示「最新的一页」。查询按 id 倒序取 limit+1 条，
// 多出来的那条只用来判断还有没有更早的，不返回给调用方。
func (r *Repo) ListMessages(ctx context.Context, matchID, beforeID int64, limit int) ([]MessageRow, error) {
	var rows []MessageRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT id, match_id, sender_id, content, client_msg_id, created_at
		FROM messages
		WHERE match_id = ?
		  AND (? = 0 OR id < ?)
		ORDER BY id DESC
		LIMIT ?`, matchID, beforeID, beforeID, limit+1).Scan(&rows).Error
	return rows, err
}

// InsertMessage 落一条消息，返回它的 id 和「是不是这次落进去的」。
//
// created 为 false 表示 (match_id, client_msg_id) 撞了 —— 客户端的
// 重发。调用方据此走「把原来那条查回来」的路径，而不是报错：
// 用户点了发送、网络抖了一下、前端重试，他期待的结果是消息在，
// 不是一句「已经发过了」。
//
// 只在这一层做幂等，不做业务校验（内容长度、会话是否 active）——
// 校验在 service。表上的 CHECK 是最后一道，不是第一道。
func (r *Repo) InsertMessage(tx *gorm.DB, m MessageRow) (id int64, created bool, err error) {
	row := tx.Raw(`
		INSERT INTO messages (match_id, sender_id, content, client_msg_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (match_id, client_msg_id) DO NOTHING
		RETURNING id`,
		m.MatchID, m.SenderID, m.Content, m.ClientMsgID).Row()

	switch err = row.Scan(&id); {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	default:
		return 0, false, err
	}
}

// GetMessageByClientID 把重发时那条已存在的消息取回来。
func (r *Repo) GetMessageByClientID(ctx context.Context, matchID int64, clientMsgID string) (*MessageRow, error) {
	var rows []MessageRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT id, match_id, sender_id, content, client_msg_id, created_at
		FROM messages
		WHERE match_id = ? AND client_msg_id = ?`, matchID, clientMsgID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// TouchLastMsg 推进会话的「最后一条消息时刻」。
//
// 与插入消息同一个事务：它是列表排序的唯一依据，落后一次就会让
// 刚聊过的那条会话沉到下面去，而消息本身是好的 —— 这类不一致
// 没有任何报错，只能靠事务边界避免。
//
// 用 GREATEST 而不是直接赋值：乱序到达的写入不能让时间倒流。
func (r *Repo) TouchLastMsg(tx *gorm.DB, matchID int64, at time.Time) error {
	return tx.Exec(
		`UPDATE matches SET last_msg_at = GREATEST(coalesce(last_msg_at, to_timestamp(0)), ?)
		 WHERE id = ?`, at, matchID).Error
}

// ---------------------------------------------------------------- 已读

// MarkRead 推进一个人的已读水位。
//
// GREATEST：并发或乱序上报不能让水位后退，否则已经读过的消息
// 会重新变成未读。没有行时插一行，0 是默认值也是「没读过」。
func (r *Repo) MarkRead(ctx context.Context, matchID, uid, lastMsgID int64) error {
	return r.DB.WithContext(ctx).Exec(`
		INSERT INTO match_reads (match_id, user_id, last_read_msg_id, updated_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (match_id, user_id) DO UPDATE SET
		    last_read_msg_id = GREATEST(match_reads.last_read_msg_id, EXCLUDED.last_read_msg_id),
		    updated_at = now()`,
		matchID, uid, lastMsgID).Error
}

// LastMessageID 取一条会话里最新的消息 id，没有消息时返回 0。
// 「全部标记为已读」需要它 —— 客户端只说「我读到最新了」，
// 不该把具体 id 交给它去猜。
func (r *Repo) LastMessageID(ctx context.Context, matchID int64) (int64, error) {
	var id int64
	err := r.DB.WithContext(ctx).
		Raw(`SELECT coalesce(max(id), 0) FROM messages WHERE match_id = ?`, matchID).
		Scan(&id).Error
	return id, err
}

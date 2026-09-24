package repo

import (
	"context"
	"time"
)

// 引荐的读侧。生成侧（候选集、打分、插入、升级）在 matching.go ——
// 那边的调用者是 worker，这边是 API，两批人关心的事情不一样，
// 混在一个文件里只会让「谁在调它」变模糊。
//
// 表本身仍然是同一张 introductions，写入侧的字段约定见 matching.go。

// IntroRow 是引荐列表的一行：这条引荐本身 + 对方的展示信息。
//
// 对方的字段是可空的：MVP 里入池要求必填 8 项，但那是「入池时」的要求，
// 而资料随时可以清空（比如把收入改成不填）。用指针接住 NULL，
// 让「没填」在界面上显示成缺席，而不是变成一个 0。
type IntroRow struct {
	ID      int64  `gorm:"column:id"`
	BatchID string `gorm:"column:batch_id"`
	Kind    string `gorm:"column:kind"`
	State   string `gorm:"column:state"`
	IssueNo int64  `gorm:"column:issue_no"`

	CreatedAt  time.Time  `gorm:"column:created_at"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	MyViewedAt *time.Time `gorm:"column:my_viewed_at"`
	MyAction   *string    `gorm:"column:my_action"`

	// MySide 是「我」在这条引荐里是 low 还是 high（'low' / 'high'）。
	//
	// 和 my_viewed_at / my_action 是同一个 CASE 手法：读侧拿到的每一行
	// 都是「以调用者为视角」的，user_low / user_high 这两列不透出去。
	// 详情页的「打开」跃迁要按侧别写列，有了它就不用再查一次。
	MySide string `gorm:"column:my_side"`

	OtherID        int64   `gorm:"column:other_id"`
	Nickname       *string `gorm:"column:nickname"`
	BirthYM        *int    `gorm:"column:birth_ym"`
	CityCode       *int    `gorm:"column:city_code"`
	EducationLevel *int16  `gorm:"column:education_level"`
	HeightCM       *int16  `gorm:"column:height_cm"`
	IncomeBand     *int16  `gorm:"column:income_band"`
	Intro          string  `gorm:"column:intro"`

	// Occupation 只有详情查询会填。列表卡片上不出现这一项（§19.2
	// 只画了昵称/出生年/城市/学历/身高/收入/自述），但 §4.6 的
	// 「引荐页展示」里有职业 —— 两张单子不一样，所以列表那边 SELECT
	// 不带这一列，扫出来就是 nil，响应里也不会出现这个键。
	Occupation *string `gorm:"column:occupation"`

	// CoverKey 是对方 position = 0 的那张照片。position 0 就是封面，
	// 表上没有 is_main —— 见 000001 的照片表注释。
	CoverKey *string `gorm:"column:cover_key"`

	// Unread 是「有没有看过」，由调用方按 state 一起判断。
	// 它不是一个列，是为了让 service 少写一次 nil 判断。
	Unread bool `gorm:"-"`
}

// ListIntroductions 列出一个人能看到的全部引荐。
//
// 「能看到」不等于「与他有关」，两者差在单向引荐的隐藏方：那条引荐
// 根本没递给他，他也不知道有这回事。所以这里要把隐藏方过滤掉 ——
// 漏掉这一步，单向引荐的隐藏方会在自己的列表里看到「他对我有意」，
// 而这正是 §13.3 明确不许发生的事。
//
// 期号（issue_no）用 DENSE_RANK 按「批次」编号，也就是卡片上那行
// 「引荐 · 第 12 期」。同一个 batch_id 的多条共号 —— 一次推送携带
// 1–3 个人，它们在用户眼里是同一封信。
//
// 排序键里带上 batch_id 不只是为了稳定：同一批次的行 created_at 完全相同
// （同一个事务里 now() 是事务时间戳），只按时间排会让同一个批次内部
// 的顺序随机，期号也就跟着飘。
//
// 编号在过滤之后才做，这是有意的：编号是「你收到的第几封信」，
// 从用户视角数才对。带上隐藏方的行去编号，他看到的期号会平白跳号。
//
// 隐藏判断必须用 IS DISTINCT FROM，不能写成 NOT (user_low = ? AND
// hidden_side = 'low')。后者看起来等价，实际上成对引荐（hidden_side
// 是 NULL）会被整片滤掉：NULL = 'low' 得到 NULL，AND 之后还是 NULL，
// NOT NULL 还是 NULL，而 WHERE 只认 TRUE。成对引荐里低 id 那一侧
// 因此永远看不到自己的信 —— 看上去什么都没报错，只是信不见了。
// 判断「有没有被藏起来」时 NULL 的含义是「没有藏」，IS DISTINCT FROM
// 正是这个意思。
func (r *Repo) ListIntroductions(ctx context.Context, uid int64) ([]IntroRow, error) {
	var rows []IntroRow
	err := r.DB.WithContext(ctx).Raw(`
		WITH visible AS (
		    SELECT i.id, i.batch_id, i.kind, i.state, i.created_at, i.expires_at,
		           CASE WHEN i.user_low = ? THEN i.low_viewed_at ELSE i.high_viewed_at END AS my_viewed_at,
		           CASE WHEN i.user_low = ? THEN i.low_action    ELSE i.high_action    END AS my_action,
		           CASE WHEN i.user_low = ? THEN i.user_high     ELSE i.user_low       END AS other_id,
		           CASE WHEN i.user_low = ? THEN 'low'           ELSE 'high'           END AS my_side
		    FROM introductions i
		    WHERE (i.user_low = ? OR i.user_high = ?)
		      AND (i.user_low  <> ? OR i.hidden_side IS DISTINCT FROM 'low')
		      AND (i.user_high <> ? OR i.hidden_side IS DISTINCT FROM 'high')
		),
		ranked AS (
		    SELECT v.*,
		           (DENSE_RANK() OVER (ORDER BY v.created_at, v.batch_id))::bigint AS issue_no
		    FROM visible v
		)
		SELECT r.id, r.batch_id, r.kind, r.state, r.issue_no,
		       r.created_at, r.expires_at, r.my_viewed_at, r.my_action,
		       r.my_side, r.other_id,
		       o.nickname, o.birth_ym, o.city_code, o.education_level,
		       o.height_cm, o.income_band, o.intro,
		       ph.object_key AS cover_key
		FROM ranked r
		JOIN profiles o ON o.user_id = r.other_id
		LEFT JOIN photos ph ON ph.user_id = o.user_id AND ph.position = 0
		ORDER BY r.created_at DESC, r.id DESC`,
		uid, uid, uid, uid, uid, uid, uid, uid).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Unread = isUnread(rows[i].MyViewedAt, rows[i].State)
	}
	return rows, nil
}

// isUnread 判一条引荐对某个人算不算未读。
//
// 只看他这一侧的 viewed_at，不看 state —— 状态是这条引荐的，
// 而「读过没有」是每个人的。写成 `MyViewedAt == nil && state == 'pending'`
// 会漏掉真正要报的那一种：成对引荐里先看的往往是对方，他一看，
// state 就翻成 viewed，另一个人的未读角标跟着消失 —— 而他从没打开过。
//
// 两个收尾状态除外。declined / expired 是「已经封上的信」（§19.2），
// 界面上不给回看入口，把它们算成未读等于挂一个点不掉的红点。
func isUnread(viewedAt *time.Time, state string) bool {
	if viewedAt != nil {
		return false
	}
	return state != "declined" && state != "expired"
}

// GetIntroRow 取一条引荐，形状与列表里的一行完全一致。
//
// 隐藏方的过滤和列表用同一套条件：单向引荐的隐藏方拿到的是「不存在」，
// 不是「无权查看」—— §18.3 明确非本人返回 404 而非 403。
// 用 403 等于告诉他这个 id 是真的，反枚举规则就破了。
func (r *Repo) GetIntroRow(ctx context.Context, uid, introID int64) (*IntroRow, error) {
	var rows []IntroRow
	err := r.DB.WithContext(ctx).Raw(`
		SELECT i.id, i.batch_id, i.kind, i.state,
		       0::bigint AS issue_no,
		       CASE WHEN i.user_low = ? THEN i.low_viewed_at ELSE i.high_viewed_at END AS my_viewed_at,
		       CASE WHEN i.user_low = ? THEN i.low_action    ELSE i.high_action    END AS my_action,
		       CASE WHEN i.user_low = ? THEN i.user_high     ELSE i.user_low       END AS other_id,
		       CASE WHEN i.user_low = ? THEN 'low'           ELSE 'high'           END AS my_side,
		       i.created_at, i.expires_at,
		       o.nickname, o.birth_ym, o.city_code, o.education_level,
		       o.height_cm, o.income_band, o.intro, o.occupation,
		       ph.object_key AS cover_key
		FROM introductions i
		JOIN profiles o ON o.user_id = CASE WHEN i.user_low = ? THEN i.user_high ELSE i.user_low END
		LEFT JOIN photos ph ON ph.user_id = o.user_id AND ph.position = 0
		WHERE i.id = ?
		  AND (i.user_low = ? OR i.user_high = ?)
		  AND (i.user_low  <> ? OR i.hidden_side IS DISTINCT FROM 'low')
		  AND (i.user_high <> ? OR i.hidden_side IS DISTINCT FROM 'high')`,
		uid, uid, uid, uid, uid, introID, uid, uid, uid, uid).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	row.Unread = isUnread(row.MyViewedAt, row.State)
	return &row, nil
}

package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// CandidateRow 是候选集查询回来的一行。列名与 service.Subject 对齐 ——
// 那条 SQL 是刻意只取打分用得到的列，不是 SELECT *。
type CandidateRow struct {
	UserID int64 `gorm:"column:user_id"`
	Gender string

	BirthYM  int    `gorm:"column:birth_ym"`
	CityCode int    `gorm:"column:city_code"`
	HeightCM *int16 `gorm:"column:height_cm"`
	EduLevel *int16 `gorm:"column:education_level"`
	// 下面这些是选填项，可能为 NULL
	HometownCode *int       `gorm:"column:hometown_code"`
	IncomeBand   *int16     `gorm:"column:income_band"`
	Chronotype   *int16     `gorm:"column:chronotype"`
	Smoking      *int16     `gorm:"column:smoking"`
	Drinking     *int16     `gorm:"column:drinking"`
	WantChild    *int16     `gorm:"column:want_child"`
	Marital      *int16     `gorm:"column:marital_status"`
	LastActiveAt *time.Time `gorm:"column:last_active_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`

	// 对方的偏好（LEFT JOIN 回来，可能整片为 NULL）
	PrefBirthYMMin *int    `gorm:"column:pref_birth_ym_min"`
	PrefBirthYMMax *int    `gorm:"column:pref_birth_ym_max"`
	PrefCityCodes  []int64 `gorm:"column:pref_city_codes"`
	PrefWantChild  *int16  `gorm:"column:pref_want_child"`
	PrefDivorced   *int16  `gorm:"column:pref_accept_divorced"`
	PrefRemote     *int16  `gorm:"column:pref_accept_remote"`
	PrefEduMin     *int16  `gorm:"column:pref_edu_min"`
	PrefHeightMin  *int16  `gorm:"column:pref_height_min"`
	PrefHeightMax  *int16  `gorm:"column:pref_height_max"`
	PrefIncomeMin  *int16  `gorm:"column:pref_income_min"`
	PrefIncomeMax  *int16  `gorm:"column:pref_income_max"`
	PrefExists     bool    `gorm:"column:pref_exists"`
}

// CandidateQuery 是候选集召回的全部入参。
type CandidateQuery struct {
	MeID       int64
	MyGender   string
	MyBirthYM  int
	MyCityCode int
	// 我声明的偏好。没填过就都是零值 / nil，等价于全部「不限」。
	PrefBirthYMMin *int
	PrefBirthYMMax *int
	CityCodes      []int64
	Limit          int
}

// subjectCols 是「一个人 + 他的偏好」这套列的唯一定义。
//
// 三处查询要用同一套列（候选集、按 id 取一批、取自己），抄三份的话
// 加一列就要改三个地方，漏一个就静默少一个字段 —— 而打分器遇到
// 缺失字段不会报错，只会把它当「没填」，分数悄悄偏低。
// 拼 SQL 用的全是编译期常量，没有任何外部输入参与，不存在注入面。
const subjectCols = `
       p.user_id, p.gender, p.birth_ym, p.city_code, p.height_cm, p.education_level,
       p.hometown_code, p.income_band, p.chronotype, p.smoking, p.drinking,
       p.want_child, p.marital_status,
       u.last_active_at, u.created_at,
       pref.user_id IS NOT NULL   AS pref_exists,
       pref.birth_ym_min          AS pref_birth_ym_min,
       pref.birth_ym_max          AS pref_birth_ym_max,
       pref.city_codes            AS pref_city_codes,
       pref.want_child            AS pref_want_child,
       pref.accept_divorced       AS pref_accept_divorced,
       pref.accept_remote         AS pref_accept_remote,
       pref.edu_min               AS pref_edu_min,
       pref.height_min            AS pref_height_min,
       pref.height_max            AS pref_height_max,
       pref.income_min            AS pref_income_min,
       pref.income_max            AS pref_income_max`

// subjectFrom 是上面那套列对应的 FROM 子句。三者必须成对修改。
const subjectFrom = `
FROM profiles p
JOIN users u ON u.id = p.user_id
LEFT JOIN preferences pref ON pref.user_id = p.user_id`

// candidateSQL 是 §12.3 的候选集查询。
//
// 与设计稿的版本有两处出入，都是有意的：
//
//  1. 原稿写 `p.city_code = ANY(pref.city_codes)`，但 pref 是**候选人自己的**
//     偏好行，拿他自己的城市去比他自己接受的城市，恒等于「他在自己接受的城市里」，
//     是个恒真条件。正确的语义是「我的城市在他接受的城市列表里」，
//     所以右边比的是我的 city_code。
//  2. 年龄两个方向都判：他的偏好要收得下我，我的偏好也要收得下他。
//     原稿第二行写成 `? BETWEEN coalesce(pref...)`，比的还是他的偏好，
//     我的偏好没有参与 —— 那会让「我只想找 30 岁以下」形同虚设。
//
// 另外 ORDER BY random() 与 LIMIT 的组合让每次生成拿到的候选组合都不同，
// 避免同一批人反复互推。LIMIT 是硬上限：池子小的时候不生效，
// 池子涨到万级时它是唯一阻止这条查询变慢的东西。
const candidateSQL = `
SELECT ` + subjectCols + `
` + subjectFrom + `
WHERE u.status = 'active'
  AND p.user_id <> ?                      -- 不是我
  AND p.gender IS NOT NULL
  AND p.gender <> ?                       -- 异性，写死不做同性
  AND p.completeness >= 60                -- 对方也得过引荐门槛
  AND p.city_code IS NOT NULL
  AND p.birth_ym IS NOT NULL
  -- 硬条件：他的年龄偏好要收得下我
  AND (pref.birth_ym_min IS NULL OR ? >= pref.birth_ym_min)
  AND (pref.birth_ym_max IS NULL OR ? <= pref.birth_ym_max)
  -- 硬条件：我的年龄偏好要收得下他
  AND (?::int IS NULL OR p.birth_ym >= ?::int)
  AND (?::int IS NULL OR p.birth_ym <= ?::int)
  -- 硬条件：我的城市要在他接受的城市里（NULL = 不限）
  AND (pref.city_codes IS NULL OR ? = ANY(pref.city_codes))
  -- 排重：没有未终结的引荐，也没有已经成过匹配的
  --
  -- matched 要一起排掉，而 declined / expired 不能：M3 起「成匹配」是一种
  -- 终结态，两个人已经在会话里了，再收到一封介绍信是荒唐的。反过来，
  -- 被婉拒和超时释放的引荐按 §4.8 正是要「回到池子里」再推一次，
  -- 排掉它们等于把池子越推越小。
  AND NOT EXISTS (
      SELECT 1 FROM introductions i
      WHERE ((i.user_low = ? AND i.user_high = p.user_id)
          OR (i.user_high = ? AND i.user_low = p.user_id))
        AND i.state IN ('pending','viewed','responded','matched')
  )
  -- 排重：没拉黑（双向）
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = ? AND b.blocked_id = p.user_id)
         OR (b.blocker_id = p.user_id AND b.blocked_id = ?)
  )
  -- 同城或同省
  AND (p.city_code = ? OR (p.city_code / 10000) = (? / 10000))
ORDER BY random()
LIMIT ?`

// FetchCandidates 召回候选集。所有条件都在这条 SQL 里，GO 侧只负责打分 ——
// 这一层的职责分工写死在 repo 包的注释里，不要往这里塞业务判断。
func (r *Repo) FetchCandidates(ctx context.Context, q CandidateQuery) ([]CandidateRow, error) {
	var rows []CandidateRow
	err := r.DB.WithContext(ctx).Raw(candidateSQL,
		q.MeID,                             // 不是我
		q.MyGender,                         // 异性
		q.MyBirthYM,                        // 他的下限收得下我
		q.MyBirthYM,                        // 他的上限收得下我
		q.PrefBirthYMMin, q.PrefBirthYMMin, // 我的下限（判两次是为了让 NULL 短路成立）
		q.PrefBirthYMMax, q.PrefBirthYMMax, // 我的上限
		q.MyCityCode,   // 我的城市在他接受的城市里
		q.MeID, q.MeID, // 没有未终结的引荐
		q.MeID, q.MeID, // 没拉黑
		q.MyCityCode, q.MyCityCode, // 同城或同省
		q.Limit,
	).Scan(&rows).Error
	return rows, err
}

// PoolSize 数一个城市里有多少可用的人：已 active、过了引荐门槛、性别已知。
//
// 不分性别，因为它衡量的是「这个城市的市场规模」——§12.4 的
// 500 / 2000 两条线是按整个城市的活跃用户数定的策略档位，不是按
// 每个人能看到的异性数量。男女比例失衡到需要分开算的时候，
// 这里加一个 gender 参数即可。
func (r *Repo) PoolSize(ctx context.Context, cityCode int) (int, error) {
	var n int64
	err := r.DB.WithContext(ctx).
		Raw(`SELECT count(*) FROM profiles p
		     JOIN users u ON u.id = p.user_id
		     WHERE u.status = 'active'
		       AND p.completeness >= 60
		       AND p.gender IS NOT NULL
		       AND p.city_code = ?`, cityCode).
		Scan(&n).Error
	return int(n), err
}

// ActiveCities 列出所有有可用用户的城市。生成任务逐城处理，
// 因为池子阶梯（§12.4）是分城算的 —— 全国池子再大，
// 也救不了一个只有 20 个人的城市。
func (r *Repo) ActiveCities(ctx context.Context) ([]int, error) {
	var rows []struct {
		CityCode int `gorm:"column:city_code"`
	}
	err := r.DB.WithContext(ctx).
		Raw(`SELECT DISTINCT p.city_code
		     FROM profiles p
		     JOIN users u ON u.id = p.user_id
		     WHERE u.status = 'active'
		       AND p.completeness >= 60
		       AND p.gender IS NOT NULL
		       AND p.city_code IS NOT NULL
		     ORDER BY p.city_code`).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	cities := make([]int, 0, len(rows))
	for _, r := range rows {
		cities = append(cities, r.CityCode)
	}
	return cities, nil
}

// LoadSelves 按 id 批量取「一个人 + 他的偏好」。
//
// 批量而不是逐个：生成一轮里要算自己和每个候选人的双向分，
// 加上待升级的单向引荐对手方，逐个取会变成几十次往返。
//
// 这里必须是 IN (?) 而不是 = ANY(?)：GORM 会把切片参数摊成若干个
// 占位符，ANY 拿到的是 `ANY($1,$2)`（两个以上）或者一个被当成
// bigint[] 的标量（只有一个）—— 前者是语法错误，后者是编码错误，
// 而且一个人时才对得上号，排查时非常费解。IN 要的正是摊开的形式。
func (r *Repo) LoadSelves(ctx context.Context, ids []int64) ([]CandidateRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []CandidateRow
	err := r.DB.WithContext(ctx).
		Raw(`SELECT `+subjectCols+subjectFrom+` WHERE p.user_id IN (?)`, ids).
		Scan(&rows).Error
	return rows, err
}

// LoadSelf 取一个人。取不到返回 (nil, nil) —— 调用方按「这个人已经不可用」处理。
func (r *Repo) LoadSelf(ctx context.Context, uid int64) (*CandidateRow, error) {
	rows, err := r.LoadSelves(ctx, []int64{uid})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// OpenOnewayHiddenFrom 列出「我被藏起来」的未终结单向引荐。
//
// §13.3 的升级路径靠它。场景是：A 收到过一条关于我的单向引荐（我不知情），
// 而我现在对 A 的分数也过线了 —— 那就把这条升级成成对，双方同时看到。
//
// 判据是「我是不是隐藏方」，不是「我是不是可见方」：可见方那条引荐
// 早就递出去了，不需要重算；需要重算的恰恰是我这一侧。
//
// 这类引荐的对手方被候选集 SQL 的排重条件挡住了（同一对已有未终结引荐），
// 所以生成时根本看不到他，必须单独捞出来。
func (r *Repo) OpenOnewayHiddenFrom(ctx context.Context, uid int64) ([]struct {
	IntroID int64 `gorm:"column:intro_id"`
	OtherID int64 `gorm:"column:other_id"`
}, error) {
	var rows []struct {
		IntroID int64 `gorm:"column:intro_id"`
		OtherID int64 `gorm:"column:other_id"`
	}
	err := r.DB.WithContext(ctx).Raw(`
		SELECT i.id AS intro_id,
		       CASE WHEN i.user_low = ? THEN i.user_high ELSE i.user_low END AS other_id
		FROM introductions i
		WHERE i.kind = 'oneway'
		  AND i.state IN ('pending','viewed','responded')
		  AND (
		        (i.user_low = ? AND i.hidden_side = 'low')
		     OR (i.user_high = ? AND i.hidden_side = 'high')
		      )`, uid, uid, uid).Scan(&rows).Error
	return rows, err
}

// ------------------------------------------------------------ 偏好读写

// GetPreference 读一个人的偏好。没有行返回 (nil, nil) ——
// 「没填过」和「填了但全是空」在打分时等价（都是全不限），
// 但对界面不一样：前者要显示「不限」，后者也是。所以这里不区分。
func (r *Repo) GetPreference(ctx context.Context, uid int64) (*PrefRow, error) {
	var rows []PrefRow
	err := r.DB.WithContext(ctx).
		Raw(`SELECT user_id, birth_ym_min, birth_ym_max, city_codes,
		            want_child, accept_divorced, accept_remote,
		            edu_min, height_min, height_max, income_min, income_max
		     FROM preferences WHERE user_id = ?`, uid).
		Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// PrefRow 与 preferences 表一一对应。
type PrefRow struct {
	UserID         int64   `gorm:"column:user_id"`
	BirthYMMin     *int    `gorm:"column:birth_ym_min"`
	BirthYMMax     *int    `gorm:"column:birth_ym_max"`
	CityCodes      []int64 `gorm:"column:city_codes"`
	WantChild      *int16  `gorm:"column:want_child"`
	AcceptDivorced *int16  `gorm:"column:accept_divorced"`
	AcceptRemote   *int16  `gorm:"column:accept_remote"`
	EduMin         *int16  `gorm:"column:edu_min"`
	HeightMin      *int16  `gorm:"column:height_min"`
	HeightMax      *int16  `gorm:"column:height_max"`
	IncomeMin      *int16  `gorm:"column:income_min"`
	IncomeMax      *int16  `gorm:"column:income_max"`
}

// UpsertPreference 整行覆盖。偏好是「当前要求」，没有历史价值，
// 所以不做增量合并 —— 前端每次提交的都是完整的一份。
func (r *Repo) UpsertPreference(ctx context.Context, p PrefRow) error {
	return r.DB.WithContext(ctx).Exec(`
		INSERT INTO preferences (user_id, birth_ym_min, birth_ym_max, city_codes,
		                         want_child, accept_divorced, accept_remote,
		                         edu_min, height_min, height_max, income_min, income_max,
		                         updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?, now())
		ON CONFLICT (user_id) DO UPDATE SET
		    birth_ym_min = EXCLUDED.birth_ym_min,
		    birth_ym_max = EXCLUDED.birth_ym_max,
		    city_codes = EXCLUDED.city_codes,
		    want_child = EXCLUDED.want_child,
		    accept_divorced = EXCLUDED.accept_divorced,
		    accept_remote = EXCLUDED.accept_remote,
		    edu_min = EXCLUDED.edu_min,
		    height_min = EXCLUDED.height_min,
		    height_max = EXCLUDED.height_max,
		    income_min = EXCLUDED.income_min,
		    income_max = EXCLUDED.income_max,
		    updated_at = now()`,
		p.UserID, p.BirthYMMin, p.BirthYMMax, p.CityCodes,
		p.WantChild, p.AcceptDivorced, p.AcceptRemote,
		p.EduMin, p.HeightMin, p.HeightMax, p.IncomeMin, p.IncomeMax,
	).Error
}

// ------------------------------------------------------- 生成任务的选人

// PickUsersForGeneration 取这一轮要处理的用户，按「距上次收到引荐最久」排序。
//
// 这个排序是公平性的全部实现：不这么排的话，先注册的人会被反复推、
// 后进来的人永远轮不到。从没收到过引荐的人排在最前（NULLS FIRST）。
func (r *Repo) PickUsersForGeneration(ctx context.Context, cityCode int, limit int) ([]int64, error) {
	// 扫进一个单列结构体而不是 []int64：GORM 的 Scan 支持前者是确定的，
	// 后者要靠 Pluck，而 Pluck 配 Raw 的行为不值得赌。
	var rows []struct {
		ID int64 `gorm:"column:id"`
	}
	err := r.DB.WithContext(ctx).Raw(`
		SELECT u.id
		FROM users u
		JOIN profiles p ON p.user_id = u.id
		LEFT JOIN user_settings s ON s.user_id = u.id
		LEFT JOIN LATERAL (
		    SELECT max(i.created_at) AS last_at
		    FROM introductions i
		    WHERE i.user_low = u.id OR i.user_high = u.id
		) hist ON true
		WHERE u.status = 'active'
		  AND p.completeness >= 60
		  AND p.city_code = ?
		  AND NOT coalesce(s.intros_paused, false)
		ORDER BY hist.last_at ASC NULLS FIRST, u.id ASC
		LIMIT ?`, cityCode, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// ------------------------------------------------------------- 租约

// ClaimLease 抢任务租约。
//
// 条件里的 leased_until < now() 保证只有一个实例能改到这行 ——
// 返回 true 表示抢到，false 表示别人正在跑，本轮跳过。
// 语义与 FOR UPDATE SKIP LOCKED 同源，只是持有时长更长。
//
// 注意租约只防重复劳动，不保证正确性：两个实例同时跑完全程也不会
// 产生重复引荐，那由 uniq_intro_open_pair 兜底。
func (r *Repo) ClaimLease(ctx context.Context, name string, lease time.Duration) (bool, error) {
	res := r.DB.WithContext(ctx).Exec(`
		UPDATE job_runs
		SET leased_until = now() + ?::interval,
		    last_run_at  = now()
		WHERE name = ?
		  AND leased_until < now()`, lease.String(), name)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// FinishLease 记录本轮结果，并把租约立刻放开 ——
// 不放开的话任务跑完还要空等到租约到期，下一轮才可能开始。
func (r *Repo) FinishLease(ctx context.Context, name, result string) error {
	return r.DB.WithContext(ctx).Exec(
		`UPDATE job_runs SET leased_until = to_timestamp(0), last_result = ? WHERE name = ?`,
		result, name).Error
}

// --------------------------------------------------------- 引荐的读写

// LockOpenPair 锁住某一对用户之间那条未终结的引荐，没有就返回 nil。
//
// 生成任务用它来发现「已存在同对的 open 单向引荐」——
// 那种情况不能插新行（uniq_intro_open_pair 会拒绝），必须走升级。
func (r *Repo) LockOpenPair(tx *gorm.DB, low, high int64) (*OpenPair, error) {
	var rows []OpenPair
	err := tx.Raw(`
		SELECT id, kind, state, hidden_side
		FROM introductions
		WHERE user_low = ? AND user_high = ?
		  AND state IN ('pending','viewed','responded')
		FOR UPDATE`, low, high).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// OpenPair 是排重与升级判定要用到的最小信息。
type OpenPair struct {
	ID         int64   `gorm:"column:id"`
	Kind       string  `gorm:"column:kind"`
	State      string  `gorm:"column:state"`
	HiddenSide *string `gorm:"column:hidden_side"`
}

// InsertIntroduction 插入一条新引荐，返回它的 id。
//
// 第二个返回值是「真的插进去了」。并发下另一个实例刚好插进同一对时，
// uniq_intro_open_pair 让这条 INSERT 静默地什么都不做（ON CONFLICT
// DO NOTHING + RETURNING 于是不返回行），此时 created 为 false，
// 调用方按「本轮跳过」处理，不当作故障。
//
// 用 RETURNING 而不是再查一次：插入和取 id 之间没有窗口，
// 也就不会出现「插进去了但拿到的 id 是别人那条」。
func (r *Repo) InsertIntroduction(tx *gorm.DB, in *IntroInsert) (id int64, created bool, err error) {
	row := tx.Raw(`
		INSERT INTO introductions
		    (batch_id, user_low, user_high, kind, state, hidden_side,
		     score_low_to_high, score_high_to_low, expires_at)
		VALUES (?::uuid, ?, ?, ?, 'pending', ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		in.BatchID, in.UserLow, in.UserHigh, in.Kind, in.HiddenSide,
		in.ScoreLowToHigh, in.ScoreHighToLow, in.ExpiresAt,
	).Row()

	switch err = row.Scan(&id); {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		// 撞上了 uniq_intro_open_pair：这一对已经有未终结的引荐了
		return 0, false, nil
	default:
		return 0, false, err
	}
}

// IntroInsert 是插入一条引荐所需的全部字段。
type IntroInsert struct {
	BatchID        string
	UserLow        int64
	UserHigh       int64
	Kind           string
	HiddenSide     *string
	ScoreLowToHigh *float64
	ScoreHighToLow *float64
	ExpiresAt      time.Time
}

// ------------------------------------------------- 引荐的生命周期（写）

// LockIntro 取出一条引荐并锁住它，直到事务结束。
//
// §13.4 的原子结算靠这一句。双方几乎同时表态时，没有行锁就会各自
// 读到「对方还没动」，两边都不建 match —— 一对本该成立的人凭空消失，
// 而且没有任何东西会报错。
//
// 取不到返回 (nil, nil)：调用方按「这条引荐不存在」处理，
// 不区分「真的不存在」与「不属于你」。
func (r *Repo) LockIntro(tx *gorm.DB, id int64) (*model.Introduction, error) {
	var in model.Introduction
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(&in).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// 一方的三个动作列。列名是编译期常量，不拼任何外部输入。
func sideColumns(side string) (action, actionAt, reason string, err error) {
	switch side {
	case model.SideLow:
		return "low_action", "low_action_at", "low_reason", nil
	case model.SideHigh:
		return "high_action", "high_action_at", "high_reason", nil
	}
	return "", "", "", fmt.Errorf("未知的侧别 %q", side)
}

// sideViewedColumn 同理，是「这一方看过了没有」的那一列。
func sideViewedColumn(side string) (string, error) {
	switch side {
	case model.SideLow:
		return "low_viewed_at", nil
	case model.SideHigh:
		return "high_viewed_at", nil
	}
	return "", fmt.Errorf("未知的侧别 %q", side)
}

// WriteDecision 把一次表态的全部结果写下去：动作列 + 状态 + 时限 + 终结时刻。
//
// 合成一条 UPDATE 而不是分成几条：调用方此刻持有行锁，中间态不会被任何人
// 看到，拆开只会多几次往返。
//
// state 由 service 决策后传入（那段决策是纯函数，单独可测），
// 这一层不判断该不该匹配。
func (r *Repo) WriteDecision(tx *gorm.DB, d Decision) error {
	aCol, atCol, rCol, err := sideColumns(d.Side)
	if err != nil {
		return err
	}
	return tx.Exec(`UPDATE introductions
		SET `+aCol+` = ?, `+atCol+` = ?, `+rCol+` = ?,
		    state = ?, expires_at = ?, closed_at = ?
		WHERE id = ?`,
		d.Action, d.At, d.Reason, d.State, d.ExpiresAt, d.ClosedAt, d.IntroID).Error
}

// Decision 是一次表态落库所需的全部字段。
type Decision struct {
	IntroID   int64
	Side      string
	Action    string
	Reason    *string
	At        time.Time
	State     string
	ExpiresAt time.Time
	ClosedAt  *time.Time
}

// MarkViewed 记下「这一方打开了这条引荐」。
//
// 两件事分得很清楚：
//   - 我这一侧的 viewed_at 一定写上（列表上的未读角标按它算）
//   - 状态和时限只在「我是第一个打开的人」时动 —— §13.2 规定 viewed 的
//     七天从首次查看起算，是这条引荐的一个计时器，不是每人一个。
//     第二个人打开时把它再推七天，等于让先看的那个人白等。
//
// 返回 false 表示这一侧早就看过了（重复打开、并发打开），静默跳过。
//
// 不需要行锁：两个并发请求里必然只有一个的 WHERE 成立（`col` IS NULL），
// 另一个改到 0 行 —— 这正是我们要的语义，而且比抢锁便宜。
func (r *Repo) MarkViewed(ctx context.Context, introID int64, side string, expiresAt time.Time) (bool, error) {
	col, err := sideViewedColumn(side)
	if err != nil {
		return false, err
	}
	// SET 里的两个 CASE 读的都是这一行的旧值，所以两处判断的是同一个
	// state，不会互相影响。
	res := r.DB.WithContext(ctx).Exec(`
		UPDATE introductions
		SET `+col+` = now(),
		    state = CASE WHEN state = 'pending' THEN 'viewed' ELSE state END,
		    expires_at = CASE WHEN state = 'pending' THEN ? ELSE expires_at END
		WHERE id = ? AND `+col+` IS NULL`, expiresAt, introID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ---------------------------------------------------------------- 匹配

// CreateMatch 建一条匹配，返回它的 id 和「是不是这次建的」。
//
// ON CONFLICT DO NOTHING + 回头再查，与 InsertIntroduction 同一手法：
// matches 的 UNIQUE (user_low, user_high) 是第二道保险（§13.4），
// 即使行锁失效也不会出现两条。
//
// 撞上冲突时把已有那条的 id 查回来，而不是报告失败 —— 一对用户之间
// 那条会话是复用的：消息历史挂在 match_id 上，另起一条会让两个人
// 各自看到半截聊天记录。
func (r *Repo) CreateMatch(tx *gorm.DB, low, high int64) (id int64, created bool, err error) {
	row := tx.Raw(`
		INSERT INTO matches (user_low, user_high, status)
		VALUES (?, ?, 'active')
		ON CONFLICT (user_low, user_high) DO NOTHING
		RETURNING id`, low, high).Row()

	switch err = row.Scan(&id); {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return r.FindMatchID(tx, low, high), false, nil
	default:
		return 0, false, err
	}
}

// FindMatchID 查某一对用户之间的匹配 id，没有就返回 0。
func (r *Repo) FindMatchID(tx *gorm.DB, low, high int64) int64 {
	var id int64
	// 查不到时 id 保持 0，与「没有匹配」是同一个值，不需要额外区分
	_ = tx.Raw(`SELECT id FROM matches WHERE user_low = ? AND user_high = ?`, low, high).
		Scan(&id).Error
	return id
}

// UpgradeToPaired 把一条单向引荐升级为成对（§13.3）。
//
// 必须是 UPDATE 而不是「终结旧的、插入新的」：uniq_intro_open_pair
// 会拒绝同一对的第二条未终结引荐。这个索引之所以存在，就是逼出这里的
// UPDATE —— 详见迁移文件里那条索引上方的注释。
//
// expiresAt 由调用方给，值是「被藏起来那一侧的 72 小时」。升级后
// 他才第一次看到这条引荐，所以他的时限要从现在重新算，
// 而可见方此前的等待被完整保留（kind 变了，行没换）。
//
// 返回 false 表示这一行已经被别人升级过了（RowsAffected == 0）。
// 那是并发信号而不是错误 —— 与处理 matches 唯一冲突的手法一致：
// 不重复发通知，静默返回。
func (r *Repo) UpgradeToPaired(tx *gorm.DB, introID int64, expiresAt time.Time) (bool, error) {
	res := tx.Exec(`
		UPDATE introductions
		SET kind = 'paired',
		    hidden_side = NULL,
		    expires_at = ?
		WHERE id = ?
		  AND kind = 'oneway'
		  AND state IN ('pending','viewed','responded')`, expiresAt, introID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

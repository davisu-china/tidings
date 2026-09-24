package service

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 引荐生成（§12–§13）。这个文件是 M2 的心脏：把匹配引擎算出来的分数
// 变成 introductions 表里的一行行记录。
//
// 分三层，职责不要混：
//   - match.go    纯函数打分，不碰数据库、不读时钟（now 一律当参数传）
//   - 本文件       决定「谁配谁、配成什么样」，读写都走 repo
//   - worker/     只负责按节奏调用本文件，以及抢租约
//
// 幂等靠 uniq_intro_open_pair 兜底，不靠这里的判断：任何「先查再插」
// 都有并发窗口，而这个唯一索引没有。所以插入一律 ON CONFLICT DO NOTHING。

// 一轮里处理多少个用户。
//
// 配置里只给了每用户推几条（INTRO_BATCH_SIZE=3）和每人扫多少候选
// （MATCH_SCAN_LIMIT=200），没给每轮处理多少人。取 50 是让一轮稳稳
// 跑完在 4 分钟的租约之内：50 次候选集查询 + 纯内存打分。
// 池子涨上来之后这个值要跟着调，或者改成按耗时提前收工。
const usersPerRound = 50

// introTTL 是新引荐的初始时限：未查看 72 小时（§4.4）。
//
// 「已查看未表态 7 天」「已表态等对方 7 天」是状态跃迁时重算的，
// 不在生成这里 —— 生成出来的引荐一律是 pending。
const introTTL = 72 * time.Hour

// GenStats 一轮生成的结果，用于日志与运维观察。
type GenStats struct {
	Cities       int
	SkippedTowns int // 池子太小而跳过的城市数
	Users        int
	Paired       int
	Oneway       int
	Upgraded     int
	Duplicates   int // 撞上唯一索引、本轮没插进去的
	Skipped      int // 算出来但没达标的组合
}

// scoredPair 是一个候选人的双向分数。
//
// 方向是这套引擎最容易搞混的地方，所以字段名写死成「谁对谁」：
// Out 是我对他的分，In 是他对我的分。
type scoredPair struct {
	UserID int64
	Out    float64 // Score(我 → 他)
	In     float64 // Score(他 → 我)
}

// bothPass 是成对的门槛：两个方向都要过线（决策 13）。
func (p scoredPair) bothPass(threshold float64) bool {
	return p.Out >= threshold && p.In >= threshold
}

// GenerateIntroductions 跑一轮生成。
//
// 调用方（worker）负责抢租约。本函数可以安全地被并发调用 ——
// 最坏情况是白算一遍，不会产生重复引荐。
func (s *Service) GenerateIntroductions(ctx context.Context) (GenStats, error) {
	var stats GenStats
	now := time.Now()
	threshold := s.Cfg.Match.Threshold

	cities, err := s.Repo.ActiveCities(ctx)
	if err != nil {
		return stats, err
	}
	stats.Cities = len(cities)

	for _, city := range cities {
		// 池子阶梯每城单独算：全国池子再大，也救不了一个只有 20 人的城市
		pool, err := s.Repo.PoolSize(ctx, city)
		if err != nil {
			return stats, err
		}
		tier := TierFor(pool, threshold, s.Cfg.Intro.PoolMin)
		if tier.TooSmall {
			stats.SkippedTowns++
			continue
		}

		userIDs, err := s.Repo.PickUsersForGeneration(ctx, city, usersPerRound)
		if err != nil {
			return stats, err
		}
		for _, uid := range userIDs {
			n, err := s.generateForUser(ctx, uid, tier, now)
			if err != nil {
				// 一个人算失败不该拖垮整轮。记下来继续 ——
				// 下一轮按「距上次收到引荐最久」排序时他还会排在最前，
				// 所以失败不会让谁被永久跳过。
				s.Log.ErrorContext(ctx, "生成引荐失败",
					slog.Int64("user_id", uid), slog.Any("err", err))
				continue
			}
			stats.Users++
			stats.Paired += n.Paired
			stats.Oneway += n.Oneway
			stats.Upgraded += n.Upgraded
			stats.Duplicates += n.Duplicates
			stats.Skipped += n.Skipped
		}
	}

	s.Log.InfoContext(ctx, "引荐生成完成",
		slog.Int("cities", stats.Cities),
		slog.Int("skipped_towns", stats.SkippedTowns),
		slog.Int("users", stats.Users),
		slog.Int("paired", stats.Paired),
		slog.Int("oneway", stats.Oneway),
		slog.Int("upgraded", stats.Upgraded),
		slog.Int("duplicates", stats.Duplicates),
		slog.Int("skipped", stats.Skipped))
	return stats, nil
}

// generateForUser 为一个人算一轮引荐。
//
// 顺序有讲究：先处理升级，再处理新配对。升级不占本轮的新引荐名额
// （它复用的是已有的那条记录），所以不该被 batch 上限挤掉。
func (s *Service) generateForUser(ctx context.Context, uid int64, tier Tier, now time.Time) (GenStats, error) {
	var stats GenStats

	self, err := s.Repo.LoadSelf(ctx, uid)
	if err != nil || self == nil {
		return stats, err
	}
	me := toSubject(*self)
	// 没有性别或城市就做不了异性同城的匹配。这些人在 M1 里
	// 不该是 active，但状态是运营能改的，所以这里再挡一次。
	if me.Gender == "" || me.CityCode == 0 || me.BirthYM == 0 {
		return stats, nil
	}

	upgraded, err := s.upgradeExisting(ctx, me, tier, now)
	if err != nil {
		return stats, err
	}
	stats.Upgraded = upgraded

	cands, err := s.Repo.FetchCandidates(ctx, repo.CandidateQuery{
		MeID:           me.UserID,
		MyGender:       me.Gender,
		MyBirthYM:      me.BirthYM,
		MyCityCode:     me.CityCode,
		PrefBirthYMMin: prefBirthYMMin(me.Pref),
		PrefBirthYMMax: prefBirthYMMax(me.Pref),
		CityCodes:      prefCityCodes(me.Pref),
		Limit:          s.Cfg.Match.ScanLimit,
	})
	if err != nil {
		return stats, err
	}

	// 打分。双向都算，因为成对要求两个方向都过线。
	scored := make([]scoredPair, 0, len(cands))
	for _, c := range cands {
		other := toSubject(c)

		// 硬条件在打分之前：不合的组合连分都不该算，
		// 省得日志里混进一堆「高分但被淘汰」的噪声
		if !PassesHardConditions(me, other) {
			stats.Skipped++
			continue
		}

		p := scoredPair{
			UserID: other.UserID,
			Out:    Score(me, other, now),
			In:     Score(other, me, now),
		}
		if !qualifies(p, tier) {
			stats.Skipped++
			continue
		}
		scored = append(scored, p)
	}

	if len(scored) == 0 {
		return stats, nil
	}

	// 排序：成对的一律排在单向之前，同档内按「决定这段关系的那个分数」降序。
	// 用同一个键排序会让成对的高分组合被单向的极端分挤掉。
	sort.SliceStable(scored, func(i, j int) bool {
		return rankOf(scored[i], tier) > rankOf(scored[j], tier)
	})

	limit := s.Cfg.Intro.BatchSize
	if len(scored) > limit {
		scored = scored[:limit]
	}

	batchID := uuid.NewString()
	expires := now.Add(introTTL)

	for _, p := range scored {
		in, ok := s.buildInsert(me.UserID, p, tier, batchID, expires)
		if !ok {
			continue
		}
		created, err := s.insertIntro(ctx, in, me.UserID, p.UserID)
		if err != nil {
			return stats, err
		}
		if !created {
			stats.Duplicates++
			continue
		}
		if in.Kind == model.IntroPaired {
			stats.Paired++
		} else {
			stats.Oneway++
		}
	}

	return stats, nil
}

// buildInsert 把一对算好的分数翻成一行待插入的引荐。
//
// 第二个返回值是「这一对由我这一轮来生成吗」。为 false 时跳过 ——
// 见下面单向分支里的说明。
func (s *Service) buildInsert(meID int64, p scoredPair, tier Tier, batchID string, expires time.Time) (repo.IntroInsert, bool) {
	low, high := orderPair(meID, p.UserID)
	threshold := tier.Threshold

	// 分数按 low/high 的方向存，不按「我/他」——
	// 同一对无论谁那一轮先处理，落库的两列必须是同一个方向，
	// 否则后续调参时读到的分数会是反的。
	var sLowToHigh, sHighToLow float64
	if meID == low {
		sLowToHigh, sHighToLow = p.Out, p.In
	} else {
		sLowToHigh, sHighToLow = p.In, p.Out
	}

	in := repo.IntroInsert{
		BatchID:        batchID,
		UserLow:        low,
		UserHigh:       high,
		ExpiresAt:      expires,
		ScoreLowToHigh: &sLowToHigh,
		ScoreHighToLow: &sHighToLow,
	}

	if p.bothPass(threshold) {
		in.Kind = model.IntroPaired
		// 成对没有隐藏方，CHECK 约束 intro_hidden_kind_chk 也要求如此
		return in, true
	}

	// 单向：递出给「分数过线的那一方」，也就是我（qualifies 已经保证
	// 过线的一定是 Out）。被藏起来的是对方 —— §4.4「对方分数过线即可
	// 递出，对方不收到通知」，§13.3 的场景「A 收到一条 B 的单向引荐
	// （B 不知道）」都是这个意思。
	//
	// 反过来的情形（他对我过线、我对他没过线）在这里必然为 false，
	// 因为 qualifies 只放行 Out 过线的组合 —— 那一条该由他自己那一轮
	// 生成，我这一轮不抢着建，免得两边各建一条相反的。
	hidden := hiddenSideOf(meID, low, high)
	in.Kind = model.IntroOneway
	in.HiddenSide = &hidden
	return in, true
}

// insertIntro 在一个事务里落库并写通知。
//
// 引荐和它的通知必须同事务：否则进程在两者之间挂掉，用户会有一条
// 永远等不到推送的引荐 —— 而推送是唯一入口，他不会主动来翻列表
// （§4.5：没有推送就只有站内角标）。
func (s *Service) insertIntro(ctx context.Context, in repo.IntroInsert, meID, otherID int64) (bool, error) {
	created := false
	err := s.Repo.Tx(func(tx *gorm.DB) error {
		id, ok, err := s.Repo.InsertIntroduction(tx, &in)
		if err != nil || !ok {
			return err
		}
		created = true

		// 成对：双方各一行通知。
		// 单向：只有可见方那一行 —— 隐藏方连它存在都不该知道。
		for _, uid := range recipientsOf(meID, otherID, in) {
			key := introDeliveredKey(id, sideOf(uid, in.UserLow, in.UserHigh))
			if err := s.EnqueueNotify(tx, uid, model.TplIntroDelivered, key, map[string]any{
				"intro_id": id,
				"kind":     in.Kind,
				// batch_id 让投递端把同一批的几条合成一条通知：
				// 一次生成最多 3 条，各发一条就是连震三下（§4.4
				// 「一次推送携带 1–3 个人」）。库里仍然是每条一行 ——
				// 幂等要按条算，而打扰要按批算。
				"batch_id": in.BatchID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return created, err
}

// recipientsOf 列出这条引荐该通知谁。
func recipientsOf(meID, otherID int64, in repo.IntroInsert) []int64 {
	if in.Kind == model.IntroPaired {
		// 决策 13：成对是同时递出、同时通知
		return []int64{in.UserLow, in.UserHigh}
	}
	// 单向：只通知可见方
	if in.HiddenSide != nil && *in.HiddenSide == sideOf(meID, in.UserLow, in.UserHigh) {
		return []int64{otherID}
	}
	return []int64{meID}
}

// upgradeExisting 把「我不知情、但对手方已经收到过」的单向引荐升级成成对（§13.3）。
//
// 为什么需要这一步：这类引荐的对手方被候选集 SQL 的排重条件挡住了
// （同一对已有未终结引荐），所以打分那一段根本看不到他。不单独捞出来
// 重算，这条引荐只能干等超时 —— 而它本来可能已经该成立匹配了。
//
// 触发条件是「我现在对他也过线了」。分数会自己变：档案填得更全、
// 活跃度随时间衰减，都会让分数漂移，所以这一步不是白跑。
func (s *Service) upgradeExisting(ctx context.Context, me Subject, tier Tier, now time.Time) (int, error) {
	rows, err := s.Repo.OpenOnewayHiddenFrom(ctx, me.UserID)
	if err != nil || len(rows) == 0 {
		return 0, err
	}

	others, err := s.Repo.LoadSelves(ctx, otherIDs(rows))
	if err != nil {
		return 0, err
	}
	byID := make(map[int64]Subject, len(others))
	for _, o := range others {
		byID[o.UserID] = toSubject(o)
	}

	upgraded := 0
	for _, r := range rows {
		other, ok := byID[r.OtherID]
		if !ok {
			continue // 对手方已经不可用（被删号或状态变了）
		}
		// 硬条件要重判一次：档案改了之后可能已经不合了，
		// 那时候不该升级，让它自然超时即可。
		if !PassesHardConditions(me, other) {
			continue
		}
		// 我→他过线即可升级：他→我当初就是因为过线才建出这条引荐的，
		// 而且只要这条引荐还没被终结，那个方向就仍然成立。
		if Score(me, other, now) < tier.Threshold {
			continue
		}

		ok, err := s.upgradeOne(ctx, r.IntroID, r.OtherID, me.UserID, now)
		if err != nil {
			return upgraded, err
		}
		if ok {
			upgraded++
		}
	}
	return upgraded, nil
}

// upgradeOne 升级一条，并在同一事务里通知「刚刚才第一次看到它」的那一方。
func (s *Service) upgradeOne(ctx context.Context, introID, otherID, meID int64, now time.Time) (bool, error) {
	done := false
	err := s.Repo.Tx(func(tx *gorm.DB) error {
		// §13.3 明确：升级后把被藏起来那一侧的时限重算为「从现在起 72 小时」——
		// 他此刻才第一次看到这条引荐，可见方此前的等待被完整保留。
		ok, err := s.Repo.UpgradeToPaired(tx, introID, now.Add(introTTL))
		if err != nil || !ok {
			// RowsAffected == 0：另一个实例刚升级过了。
			// 不重复发通知，静默返回（与处理 matches 唯一冲突同一手法）。
			return err
		}
		done = true

		// 只通知我自己：可见方早就收到过他那条通知了。
		low, high := orderPair(meID, otherID)
		key := introDeliveredKey(introID, sideOf(meID, low, high))
		return s.EnqueueNotify(tx, meID, model.TplIntroDelivered, key, map[string]any{
			"intro_id": introID,
			"kind":     model.IntroPaired,
			"upgraded": true,
		})
	})
	return done, err
}

// ------------------------------------------------------------- 小工具

// qualifies 判一个组合能不能生成。
//
// 成对优先（决策 13）；池子不足时才允许单向，且单向只认「我对他过线」——
// 因为递出对象就是过线的那一方。允许单向的档位由 §12.4 的阶梯决定。
func qualifies(p scoredPair, tier Tier) bool {
	if p.bothPass(tier.Threshold) {
		return true
	}
	return tier.AllowOneway && p.Out >= tier.Threshold
}

// rankOf 是排序键。成对整体加 2、单向加 1，保证成对永远排在前面；
// 档内分别用「较短的那块板」和「过线的那个分数」比高下。
func rankOf(p scoredPair, tier Tier) float64 {
	if p.bothPass(tier.Threshold) {
		return 2 + math.Min(p.Out, p.In)
	}
	return 1 + p.Out
}

// orderPair 把两个人归一成 low < high。
// introductions 的 CHECK 约束 intro_order_chk 要求这个顺序。
func orderPair(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func otherIDs(rows []struct {
	IntroID int64 `gorm:"column:intro_id"`
	OtherID int64 `gorm:"column:other_id"`
}) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.OtherID)
	}
	return ids
}

// toSubject 把 repo 的行翻成打分子。
//
// Preference 只在真有一行时才构造：PrefExists 为假时给 nil，
// 而 nil 在打分器里表示「从没填过偏好」，与「填了但全是空」等价。
// 之所以还区分，是因为候选集 SQL 用 LEFT JOIN，两种情况的列形态不同，
// 混着读会让「没填过」被当成「填了空值」。
func toSubject(r repo.CandidateRow) Subject {
	s := Subject{
		UserID:         r.UserID,
		Gender:         r.Gender,
		BirthYM:        r.BirthYM,
		CityCode:       r.CityCode,
		HometownCode:   r.HometownCode,
		HeightCM:       r.HeightCM,
		EducationLevel: r.EduLevel,
		IncomeBand:     r.IncomeBand,
		Chronotype:     r.Chronotype,
		Smoking:        r.Smoking,
		Drinking:       r.Drinking,
		WantChild:      r.WantChild,
		MaritalStatus:  r.Marital,
		LastActiveAt:   r.LastActiveAt,
		CreatedAt:      r.CreatedAt,
	}
	if r.PrefExists {
		s.Pref = &model.Preference{
			UserID:         r.UserID,
			BirthYMMin:     r.PrefBirthYMMin,
			BirthYMMax:     r.PrefBirthYMMax,
			CityCodes:      r.PrefCityCodes,
			WantChild:      r.PrefWantChild,
			AcceptDivorced: r.PrefDivorced,
			AcceptRemote:   r.PrefRemote,
			EduMin:         r.PrefEduMin,
			HeightMin:      r.PrefHeightMin,
			HeightMax:      r.PrefHeightMax,
			IncomeMin:      r.PrefIncomeMin,
			IncomeMax:      r.PrefIncomeMax,
		}
	}
	return s
}

// prefBirthYMMin / Max 和 prefCityCodes 三个取值小工具。
// 偏好为 nil（从没填过）时返回 nil，等价于 SQL 里的「不限」。
func prefBirthYMMin(p *model.Preference) *int {
	if p == nil {
		return nil
	}
	return p.BirthYMMin
}

func prefBirthYMMax(p *model.Preference) *int {
	if p == nil {
		return nil
	}
	return p.BirthYMMax
}

func prefCityCodes(p *model.Preference) []int64 {
	if p == nil {
		return nil
	}
	return p.CityCodes
}

// ------------------------------------------------------------ 引荐列表

// 读侧。与生成侧共用同一个文件，因为「引荐」这一件事的状态机
// 两头都要看：生成时决定它长什么样，读出来时决定它显示成什么样。
// 分成两个文件的话，改一个状态取值要记得同时改两处。

// IntroView 是引荐列表里的一条，形状按引荐卡来定（§19.2）。
//
// 对方的字段直接给编号（city_code / education_level / income_band），
// 不在这里翻成中文：档案接口也是这么给的，前端已经有一套字典
// （web/src/lib/dict.ts）。在两个地方各翻一次，就会出现
// 「档案页写硕士、引荐卡写研究生」这种不一致。
type IntroView struct {
	ID      int64  `json:"id"`
	IssueNo int64  `json:"issue_no"` // 卡片上那行「引荐 · 第 12 期」
	Kind    string `json:"kind"`
	State   string `json:"state"`

	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// Viewed 与 MyAction 是「我这一侧」的状态。对方的表态不出现 ——
	// 界面要的「等对方回音」由 State 表达，把对方的具体动作透出去
	// 等于让他能看着对方犹豫，这不是这个产品想给的体验。
	Viewed   bool    `json:"viewed"`
	MyAction *string `json:"my_action"`

	Other IntroPerson `json:"other"`
}

// IntroPerson 是引荐卡上的那个人。
//
// 这里是一个白名单，不是 profiles 的一行：给出去多少字段是产品决策。
// 学校名、公司、体重、作息这几项在卡上不出现（§19.2 只画了
// 昵称/出生年/城市/学历/身高/收入/自述），所以它们也不该出现在响应里 ——
// 前端不显示不等于没泄露，抓包一样能看到。
type IntroPerson struct {
	UserID         int64   `json:"user_id"`
	Nickname       *string `json:"nickname"`
	BirthYM        *int    `json:"birth_ym"`
	CityCode       *int    `json:"city_code"`
	EducationLevel *int16  `json:"education_level"`
	HeightCM       *int16  `json:"height_cm"`
	IncomeBand     *int16  `json:"income_band"`
	Intro          string  `json:"intro"`

	// Occupation 只有详情页给（§4.6 的展示清单里有职业，卡片草图上没有）。
	// omitempty 是为了让列表响应里根本不出现这个键 —— 值是空串时
	// 前端会显示成一个空标签，而不出现和「没填」是两回事。
	Occupation *string `json:"occupation,omitempty"`

	// CoverURL 用 card 档（800w）—— 卡片上是封面图，不是缩略图，
	// 也不是原图。空串表示对方还没有照片，前端显示占位。
	CoverURL string `json:"cover_url"`
}

// IntroListView 是 GET /me/introductions 的响应。
//
// 用对象而不是裸数组，是为了带上 Unread：没有推送权限的人
// （iOS 上没装到主屏幕，§19.5）靠站内角标兜底，而角标要的数字
// 不该让前端把列表拉下来自己数 —— 那要求列表是全量的，
// 一页页翻的时候就数错了。
type IntroListView struct {
	Introductions []IntroView `json:"introductions"`
	Unread        int         `json:"unread"`
}

// ListIntroductions 读本人的引荐列表。
func (s *Service) ListIntroductions(ctx context.Context, user *model.User) (*IntroListView, error) {
	rows, err := s.Repo.ListIntroductions(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	// make(...,0) 而不是 var：空列表要出 []，出 null 会让前端多一个分支
	out := make([]IntroView, 0, len(rows))
	unread := 0
	for _, r := range rows {
		if r.Unread {
			unread++
		}
		out = append(out, s.toIntroView(r))
	}
	return &IntroListView{Introductions: out, Unread: unread}, nil
}

func (s *Service) toIntroView(r repo.IntroRow) IntroView {
	cover := ""
	if r.CoverKey != nil {
		cover = s.ImgURL(*r.CoverKey, storage.VariantCard)
	}

	return IntroView{
		ID:        r.ID,
		IssueNo:   r.IssueNo,
		Kind:      r.Kind,
		State:     r.State,
		CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		Viewed:    r.MyViewedAt != nil,
		MyAction:  r.MyAction,
		Other: IntroPerson{
			UserID:         r.OtherID,
			Nickname:       r.Nickname,
			BirthYM:        r.BirthYM,
			CityCode:       r.CityCode,
			EducationLevel: r.EducationLevel,
			HeightCM:       r.HeightCM,
			IncomeBand:     r.IncomeBand,
			Intro:          r.Intro,
			CoverURL:       cover,
		},
	}
}

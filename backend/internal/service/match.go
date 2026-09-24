package service

import (
	"math"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// 匹配引擎。MVP 是规则引擎，不是 AI —— 它要足够简单到能被人读懂和手调，
// 因为 MVP 期间唯一的优化对象就是它（§12）。
//
// 本文件里的打分函数全是纯函数：不碰数据库、不读时钟（now 一律当参数传）。
// 这样阈值和权重能直接跑单元测试，调参时不用起数据库。
// 取数据和落库在 introduction.go / repo 里。

// 打分权重。合计必须为 1.00 —— match_test.go 里有一条断言守着它。
//
// 设计稿正文写「六个因子」，但表里只有五个，权重正好加到 1.00。
// 按表实现，正文那句是笔误。
const (
	// wReverseMatch 反向匹配度：我满足对方偏好的比例。
	// 独占 0.40 是这套引擎最重要的取舍 —— 它问的不是「这个人好不好」，
	// 而是「这个人会不会回应我」。推一个我看得上但看不上我的人，
	// 浪费的是一次推送机会和一次期待。
	wReverseMatch = 0.40
	// wForwardMatch 正向匹配度：对方满足我偏好的比例。
	wForwardMatch = 0.25
	// wGeo 地域接近。
	wGeo = 0.15
	// wLifestyle 生活方式相容。
	wLifestyle = 0.10
	// wActivity 活跃度。7 天时间常数。
	wActivity = 0.10
)

// 地域分的取值。
const (
	geoSameCity     = 1.0
	geoSameProvince = 0.5
	geoSameHometown = 0.2
)

// 活跃度的时间常数（天）。
const activityTau = 7.0

// 生活方式相容的维度数：作息、吸烟、饮酒。
const lifestyleDims = 3

// Subject 是打分要用到的一方：档案 + 他自己声明的偏好。
//
// 刻意不用 model.Profile —— 候选集 SQL 只取打分需要的列，
// 用整表会让「哪些字段真被用到了」变得看不出来。
type Subject struct {
	UserID   int64
	Gender   string
	BirthYM  int
	CityCode int

	HometownCode   *int
	HeightCM       *int16
	EducationLevel *int16
	IncomeBand     *int16
	Chronotype     *int16
	Smoking        *int16
	Drinking       *int16

	// 硬条件两项（自己的情况）
	WantChild     *int16
	MaritalStatus *int16

	LastActiveAt *time.Time
	CreatedAt    time.Time

	// Pref 为 nil 表示这个人从没填过偏好，等价于全部「不限」。
	Pref *model.Preference
}

// Score 算 a 对 b 的分数，值域 0–1。
//
// 注意方向：返回的是「a 对 b 有多满意」，不是两个人的契合度。
// 成对匹配要算两次（a→b 与 b→a）并分别过阈值。
func Score(a, b Subject, now time.Time) float64 {
	s := wReverseMatch*matchRatio(b, a) + // a 满足 b 声明的偏好
		wForwardMatch*matchRatio(a, b) + // b 满足 a 声明的偏好
		wGeo*geoAffinity(a, b) +
		wLifestyle*lifestyleAffinity(a, b) +
		wActivity*activityScore(b, now)

	// 浮点累加可能到 1.0000000000000002，夹一下免得阈值判定出怪事
	return math.Min(1.0, math.Max(0.0, s))
}

// matchRatio 算 cand 满足了 seeker 声明的软偏好里的几项，返回 0–1。
//
// 三个维度：学历下限、身高区间、收入区间。「不限」的维度不计入分母 ——
// 这就是 §4.3 说的「存储为空 → 打分时该维度不计入分母 → 界面显示不限」
// 三处协同的中间那处。seeker 一个维度都没声明 → 1.0（不是 0）：
// 没提要求的人不该因此被扣分。
//
// 判不了的一律算「不满足」：seeker 声明了学历下限，而 cand 没填学历，
// 这一项就落在分母里而不在分子里。反过来（把没填的也算满足）会让
// 档案填得少的人占便宜，与「填了才有机会」的立场相反。
func matchRatio(seeker, cand Subject) float64 {
	p := seeker.Pref
	if p == nil {
		return 1.0
	}

	var declared, satisfied int

	if p.EduMin != nil {
		declared++
		if cand.EducationLevel != nil && *cand.EducationLevel >= *p.EduMin {
			satisfied++
		}
	}
	if p.HeightMin != nil || p.HeightMax != nil {
		declared++
		if inRange16(cand.HeightCM, p.HeightMin, p.HeightMax) {
			satisfied++
		}
	}
	if p.IncomeMin != nil || p.IncomeMax != nil {
		declared++
		if inRange16(cand.IncomeBand, p.IncomeMin, p.IncomeMax) {
			satisfied++
		}
	}

	if declared == 0 {
		return 1.0
	}
	return float64(satisfied) / float64(declared)
}

// inRange16 判 v 是否落在 [lo, hi] 内，两端各自可以为空（表示这一端不限）。
// v 为空一律算不满足。
func inRange16(v, lo, hi *int16) bool {
	if v == nil {
		return false
	}
	if lo != nil && *v < *lo {
		return false
	}
	if hi != nil && *v > *hi {
		return false
	}
	return true
}

// geoAffinity 地域接近：同城 1.0 / 同省 0.5 / 否则 0，同家乡再加 0.2，上限 1.0。
//
// 省份用 city_code / 10000 —— 国标 6 位码的前两位就是省级。
//
// 同家乡按 hometown_code 完全相同算，不放宽到同省。设计稿只说了
// 「同家乡 0.2」，没给省份换算，字面读就是同一个地方。
// 如果实测发现老乡的加分几乎不触发（大家都只填到市），
// 把下面的 sameHometown 改成比省份即可，是个一行改动。
func geoAffinity(a, b Subject) float64 {
	var base float64
	switch {
	case a.CityCode != 0 && a.CityCode == b.CityCode:
		base = geoSameCity
	case a.CityCode != 0 && provinceOf(a.CityCode) == provinceOf(b.CityCode):
		base = geoSameProvince
	}
	if sameHometown(a, b) {
		base += geoSameHometown
	}
	return math.Min(1.0, base)
}

func provinceOf(cityCode int) int { return cityCode / 10000 }

func sameHometown(a, b Subject) bool {
	return a.HometownCode != nil && b.HometownCode != nil && *a.HometownCode == *b.HometownCode
}

// lifestyleAffinity 生活方式相容：作息、吸烟、饮酒三项里相同的比例。
//
// 任一方未填的那一项不计入分母。三项都没得比时返回 1.0 ——
// 与 matchRatio 的「没声明就满分」同一个立场：不因为没填而扣分。
func lifestyleAffinity(a, b Subject) float64 {
	same, comparable := 0, 0

	compare := func(x, y *int16) {
		if x == nil || y == nil {
			return
		}
		comparable++
		if *x == *y {
			same++
		}
	}
	compare(a.Chronotype, b.Chronotype)
	compare(a.Smoking, b.Smoking)
	compare(a.Drinking, b.Drinking)

	if comparable == 0 {
		return 1.0
	}
	return float64(same) / float64(comparable)
}

// activityScore 活跃度 exp(-Δ天 / 7)。Δ 取 now() - coalesce(last_active_at, created_at)。
//
// 时钟回拨导致 Δ 为负时按 0 处理：否则 exp 会大于 1，把分数顶出去。
func activityScore(b Subject, now time.Time) float64 {
	last := b.CreatedAt
	if b.LastActiveAt != nil {
		last = *b.LastActiveAt
	}
	days := now.Sub(last).Hours() / 24
	if days < 0 {
		days = 0
	}
	return math.Exp(-days / activityTau)
}

// PassesHardConditions 是 §12.1 的第二步：硬条件过滤，任一不合直接淘汰，不打分。
//
// 年龄区间和城市已经在候选集 SQL 里筛过一遍了（那边能用上索引），
// 这里补的是没法用索引、也不适合塞进那条 SQL 的三项：
//
//	婚育意愿  —— 对方得满足我声明的意愿；「想要」撞上「不要」是硬冲突
//	婚史      —— 我不接受有婚史，而对方离异，就淘汰
//	异地      —— 不在同一个城市时，要求双方都接受异地
//
// 三项都是「我提的要求」，所以全部双向各判一次：A 接受不代表 B 接受。
// 任一方的该项未填都按「不限」放行，与 preferences 的「不限」约定一致。
func PassesHardConditions(a, b Subject) bool {
	return satisfiesWantChild(a, b) && satisfiesWantChild(b, a) &&
		acceptsMarital(a, b) && acceptsMarital(b, a) &&
		acceptsRemote(a, b) && acceptsRemote(b, a)
}

// satisfiesWantChild 判 other 的婚育意愿是否满足 me 声明的期望。
//
// 读的是 preferences.want_child（我要求对方怎样）对着 profiles.want_child
// （对方自己怎样）比 —— 与婚史、异地同一套路，不要退化成拿两份档案直接对撞：
// 那样 preferences 侧的 want_child 就成了一个存了却没人看的列。
//
// 「再说」不是拒绝，与两种明确意愿都相容；只有「想要」撞上「不要」才淘汰。
func satisfiesWantChild(me, other Subject) bool {
	if me.Pref == nil || me.Pref.WantChild == nil {
		return true
	}
	if other.WantChild == nil {
		return true // 对方没填，按不限
	}
	wanted, actual := *me.Pref.WantChild, *other.WantChild
	if wanted == actual {
		return true
	}
	return wanted == model.WantChildTBD || actual == model.WantChildTBD
}

// acceptsMarital 判 me 接不接受 other 的婚史。
// accept_divorced 只在 preferences 侧：1 接受 / 0 不接受 / 未填不限。
func acceptsMarital(me, other Subject) bool {
	if me.Pref == nil || me.Pref.AcceptDivorced == nil {
		return true
	}
	if *me.Pref.AcceptDivorced != 0 {
		return true
	}
	// 明确说了不接受，那对方得是未婚才算过 —— 对方未填婚史时按不限放行
	return other.MaritalStatus == nil || *other.MaritalStatus != model.MaritalDivorced
}

// acceptsRemote 判 me 接不接受与 other 异地。
//
// 取值来自 preferences.accept_remote（不是档案上的字段）：异地接受度是
// 「我对关系形态的要求」，与「我未婚」这类关于自己的事实不同，
// 所以只存在偏好侧一份。
//
// 同城直接过 —— 异地条款不该管到同城的人头上。
func acceptsRemote(me, other Subject) bool {
	if me.CityCode == 0 || other.CityCode == 0 || me.CityCode == other.CityCode {
		return true
	}
	if me.Pref == nil || me.Pref.AcceptRemote == nil {
		return true
	}
	return *me.Pref.AcceptRemote == model.RemoteAccept
}

// ------------------------------------------------------------- 池子阶梯

// 池子阶梯的分档线（§12.4）。低于 PoolMin 时暂停自动引荐。
const (
	poolStrictMax  = 2000 // ≥ 此值：严格成对
	poolRelaxedMax = 500  // 500–2000：阈值下调 10%
	// 100–500：成对为主，成不了对时允许单向
	// < 100：暂停，绝不推不合标准的人填坑
)

// RelaxFactor 是第二档的阈值折扣。
const RelaxFactor = 0.9

// Tier 是本轮生成用的策略档位。
type Tier struct {
	PoolSize int
	// Threshold 是本轮实际用的阈值。
	Threshold float64
	// AllowOneway 为真时，只有一方过线也允许递出单向引荐。
	AllowOneway bool
	// TooSmall 为真时不生成任何引荐 —— 宁可让用户看到
	//「你所在城市当前可引荐的人不足」，也不推不合标准的人填坑。
	TooSmall bool
}

// TierFor 按当前可用池人数决定本轮策略。池子规模每次生成任务开始时查一次。
//
// poolMin 由配置传入（INTRO_POOL_MIN，默认 100）而不是写成常量：
// 上线初期池子小，这个值要能临时调低去验证链路，改环境变量比改代码快。
func TierFor(poolSize int, baseThreshold float64, poolMin int) Tier {
	switch {
	case poolSize < poolMin:
		return Tier{PoolSize: poolSize, Threshold: baseThreshold, TooSmall: true}
	case poolSize < poolRelaxedMax:
		// 100–500：成对为主，成不了对时允许单向
		return Tier{PoolSize: poolSize, Threshold: baseThreshold, AllowOneway: true}
	case poolSize < poolStrictMax:
		return Tier{PoolSize: poolSize, Threshold: baseThreshold * RelaxFactor}
	default:
		return Tier{PoolSize: poolSize, Threshold: baseThreshold}
	}
}

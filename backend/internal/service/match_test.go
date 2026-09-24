package service

import (
	"math"
	"testing"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
)

// str / num / i16 三个指针小工具在 profile_test.go 里，同包直接用。
// int 的那个（intPtr）在生产代码里的 preference.go，两边共用一个。

// now 固定住，活跃度才可断言。
var testNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) *time.Time {
	t := testNow.Add(-time.Duration(d * 24 * float64(time.Hour)))
	return &t
}

// 权重合计必须是 1.00。改了权重忘了调另一个，分数会整体偏移，
// 而这种错不会报错、只会让阈值悄悄失准。
func TestWeightsSumToOne(t *testing.T) {
	sum := wReverseMatch + wForwardMatch + wGeo + wLifestyle + wActivity
	if math.Abs(sum-1.0) > 1e-9 {
		t.Fatalf("权重合计 %v，必须等于 1.00", sum)
	}
}

// 两个「什么都填了、处处合拍」的人，分数应该接近 1；
// 两个处处不合的人应该接近 0。这条守住的是量纲，不是某个因子。
func TestScoreBounds(t *testing.T) {
	pref := &model.Preference{
		EduMin: i16(2), HeightMin: i16(160), HeightMax: i16(180), IncomeMin: i16(3), IncomeMax: i16(6),
	}
	perfect := Subject{
		CityCode: 310000, HometownCode: intPtr(310000),
		HeightCM: i16(170), EducationLevel: i16(3), IncomeBand: i16(4),
		Chronotype: i16(1), Smoking: i16(0), Drinking: i16(0),
		LastActiveAt: daysAgo(0), CreatedAt: testNow,
		Pref: pref,
	}
	// 完全相同的两个人：三项软偏好互相满足、同城同乡、生活方式三项全同
	if got := Score(perfect, perfect, testNow); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("完全合拍的两个人应该得 1.0，得到 %v", got)
	}

	worst := Subject{
		CityCode: 110000, HometownCode: intPtr(440000),
		HeightCM: i16(150), EducationLevel: i16(1), IncomeBand: i16(1),
		Chronotype: i16(3), Smoking: i16(2), Drinking: i16(2),
		LastActiveAt: daysAgo(365), CreatedAt: testNow,
		Pref: &model.Preference{EduMin: i16(4), HeightMin: i16(180), IncomeMin: i16(6)},
	}

	// 四个因子全归零：软偏好两个方向都是 0/3、不同省不同乡、生活方式三项全不同。
	// 剩下的 0.10 全部来自活跃度 —— 那是「对方」的活跃度，perfect 今天刚活跃过，
	// 所以这 0.1 跟 worst 自己合不合拍无关，它拿定了。
	// 断言到小数点后 9 位，是为了让「某个因子偷偷漏算」这种事立刻暴露。
	if got := Score(worst, perfect, testNow); math.Abs(got-wActivity) > 1e-9 {
		t.Fatalf("处处不合应该只剩活跃度的 %v，得到 %v", wActivity, got)
	}

	// 把对方也换成一个沉寂一年的人，分数才应该真正见底
	stale := perfect
	stale.LastActiveAt = daysAgo(365)
	if got := Score(worst, stale, testNow); got > 1e-9 {
		t.Fatalf("双方处处不合且都已沉寂，应该 0，得到 %v", got)
	}
}

// 「不限」三处协同里打分那一处：一个维度都没声明的偏好必须返回 1.0，
// 不是 0 —— 没提要求的人不该因此被扣分。
func TestMatchRatioUnconstrainedIsFullCredit(t *testing.T) {
	cand := Subject{HeightCM: i16(170)}
	for name, pref := range map[string]*model.Preference{
		"没填过偏好": nil,
		"填了但全空": {},
	} {
		if got := matchRatio(Subject{Pref: pref}, cand); got != 1.0 {
			t.Errorf("%s：应该 1.0，得到 %v", name, got)
		}
	}
}

// 声明的维度才进分母。声明两项、满足两项 → 1.0；
// 声明三项、满足一项 → 1/3。
func TestMatchRatioDenominator(t *testing.T) {
	cand := Subject{EducationLevel: i16(3), HeightCM: i16(170), IncomeBand: i16(2)}

	twoDeclared := Subject{Pref: &model.Preference{EduMin: i16(2), HeightMin: i16(160), HeightMax: i16(180)}}
	if got := matchRatio(twoDeclared, cand); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("声明两项都满足，应该 1.0，得到 %v", got)
	}

	threeDeclared := Subject{Pref: &model.Preference{
		EduMin: i16(2), HeightMin: i16(160), HeightMax: i16(180), IncomeMin: i16(5),
	}}
	// 学历过、身高过、收入 2 < 5 不过 → 2/3
	if got := matchRatio(threeDeclared, cand); math.Abs(got-2.0/3.0) > 1e-9 {
		t.Errorf("三项里过两项，应该 2/3，得到 %v", got)
	}
}

// 判不了的算不满足：对方声明了学历下限，而我没填学历。
// 反过来会让档案填得少的人占便宜。
func TestMatchRatioMissingFieldCountsAgainst(t *testing.T) {
	seeker := Subject{Pref: &model.Preference{EduMin: i16(2), HeightMin: i16(160), HeightMax: i16(180)}}
	blank := Subject{} // 学历和身高都没填
	if got := matchRatio(seeker, blank); got != 0 {
		t.Fatalf("两项都判不了应该 0，得到 %v", got)
	}
}

func TestInRange16(t *testing.T) {
	cases := []struct {
		name      string
		v, lo, hi *int16
		want      bool
	}{
		{"开区间", i16(170), nil, nil, true},
		{"单边下限内", i16(170), i16(160), nil, true},
		{"单边下限外", i16(150), i16(160), nil, false},
		{"单边上限内", i16(170), nil, i16(180), true},
		{"单边上限外", i16(190), nil, i16(180), false},
		{"闭区间边界", i16(160), i16(160), i16(180), true},
		{"值为空", nil, i16(160), i16(180), false},
	}
	for _, c := range cases {
		if got := inRange16(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("%s: inRange16 = %v, want %v", c.name, got, c.want)
		}
	}
}

// 地域：同城 1.0、同省 0.5、都不是 0，同家乡各加 0.2，且封顶 1.0。
func TestGeoAffinity(t *testing.T) {
	cases := []struct {
		name string
		a, b Subject
		want float64
	}{
		{"同城异乡", Subject{CityCode: 310000}, Subject{CityCode: 310000}, 1.0},
		{"同城同乡，封顶 1.0", Subject{CityCode: 310000, HometownCode: intPtr(310000)},
			Subject{CityCode: 310000, HometownCode: intPtr(310000)}, 1.0},
		{"同省不同市", Subject{CityCode: 320100}, Subject{CityCode: 321000}, 0.5},
		{"不同省", Subject{CityCode: 320100}, Subject{CityCode: 110000}, 0.0},
		{"异地但同乡", Subject{CityCode: 110000, HometownCode: intPtr(320100)},
			Subject{CityCode: 310000, HometownCode: intPtr(320100)}, 0.2},
		{"同省且同乡", Subject{CityCode: 320100, HometownCode: intPtr(320100)},
			Subject{CityCode: 321000, HometownCode: intPtr(320100)}, 0.7},
	}
	for _, c := range cases {
		if got := geoAffinity(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: geoAffinity = %v, want %v", c.name, got, c.want)
		}
	}
}

// 生活方式：任一方未填的那一项不进分母。
func TestLifestyleAffinity(t *testing.T) {
	// 三项全填且全同
	full := Subject{Chronotype: i16(1), Smoking: i16(0), Drinking: i16(0)}
	if got := lifestyleAffinity(full, full); got != 1.0 {
		t.Errorf("三项全同应该 1.0，得到 %v", got)
	}

	// 只有作息可比，且不同 → 0/1
	onlyChronoA := Subject{Chronotype: i16(1)}
	onlyChronoB := Subject{Chronotype: i16(2), Smoking: i16(0)}
	if got := lifestyleAffinity(onlyChronoA, onlyChronoB); got != 0 {
		t.Errorf("唯一可比的作息不同，应该 0，得到 %v", got)
	}

	// 三项都没得比 → 1.0（不因为没填而扣分）
	if got := lifestyleAffinity(Subject{}, Subject{}); got != 1.0 {
		t.Errorf("没有可比项应该 1.0，得到 %v", got)
	}

	// 一项可比且相同 → 1.0
	if got := lifestyleAffinity(Subject{Smoking: i16(1)}, Subject{Smoking: i16(1)}); got != 1.0 {
		t.Errorf("只有吸烟可比且相同，应该 1.0，得到 %v", got)
	}
}

// 活跃度：7 天时间常数。
func TestActivityScore(t *testing.T) {
	cases := []struct {
		name string
		days float64
		want float64
	}{
		{"今天活跃", 0, 1.0},
		{"7 天前", 7, math.Exp(-1)},
		{"30 天前", 30, math.Exp(-30.0 / 7.0)},
	}
	for _, c := range cases {
		s := Subject{LastActiveAt: daysAgo(c.days), CreatedAt: testNow}
		if got := activityScore(s, testNow); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: activityScore = %v, want %v", c.name, got, c.want)
		}
	}

	// 时钟回拨：Δ 为负时按 0 处理，否则 exp 会大于 1 把分数顶出去
	future := Subject{LastActiveAt: daysAgo(-3), CreatedAt: testNow}
	if got := activityScore(future, testNow); got != 1.0 {
		t.Errorf("时钟回拨应该夹到 1.0，得到 %v", got)
	}

	// 从没活跃过就退回创建时间
	never := Subject{CreatedAt: testNow.Add(-14 * 24 * time.Hour)}
	if got := activityScore(never, testNow); math.Abs(got-math.Exp(-2)) > 1e-9 {
		t.Errorf("退回 created_at 应该 exp(-2)，得到 %v", got)
	}
}

// ---------------------------------------------------------- 硬条件过滤

// 读的是 preferences 侧对着 profiles 侧比，不是两份档案对撞。
func TestSatisfiesWantChild(t *testing.T) {
	// me 声明「我希望对方想要孩子」
	me := func(want *int16) Subject {
		if want == nil {
			return Subject{}
		}
		return Subject{Pref: &model.Preference{WantChild: want}}
	}

	cases := []struct {
		name       string
		want       *int16 // 我要求的
		actual     *int16 // 对方档案里的
		compatible bool
	}{
		{"我没提要求", nil, i16(model.WantChildNo), true},
		{"我没提要求，对方也没填", nil, nil, true},
		{"我要求想要，对方没填", i16(model.WantChildYes), nil, true},
		{"我要求想要，对方也想要", i16(model.WantChildYes), i16(model.WantChildYes), true},
		{"我要求想要，对方不要", i16(model.WantChildYes), i16(model.WantChildNo), false},
		{"我要求不要，对方想要", i16(model.WantChildNo), i16(model.WantChildYes), false},
		{"我要求想要，对方再说", i16(model.WantChildYes), i16(model.WantChildTBD), true},
		{"我要求不要，对方再说", i16(model.WantChildNo), i16(model.WantChildTBD), true},
		{"我要求再说，对方想要", i16(model.WantChildTBD), i16(model.WantChildYes), true},
		{"我要求再说，对方不要", i16(model.WantChildTBD), i16(model.WantChildNo), true},
	}
	for _, c := range cases {
		got := satisfiesWantChild(me(c.want), Subject{WantChild: c.actual})
		if got != c.compatible {
			t.Errorf("%s: = %v, want %v", c.name, got, c.compatible)
		}
	}
}

// 我不接受有婚史、对方离异 → 淘汰；反过来也要对称生效。
func TestAcceptsMarital(t *testing.T) {
	refuse := Subject{Pref: &model.Preference{AcceptDivorced: i16(0)}}
	accept := Subject{Pref: &model.Preference{AcceptDivorced: i16(1)}}
	divorced := Subject{MaritalStatus: i16(model.MaritalDivorced)}
	single := Subject{MaritalStatus: i16(model.MaritalSingle)}
	unknown := Subject{}

	if !acceptsMarital(refuse, single) {
		t.Error("不接受婚史，但对方未婚 —— 应该放行")
	}
	if acceptsMarital(refuse, divorced) {
		t.Error("不接受婚史，对方离异 —— 应该淘汰")
	}
	if !acceptsMarital(refuse, unknown) {
		t.Error("对方婚史未填按不限 —— 应该放行")
	}
	if !acceptsMarital(accept, divorced) {
		t.Error("接受有婚史 —— 应该放行")
	}
	if !acceptsMarital(unknown, divorced) {
		t.Error("没填偏好等于不限 —— 应该放行")
	}
}

// 异地条款只管异地，不该管到同城的人头上。
// 取值来自 preferences 侧，不是档案字段。
func TestAcceptsRemote(t *testing.T) {
	pref := func(v int16) *model.Preference { return &model.Preference{AcceptRemote: i16(v)} }

	refuseRemote := Subject{CityCode: 110000, Pref: pref(model.RemoteNotAccept)}
	sameCity := Subject{CityCode: 110000}
	otherCity := Subject{CityCode: 310000}
	acceptRemote := Subject{CityCode: 110000, Pref: pref(model.RemoteAccept)}

	if !acceptsRemote(refuseRemote, sameCity) {
		t.Error("同城不该受异地条款约束")
	}
	if acceptsRemote(refuseRemote, otherCity) {
		t.Error("不接受异地，对方在别的城市 —— 应该淘汰")
	}
	if !acceptsRemote(acceptRemote, otherCity) {
		t.Error("接受异地 —— 应该放行")
	}
	if !acceptsRemote(Subject{CityCode: 110000}, otherCity) {
		t.Error("没填偏好等于不限 —— 应该放行")
	}
	if !acceptsRemote(Subject{CityCode: 110000, Pref: &model.Preference{}}, otherCity) {
		t.Error("有偏好行但异地一栏为空，同样是「不限」—— 应该放行")
	}
}

// PassesHardConditions 要双向判：A 接受不代表 B 接受。
func TestPassesHardConditionsIsSymmetric(t *testing.T) {
	a := Subject{CityCode: 110000, Pref: &model.Preference{AcceptRemote: i16(model.RemoteAccept)}}
	b := Subject{CityCode: 310000, Pref: &model.Preference{AcceptRemote: i16(model.RemoteNotAccept)}}

	if PassesHardConditions(a, b) {
		t.Fatal("b 不接受异地，a→b 方向应该被淘汰")
	}
	if PassesHardConditions(b, a) {
		t.Fatal("方向反过来也该被淘汰 —— 两边都要判")
	}
}

// ------------------------------------------------------------- 池子阶梯

func TestTierFor(t *testing.T) {
	const base = 0.55
	cases := []struct {
		name         string
		pool         int
		wantTooSmall bool
		wantOneway   bool
		wantThresh   float64
	}{
		{"池子太小，不生成", 99, true, false, base},
		{"刚够 100，允许单向", 100, false, true, base},
		{"499 仍允许单向", 499, false, true, base},
		{"500 起回到成对，阈值打九折", 500, false, false, base * RelaxFactor},
		{"1999 仍打折", 1999, false, false, base * RelaxFactor},
		{"2000 起严格成对", 2000, false, false, base},
		{"大池子", 50000, false, false, base},
	}
	for _, c := range cases {
		got := TierFor(c.pool, base, 100)
		if got.TooSmall != c.wantTooSmall || got.AllowOneway != c.wantOneway ||
			math.Abs(got.Threshold-c.wantThresh) > 1e-9 {
			t.Errorf("%s: TierFor(%d) = %+v, want {tooSmall:%v oneway:%v thresh:%v}",
				c.name, c.pool, got, c.wantTooSmall, c.wantOneway, c.wantThresh)
		}
	}
}

// 阈值折扣后仍要能分出高下：0.55 打九折是 0.495，
// 一个 0.52 分的组合在严格档被淘汰、在放宽松档应该能过。
func TestRelaxedTierAdmitsBorderline(t *testing.T) {
	strict := TierFor(3000, 0.55, 100)
	relaxed := TierFor(1000, 0.55, 100)

	const score = 0.52
	if score >= strict.Threshold {
		t.Fatal("前提不成立：0.52 不该过严格档")
	}
	if score < relaxed.Threshold {
		t.Fatalf("0.52 应该过放宽档（阈值 %v）", relaxed.Threshold)
	}
}

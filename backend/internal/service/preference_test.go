package service

import (
	"testing"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/repo"
)

// 换算一律从这个时刻出发，断言才是确定的。
var prefNow = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

// 方向搞反不会报错，只会把「25 到 35 岁」变成「35 到 25 岁」，
// 然后一个候选人都匹配不到。这条守住方向。
func TestAgesToBirthYMDirection(t *testing.T) {
	minYM, maxYM := AgesToBirthYM(intPtr(25), intPtr(35), prefNow)

	if minYM == nil || maxYM == nil {
		t.Fatal("两端都填了，不该有 nil")
	}
	// 「35 岁以下」决定的是出生年月的下界（出生越早越老）
	if *minYM != 199010 {
		t.Errorf("birth_ym_min = %d, want 199010", *minYM)
	}
	// 「25 岁以上」决定上界
	if *maxYM != 200109 {
		t.Errorf("birth_ym_max = %d, want 200109", *maxYM)
	}
	if *minYM >= *maxYM {
		t.Fatalf("下界 %d 不该大于等于上界 %d —— 方向反了", *minYM, *maxYM)
	}
}

// 单边偏好：只填下限时上界必须是 nil（不限），不能顺手填成今天。
func TestAgesToBirthYMOneSided(t *testing.T) {
	minYM, maxYM := AgesToBirthYM(intPtr(25), nil, prefNow)
	if minYM != nil {
		t.Errorf("只填了 age_min，不该算出 birth_ym_min = %d", *minYM)
	}
	if maxYM == nil || *maxYM != 200109 {
		t.Errorf("age_min=25 应该给出 birth_ym_max=200109，得到 %v", maxYM)
	}

	minYM, maxYM = AgesToBirthYM(nil, intPtr(35), prefNow)
	if minYM == nil || *minYM != 199010 {
		t.Errorf("age_max=35 应该给出 birth_ym_min=199010，得到 %v", minYM)
	}
	if maxYM != nil {
		t.Errorf("只填了 age_max，不该算出 birth_ym_max = %d", *maxYM)
	}

	if minYM, maxYM := AgesToBirthYM(nil, nil, prefNow); minYM != nil || maxYM != nil {
		t.Error("两端都不填应该是两个 nil（全不限）")
	}
}

// 存进去再读出来必须回到原值，否则用户会发现系统偷偷改了他的设置。
func TestAgeRoundTrip(t *testing.T) {
	cases := []struct{ lo, hi int }{
		{18, 60}, {25, 35}, {30, 30}, {18, 18}, {59, 60},
	}
	for _, c := range cases {
		minYM, maxYM := AgesToBirthYM(intPtr(c.lo), intPtr(c.hi), prefNow)
		gotLo, gotHi := BirthYMToAges(minYM, maxYM, prefNow)
		if gotLo == nil || gotHi == nil {
			t.Fatalf("%d–%d：回显出现 nil (%v, %v)", c.lo, c.hi, gotLo, gotHi)
		}
		if *gotLo != c.lo || *gotHi != c.hi {
			t.Errorf("%d–%d 存回后变成 %d–%d", c.lo, c.hi, *gotLo, *gotHi)
		}
	}
}

// 跨年边界：1 月的时候「35 岁以下」要退到前一年，不能算成当年。
func TestAgesToBirthYMYearBoundary(t *testing.T) {
	jan := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	minYM, _ := AgesToBirthYM(nil, intPtr(35), jan)
	if minYM == nil || *minYM != 199002 {
		t.Errorf("2026 年 1 月，35 岁以下应退到 199002，得到 %v", minYM)
	}
}

// 校验：区间反了、超范围、城市太多、城市码非法，都要拦下来。
func TestValidatePreference(t *testing.T) {
	cases := []struct {
		name    string
		in      PreferenceInput
		wantErr bool
	}{
		{"空偏好合法（全不限）", PreferenceInput{}, false},
		{"正常一份", PreferenceInput{
			AgeMin: intPtr(25), AgeMax: intPtr(35),
			CityCodes: []int64{310000, 320100},
			WantChild: i16(model.WantChildYes), AcceptDivorced: i16(0),
			AcceptRemote: i16(model.RemoteAccept),
			EduMin:       i16(2), HeightMin: i16(160), HeightMax: i16(180),
			IncomeMin: i16(3), IncomeMax: i16(6),
		}, false},
		{"只填一头年龄", PreferenceInput{AgeMin: intPtr(30)}, false},
		{"年龄反了", PreferenceInput{AgeMin: intPtr(40), AgeMax: intPtr(30)}, true},
		{"年龄超上限", PreferenceInput{AgeMin: intPtr(17)}, true},
		{"年龄超下限", PreferenceInput{AgeMax: intPtr(61)}, true},
		{"城市超过 5 个", PreferenceInput{
			CityCodes: []int64{110000, 310000, 440100, 440300, 330100, 320100},
		}, true},
		{"城市码非法", PreferenceInput{CityCodes: []int64{999}}, true},
		{"婚育意愿越界", PreferenceInput{WantChild: i16(4)}, true},
		{"婚史接受度只能是 0 或 1", PreferenceInput{AcceptDivorced: i16(2)}, true},
		{"异地接受度越界", PreferenceInput{AcceptRemote: i16(3)}, true},
		{"学历要求越界", PreferenceInput{EduMin: i16(5)}, true},
		{"身高反了", PreferenceInput{HeightMin: i16(180), HeightMax: i16(160)}, true},
		{"身高越界", PreferenceInput{HeightMin: i16(139)}, true},
		{"收入反了", PreferenceInput{IncomeMin: i16(5), IncomeMax: i16(2)}, true},
		{"收入越界", PreferenceInput{IncomeMax: i16(7)}, true},
	}
	for _, c := range cases {
		err := validatePreference(&c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr = %v", c.name, err, c.wantErr)
		}
	}
}

// 重复的城市要去掉：「已选 3 个」这种计数不该被骗。
func TestValidatePreferenceDedupsCities(t *testing.T) {
	in := PreferenceInput{CityCodes: []int64{310000, 320100, 310000}}
	if err := validatePreference(&in); err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if len(in.CityCodes) != 2 {
		t.Fatalf("去重后应该是 2 个，得到 %d 个：%v", len(in.CityCodes), in.CityCodes)
	}
	if in.CityCodes[0] != 310000 || in.CityCodes[1] != 320100 {
		t.Errorf("去重后顺序应该保持，得到 %v", in.CityCodes)
	}
}

// 没填过偏好时返回空视图，不是 404：对界面来说「没填过」和「全不限」是一件事。
func TestToPreferenceViewEmpty(t *testing.T) {
	v := toPreferenceView(&repo.PrefRow{UserID: 1}, prefNow)
	if !v.Configured {
		t.Error("有行就该是 Configured")
	}
	if v.CityCodes == nil {
		t.Error("CityCodes 要是空切片而不是 nil，否则 JSON 会出 null")
	}
	if len(v.CityCodes) != 0 {
		t.Errorf("空的应该是 0 个城市，得到 %v", v.CityCodes)
	}
}

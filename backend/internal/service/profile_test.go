package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

func str(s string) *string { return &s }
func num(i int) *int       { return &i }
func i16(i int16) *int16   { return &i }

// fullProfile 造一份 20 项必填全部齐全的资料，完整度 100。
//
// v1.7 起没有「选填」这个类别了（作息删掉，其余 11 项转必填），所以这份
// 档案就是唯一的标准件：任何一条必填校验的回归都从这里长出来。
func fullProfile() model.Profile {
	return model.Profile{
		Nickname:       str("阿信"),
		Gender:         str(model.GenderMale),
		BirthYM:        num(199505),
		CityCode:       num(110000),
		HeightCM:       i16(178),
		WeightKG:       i16(65),
		EducationLevel: i16(3),
		SchoolName:     "复旦大学",
		AvatarKey:      "u/1/abc.jpg",

		HometownCode:  num(310000),
		Occupation:    "产品经理",
		Company:       "某互联网公司",
		IncomeBand:    i16(4),
		Smoking:       i16(0), // 0 = 不吸，是合法答案而不是「没填」
		Drinking:      i16(1),
		Hobbies:       "徒步、做饭",
		Intro:         "周末多半在爬山。",
		Expectation:   "想找一个愿意一起出门的人。",
		WantChild:     i16(model.WantChildYes),
		MaritalStatus: i16(model.MaritalSingle),
	}
}

func TestRequiredMissingCountsAvatar(t *testing.T) {
	p := fullProfile()
	if got := requiredMissing(&p); len(got) != 0 {
		t.Fatalf("齐全的资料不该报缺失，得到 %v", got)
	}

	// 头像必须算在必填里：它是完整度分母 20 项里的一项
	p.AvatarKey = ""
	got := requiredMissing(&p)
	if len(got) != 1 || got[0] != "avatar_key" {
		t.Fatalf("缺头像时应当只报 avatar_key，得到 %v", got)
	}

	p.Nickname = nil
	if got := requiredMissing(&p); len(got) != 2 {
		t.Fatalf("缺头像与昵称时应报 2 项，得到 %v", got)
	}

	// 空串与 nil 等价：前端清空昵称提交 "" 不能让校验放行
	p.Nickname = str("")
	if got := requiredMissing(&p); len(got) != 2 {
		t.Fatalf("空串昵称应等同于缺失，得到 %v", got)
	}
}

// TestCompletenessMatchesDoc 钉住完整度公式：filled/20*100，四舍五入取整。
//
// 它现在只是一个惰性数值（没有调用点拿它做判断），但数字仍然是可观察的
// 输出 —— 20 项的门槛一变，分母就要跟着变，这条测试是那个提醒。
func TestCompletenessMatchesDoc(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*model.Profile)
		want int
	}{
		{"齐全", func(*model.Profile) {}, 100},
		{"全空", func(p *model.Profile) { *p = model.Profile{} }, 0},
		{
			"缺一项", func(p *model.Profile) { p.HeightCM = nil },
			95, // 19/20
		},
		{
			"缺四项", func(p *model.Profile) {
				p.HeightCM, p.Company, p.Intro, p.AvatarKey = nil, "", "", ""
			},
			80, // 16/20
		},
		{
			// 「不吸烟」是一个答案，不是空值：0 必须算已填
			"吸烟填 0 仍算已填", func(p *model.Profile) { p.Smoking = i16(0) }, 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := fullProfile()
			tc.mut(&p)
			if got := completeness(&p); got != tc.want {
				t.Fatalf("完整度 = %d，期望 %d", got, tc.want)
			}
		})
	}
}

// TestRequiredMissingCountsZeroAsAnswered 单列一条：吸烟/饮酒的 0 是
// 「不」，把它当成未填会让**所有不吸烟的人**永远填不完第五步。
func TestRequiredMissingCountsZeroAsAnswered(t *testing.T) {
	p := fullProfile()
	p.Smoking, p.Drinking = i16(0), i16(0)
	if got := requiredMissing(&p); len(got) != 0 {
		t.Fatalf("吸烟/饮酒填 0 应算已填，得到缺失 %v", got)
	}

	// 反过来，nil 才是没填
	p.Smoking = nil
	if got := requiredMissing(&p); len(got) != 1 || got[0] != "smoking" {
		t.Fatalf("吸烟为 nil 时应报 smoking，得到 %v", got)
	}
}

// TestSupplementFieldsAreRequired 钉住 v1.7 的这次加项：补充那 11 项
// 逐一清空都必须报出来。写法是逐个清、逐个断言，而不是笼统地数个数 ——
// 漏掉某一项时，只数个数的那版会静静地放行。
func TestSupplementFieldsAreRequired(t *testing.T) {
	clear := map[string]func(*model.Profile){
		"hometown_code":  func(p *model.Profile) { p.HometownCode = nil },
		"occupation":     func(p *model.Profile) { p.Occupation = "" },
		"company":        func(p *model.Profile) { p.Company = "" },
		"income_band":    func(p *model.Profile) { p.IncomeBand = nil },
		"smoking":        func(p *model.Profile) { p.Smoking = nil },
		"drinking":       func(p *model.Profile) { p.Drinking = nil },
		"hobbies":        func(p *model.Profile) { p.Hobbies = "" },
		"intro":          func(p *model.Profile) { p.Intro = "" },
		"expectation":    func(p *model.Profile) { p.Expectation = "" },
		"want_child":     func(p *model.Profile) { p.WantChild = nil },
		"marital_status": func(p *model.Profile) { p.MaritalStatus = nil },
	}

	for name, mut := range clear {
		t.Run(name, func(t *testing.T) {
			p := fullProfile()
			mut(&p)
			got := requiredMissing(&p)
			if len(got) != 1 || got[0] != name {
				t.Fatalf("清空 %s 后应只报它自己，得到 %v", name, got)
			}
			if c := completeness(&p); c != 95 {
				t.Fatalf("缺一项时完整度应为 95，得到 %d", c)
			}
		})
	}
}

// TestCompletenessIgnoresPhotos 是一道防回归的锁：
// 照片不参与完整度，否则「完整度」会同时表示进度和门槛，前端没法用它。
func TestCompletenessIgnoresPhotos(t *testing.T) {
	p := fullProfile()
	base := completeness(&p)

	// admissionMissing 则相反，照片不够就不放行
	if got := admissionMissing(&p, 0); len(got) != 1 || got[0] != "photos" {
		t.Fatalf("0 张照片时应报缺 photos，得到 %v", got)
	}
	if got := admissionMissing(&p, minPhotosForActive); len(got) != 0 {
		t.Fatalf("照片够数且必填齐全时不该有缺失，得到 %v", got)
	}

	// 门槛就是 1 张，两侧都要钉住：只写下界（0 张报缺）的话，
	// 有人把 minPhotosForActive 调回 3 也不会有测试响。这个数字直接
	// 决定用户建档时要在照片页停多久，改动必须先改文档 §4.2。
	if minPhotosForActive != 1 {
		t.Fatalf("入池只要求 1 张照片，minPhotosForActive 应为 1，得到 %d", minPhotosForActive)
	}
	if got := admissionMissing(&p, 1); len(got) != 0 {
		t.Fatalf("1 张照片就该放行，得到缺失 %v", got)
	}
	if base != 100 {
		t.Fatalf("照片数不该影响完整度，齐全档案应为 100，得到 %d", base)
	}
}

func TestApplyInputValidation(t *testing.T) {
	long41 := strings.Repeat("字", 41)
	long301 := strings.Repeat("字", 301)

	cases := []struct {
		name    string
		in      ProfileInput
		wantErr *apierr.Error
	}{
		{"昵称 1 字太短", ProfileInput{Nickname: str("信")}, apierr.ErrBadRequest},
		{"昵称 2 字可用", ProfileInput{Nickname: str("阿信")}, nil},
		{"昵称 12 字可用", ProfileInput{Nickname: str(strings.Repeat("信", 12))}, nil},
		{"昵称 13 字太长", ProfileInput{Nickname: str(strings.Repeat("信", 13))}, apierr.ErrBadRequest},
		{"昵称带微信", ProfileInput{Nickname: str("微信abc")}, apierr.ErrBadRequest},
		{"昵称带长数字", ProfileInput{Nickname: str("信12345678")}, apierr.ErrBadRequest},

		{"性别取值非法", ProfileInput{Gender: str("X")}, apierr.ErrGenderImmutable},

		{"未满 18 岁", ProfileInput{BirthYM: num(201501)}, apierr.ErrBadRequest},
		{"超过 60 岁", ProfileInput{BirthYM: num(195001)}, apierr.ErrBadRequest},
		{"出生月非法", ProfileInput{BirthYM: num(199513)}, apierr.ErrBadRequest},

		{"城市码越界", ProfileInput{CityCode: num(109999)}, apierr.ErrBadRequest},
		{"身高过低", ProfileInput{HeightCM: i16(139)}, apierr.ErrBadRequest},
		{"身高过高", ProfileInput{HeightCM: i16(211)}, apierr.ErrBadRequest},
		{"体重过低", ProfileInput{WeightKG: i16(34)}, apierr.ErrBadRequest},
		{"学历越界", ProfileInput{EducationLevel: i16(5)}, apierr.ErrBadRequest},
		{"收入区间越界", ProfileInput{IncomeBand: i16(7)}, apierr.ErrBadRequest},
		{"吸烟越界", ProfileInput{Smoking: i16(3)}, apierr.ErrBadRequest},

		{"院校 41 字", ProfileInput{SchoolName: &long41}, apierr.ErrBadRequest},
		{"自我介绍 301 字", ProfileInput{Intro: &long301}, apierr.ErrBadRequest},
		{"期待的 301 字", ProfileInput{Expectation: &long301}, apierr.ErrBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := fullProfile()
			err := applyInput(&p, &tc.in)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("不该报错，得到 %v", err)
				}
				return
			}
			var got *apierr.Error
			if !errors.As(err, &got) {
				t.Fatalf("期望 %s，得到 %v", tc.wantErr.Code, err)
			}
			if got.Code != tc.wantErr.Code {
				t.Fatalf("错误码 = %s，期望 %s", got.Code, tc.wantErr.Code)
			}
		})
	}
}

// TestGenderImmutable 单列出来：允许来回改性别等于让人免费刷两侧候选池。
func TestGenderImmutable(t *testing.T) {
	p := fullProfile() // 男
	err := applyInput(&p, &ProfileInput{Gender: str(model.GenderFemale)})
	var got *apierr.Error
	if !errors.As(err, &got) || got.Code != apierr.ErrGenderImmutable.Code {
		t.Fatalf("改性别应被拒绝，得到 %v", err)
	}

	// 重复提交同一个值是幂等的，前端回填再保存不该报错
	if err := applyInput(&p, &ProfileInput{Gender: str(model.GenderMale)}); err != nil {
		t.Fatalf("重复提交原性别不该报错，得到 %v", err)
	}
}

// TestApplyInputOnlyTouchesProvidedFields 是分步建档的前提：
// 向导每一步只提交自己那几个字段，没提交的不能被清空。
func TestApplyInputOnlyTouchesProvidedFields(t *testing.T) {
	p := fullProfile()
	p.Intro = "原来的自我介绍"

	if err := applyInput(&p, &ProfileInput{CityCode: num(310000)}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if p.Intro != "原来的自我介绍" {
		t.Fatal("没提交的字段被清空了")
	}
	if p.Nickname == nil || *p.Nickname != "阿信" {
		t.Fatal("没提交的字段被清空了")
	}
	if p.CityCode == nil || *p.CityCode != 310000 {
		t.Fatal("提交的字段没写进去")
	}
}

func TestParseHobbies(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"顿号分隔", "徒步、做饭", []string{"徒步", "做饭"}, false},
		{"逗号也能用", "徒步,做饭", []string{"徒步", "做饭"}, false},
		{"中文逗号也能用", "徒步，做饭", []string{"徒步", "做饭"}, false},
		{"去重", "徒步、徒步、做饭", []string{"徒步", "做饭"}, false},
		{"空串得到空数组", "", []string{}, false},
		{"单个标签太长", strings.Repeat("字", 9), nil, true},
		{"超过 6 个", "a、b、c、d、e、f、g", nil, true},
		{"正好 6 个", "a、b、c、d、e、f", []string{"a", "b", "c", "d", "e", "f"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHobbies(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("得到 %v，期望 %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("得到 %v，期望 %v", got, tc.want)
				}
			}
		})
	}
}

func TestSplitTagsReturnsEmptyNotNil(t *testing.T) {
	// 返回 nil 的话 JSON 里是 null，前端 .length 会炸
	if got := splitTags(""); got == nil || len(got) != 0 {
		t.Fatalf("空串应返回空切片，得到 %#v", got)
	}
}

func TestAgeFromBirthYM(t *testing.T) {
	now := time.Now()
	// 当年当月出生即算 0 岁；这里只验证「生日当月是否已长一岁」
	sameMonth := now.Year() - 30
	if got, err := ageFromBirthYM(sameMonth*100 + int(now.Month())); err != nil || got != 30 {
		t.Fatalf("当月生日应为 30 岁，得到 %d, %v", got, err)
	}
	// 下个月出生，说明还没过生日
	next := now.Month() + 1
	year := now.Year() - 30
	if next > 12 {
		next, year = 1, year+1
	}
	if got, err := ageFromBirthYM(year*100 + int(next)); err != nil || got != 29 {
		t.Fatalf("还没过生日应为 29 岁，得到 %d, %v", got, err)
	}

	for _, bad := range []int{0, 199513, 189912, 300001} {
		if _, err := ageFromBirthYM(bad); err == nil {
			t.Fatalf("%d 应被判为格式错误", bad)
		}
	}
}

func TestValidCityCode(t *testing.T) {
	// 710000/810000/820000 是港澳台：建档页的省市联动把它们也列进去了，
	// 而它们在民政部数据里只有省级一条、码落在旧上界之外。放进来是为了
	// 钉住「选香港不会被静默拒掉」这件事 —— 这个错只会在港澳台用户身上
	// 出现，池子里永远碰不到。
	for _, ok := range []int{110000, 310000, 440300, 659999, 710000, 810000, 820000} {
		if !validCityCode(ok) {
			t.Fatalf("%d 应是合法城市码", ok)
		}
	}
	for _, bad := range []int{0, 109999, 830000, 999999} {
		if validCityCode(bad) {
			t.Fatalf("%d 应是非法城市码", bad)
		}
	}
}

func TestDaysInMonth(t *testing.T) {
	cases := []struct {
		year, month, want int
	}{
		{1995, 1, 31}, {1995, 4, 30}, {1995, 2, 28},
		{1996, 2, 29}, // 普通闰年
		{2000, 2, 29}, // 四百年再闰
		{1900, 2, 28}, // 百年不闰
	}
	for _, tc := range cases {
		if got := daysInMonth(tc.year, tc.month); got != tc.want {
			t.Errorf("daysInMonth(%d, %d) = %d，期望 %d", tc.year, tc.month, got, tc.want)
		}
	}
}

// TestValidDay 钉住「日必须真实存在」：2 月 29 只在闰年合法，
// 4 月没有 31 日。DB 侧还有 p_birth_day_chk 兜底，两边要一致。
func TestValidDay(t *testing.T) {
	ok := []struct {
		ym  int
		day int16
	}{
		{199508, 31}, {199501, 31}, {199509, 30},
		{199602, 29}, {200002, 29}, {199502, 28},
	}
	for _, tc := range ok {
		if !validDay(tc.ym, tc.day) {
			t.Errorf("%d 年的 %d 日应当是合法的", tc.ym, tc.day)
		}
	}

	bad := []struct {
		ym  int
		day int16
	}{
		{199502, 29}, // 平年没有 2 月 29
		{190002, 29}, // 百年不闰
		{199504, 31}, // 4 月没有 31 日
		{199509, 31},
		{199508, 0}, {199508, 32}, {199508, -1},
		{199513, 1}, // 年月本身非法
	}
	for _, tc := range bad {
		if validDay(tc.ym, tc.day) {
			t.Errorf("%d 年的 %d 日应当是非法的", tc.ym, tc.day)
		}
	}
}

// TestApplyInputBirthDay 是这次三级联动改造的核心回归：
// 「日」与「年月」是一对，年月换了之后旧的日子可能不再存在。
func TestApplyInputBirthDay(t *testing.T) {
	t.Run("合法的一天存得进去", func(t *testing.T) {
		p := fullProfile()
		if err := applyInput(&p, &ProfileInput{BirthDay: i16(20)}); err != nil {
			t.Fatalf("不该报错: %v", err)
		}
		if p.BirthDay == nil || *p.BirthDay != 20 {
			t.Fatalf("BirthDay = %v，期望 20", p.BirthDay)
		}
	})

	t.Run("越界与不存在的日子被拒", func(t *testing.T) {
		for _, d := range []int16{0, 32, -1} {
			p := fullProfile() // 199505
			if err := applyInput(&p, &ProfileInput{BirthDay: i16(d)}); err == nil {
				t.Errorf("day = %d 应被拒", d)
			}
		}
		// 1995 是平年，2 月 29 不存在
		p := fullProfile()
		if err := applyInput(&p, &ProfileInput{BirthYM: num(199502), BirthDay: i16(29)}); err == nil {
			t.Error("1995-02-29 应被拒")
		}
		// 1996 是闰年，同一天应当放行
		p = fullProfile()
		if err := applyInput(&p, &ProfileInput{BirthYM: num(199602), BirthDay: i16(29)}); err != nil {
			t.Errorf("1996-02-29 应当放行，得到 %v", err)
		}
	})

	t.Run("没有年月时不能单独提交日", func(t *testing.T) {
		p := fullProfile()
		p.BirthYM = nil
		err := applyInput(&p, &ProfileInput{BirthDay: i16(20)})
		if err == nil {
			t.Fatal("库里没有出生年月时，单独提交日应被拒")
		}
	})

	// 这一条挡住 1995-02-31 进库：前端表达不了「清空」（ProfileInput 全是
	// 指针，null 与缺失都解成 nil），所以清值只能落在服务端。
	t.Run("换年月后失效的日被清空", func(t *testing.T) {
		p := fullProfile()
		p.BirthYM, p.BirthDay = num(199501), i16(31) // 1 月 31 日
		if err := applyInput(&p, &ProfileInput{BirthYM: num(199502)}); err != nil {
			t.Fatalf("不该报错: %v", err)
		}
		if p.BirthDay != nil {
			t.Fatalf("2 月里没有 31 日，BirthDay 应被清空，得到 %v", *p.BirthDay)
		}
	})

	t.Run("换年月后仍然成立的日保留", func(t *testing.T) {
		p := fullProfile()
		p.BirthYM, p.BirthDay = num(199501), i16(31) // 1 月 31 日
		if err := applyInput(&p, &ProfileInput{BirthYM: num(199503)}); err != nil {
			t.Fatalf("不该报错: %v", err)
		}
		if p.BirthDay == nil || *p.BirthDay != 31 {
			t.Fatalf("3 月也有 31 日，不该清空，得到 %v", p.BirthDay)
		}
	})
}

// TestBirthDayOutOfScopeForAdmission 把两条红线写成可执行断言：
// 「日」不进完整度、不进入池门槛。
//
// 理由是它只影响展示（年龄始终按月算）。加进必填的代价则是实打实的：
// requiredMissing 只在 status = onboarding 时判，但 completeness 是**存量列**，
// 老档案下次保存时才重算 —— 加进去会让一批人静默掉出候选集。
// 想改这两条，得先删掉这个测试。
func TestBirthDayOutOfScopeForAdmission(t *testing.T) {
	without := fullProfile()
	with := fullProfile()
	with.BirthDay = i16(20)

	if c1, c2 := completeness(&without), completeness(&with); c1 != c2 {
		t.Fatalf("完整度不该受「日」影响：%d vs %d", c1, c2)
	}
	if m1, m2 := requiredMissing(&without), requiredMissing(&with); len(m1) != len(m2) {
		t.Fatalf("缺失项不该受「日」影响：%v vs %v", m1, m2)
	}

	// 只知道年月的档案（老数据）必须是齐的，否则老用户会被判成没建完档
	if got := requiredMissing(&without); len(got) != 0 {
		t.Fatalf("只缺「日」不算缺必填，得到 %v", got)
	}
}

func TestNextStep(t *testing.T) {
	// 登录接口与 /me 必须给出一致的下一步，
	// 否则前端会在建档向导和首页之间来回弹跳
	if NextStep(model.StatusOnboarding) != StepOnboarding {
		t.Fatal("未建档用户应被送去建档")
	}
	for _, s := range []string{model.StatusActive, model.StatusUnderReview} {
		if NextStep(s) != StepHome {
			t.Fatalf("%s 用户应进首页", s)
		}
	}
}

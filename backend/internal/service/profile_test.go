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

// fullProfile 造一份七项必填齐全、选填全空的资料。
// 它是完整度算法里 60 分的那个基准点。
func fullProfile() model.Profile {
	return model.Profile{
		Nickname:       str("阿信"),
		Gender:         str(model.GenderMale),
		BirthYM:        num(199505),
		CityCode:       num(110000),
		HeightCM:       i16(178),
		EducationLevel: i16(3),
		AvatarKey:      "u/1/abc.jpg",
	}
}

func TestRequiredMissingCountsAvatar(t *testing.T) {
	p := fullProfile()
	if got := requiredMissing(&p); len(got) != 0 {
		t.Fatalf("齐全的资料不该报缺失，得到 %v", got)
	}

	// 头像必须算在必填里：它是完整度公式里 7 项的分母之一（文档 §4.2）
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

// TestCompletenessMatchesDoc 钉住文档 §4.2 的公式：
// filledRequired/7*60 + filledOptional/12*40，四舍五入取整。
// 这个数字直接决定用户能不能被引荐，改动必须先改文档。
func TestCompletenessMatchesDoc(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*model.Profile)
		want int
	}{
		{"必填齐全、选填全空", func(*model.Profile) {}, 60},
		{"全空", func(p *model.Profile) { *p = model.Profile{} }, 0},
		{
			"必填缺一项", func(p *model.Profile) { p.HeightCM = nil },
			51, // 6/7*60 = 51.43
		},
		{
			"必填齐全 + 6 项选填", func(p *model.Profile) {
				p.SchoolName, p.Occupation, p.Company = "A", "B", "C"
				p.Intro, p.Expectation, p.Hobbies = "i", "e", "h"
			},
			80, // 60 + 6/12*40
		},
		{
			"全部填满", func(p *model.Profile) {
				p.HometownCode, p.WeightKG = num(310000), i16(65)
				p.SchoolName, p.Occupation, p.Company = "A", "B", "C"
				p.IncomeBand, p.Chronotype = i16(3), i16(1)
				p.Smoking, p.Drinking = i16(0), i16(0)
				p.Hobbies, p.Intro, p.Expectation = "h", "i", "e"
			},
			100,
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
	if base != 60 {
		t.Fatalf("照片数不该影响完整度，基准应为 60，得到 %d", base)
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
		{"作息越界", ProfileInput{Chronotype: i16(4)}, apierr.ErrBadRequest},
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
	for _, ok := range []int{110000, 310000, 440300, 659999} {
		if !validCityCode(ok) {
			t.Fatalf("%d 应是合法城市码", ok)
		}
	}
	for _, bad := range []int{0, 109999, 660000, 999999} {
		if validCityCode(bad) {
			t.Fatalf("%d 应是非法城市码", bad)
		}
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

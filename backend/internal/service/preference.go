package service

import (
	"context"
	"time"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/repo"
)

// 偏好。两层，行为完全不同（§4.3）：
//
//	硬条件 5 项 —— 一票否决，不合就淘汰，连打分的机会都没有
//	软偏好 3 项 —— 只参与打分（合计占 0.65 权重），不合也照推，只是分数低
//
// 全程「NULL = 不限」，且这条约定要在三处一致：存储为空、打分时该维度
// 不进分母、界面上显示「不限」而不是空白。任何一处漏掉，
// 用户都会以为自己的偏好被系统无视了。
//
// 接口用 PUT 全量替换，不做 PATCH：偏好是「我当前的要求」，没有历史价值，
// 也不需要区分「没传这个字段」和「显式清空」——两者都是不限。
// 这让「清空一项」不需要额外的哨兵值。

// PreferenceInput 是 PUT /me/preferences 的请求体。
//
// 年龄用「岁」而不是 birth_ym：界面上的滑块本来就是岁数，
// 让前端做生日换算会把闰月和「今年生日过没过」的边界错误散到两处。
type PreferenceInput struct {
	AgeMin *int `json:"age_min"`
	AgeMax *int `json:"age_max"`

	CityCodes []int64 `json:"city_codes"`

	WantChild      *int16 `json:"want_child"`
	AcceptDivorced *int16 `json:"accept_divorced"`
	AcceptRemote   *int16 `json:"accept_remote"`

	EduMin    *int16 `json:"edu_min"`
	HeightMin *int16 `json:"height_min"`
	HeightMax *int16 `json:"height_max"`
	IncomeMin *int16 `json:"income_min"`
	IncomeMax *int16 `json:"income_max"`
}

// PreferenceView 是返回给本人的偏好，字段与请求体一致。
// 全空表示这个人还没填过偏好 —— 界面上等价于「全部不限」，
// 但可以据此提示「填写偏好能提高引荐质量」。
type PreferenceView struct {
	AgeMin *int `json:"age_min"`
	AgeMax *int `json:"age_max"`

	CityCodes []int64 `json:"city_codes"`

	WantChild      *int16 `json:"want_child"`
	AcceptDivorced *int16 `json:"accept_divorced"`
	AcceptRemote   *int16 `json:"accept_remote"`

	EduMin    *int16 `json:"edu_min"`
	HeightMin *int16 `json:"height_min"`
	HeightMax *int16 `json:"height_max"`
	IncomeMin *int16 `json:"income_min"`
	IncomeMax *int16 `json:"income_max"`

	// Configured 表示有没有一行偏好记录。全是「不限」时为 false，
	// 界面据此决定要不要显示「还没设置偏好」的引导。
	Configured bool `json:"configured"`
}

// 年龄与身高的可填范围。与建档向导里同一项的上下界保持一致 ——
// 两边不一致会出现「我 138cm，但偏好下限只能选 140cm」这种自相矛盾的选项。
const (
	prefMinAge = 18
	prefMaxAge = 60

	prefMinHeight = 140
	prefMaxHeight = 210
)

// GetPreference 读偏好。从没填过返回一份全「不限」的空视图，不是 404 ——
// 对界面来说「没填过」和「全不限」是同一件事。
func (s *Service) GetPreference(ctx context.Context, user *model.User) (*PreferenceView, error) {
	row, err := s.Repo.GetPreference(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &PreferenceView{CityCodes: []int64{}}, nil
	}
	return toPreferenceView(row, time.Now()), nil
}

// SavePreference 全量替换偏好。
//
// 先校验再落库，中间不留半成品：校验失败时一行都不写。
func (s *Service) SavePreference(ctx context.Context, user *model.User, in *PreferenceInput) (*PreferenceView, error) {
	now := time.Now()

	if err := validatePreference(in); err != nil {
		return nil, err
	}

	row := repo.PrefRow{
		UserID:         user.ID,
		CityCodes:      in.CityCodes,
		WantChild:      in.WantChild,
		AcceptDivorced: in.AcceptDivorced,
		AcceptRemote:   in.AcceptRemote,
		EduMin:         in.EduMin,
		HeightMin:      in.HeightMin,
		HeightMax:      in.HeightMax,
		IncomeMin:      in.IncomeMin,
		IncomeMax:      in.IncomeMax,
	}
	row.BirthYMMin, row.BirthYMMax = AgesToBirthYM(in.AgeMin, in.AgeMax, now)

	if err := s.Repo.UpsertPreference(ctx, row); err != nil {
		return nil, err
	}
	return toPreferenceView(&row, now), nil
}

// toPreferenceView 把存储形态换算回界面形态。
//
// birth_ym 换回岁数是近似的：只知道出生年月时，一个人在生日当月前后
// 会算出相差 1 岁的结果，而这里没有「生日过了没」的信息。
// 用当前月份当分界就够了 —— 界面本来就是让人选「25 到 35 岁」，
// 不是让人核对某一天的精确年龄。
func toPreferenceView(p *repo.PrefRow, now time.Time) *PreferenceView {
	ageMin, ageMax := BirthYMToAges(p.BirthYMMin, p.BirthYMMax, now)

	cities := p.CityCodes
	if cities == nil {
		// 让 JSON 出 [] 而不是 null：前端不用为「空偏好」单独写一个分支
		cities = []int64{}
	}

	return &PreferenceView{
		AgeMin:         ageMin,
		AgeMax:         ageMax,
		CityCodes:      cities,
		WantChild:      p.WantChild,
		AcceptDivorced: p.AcceptDivorced,
		AcceptRemote:   p.AcceptRemote,
		EduMin:         p.EduMin,
		HeightMin:      p.HeightMin,
		HeightMax:      p.HeightMax,
		IncomeMin:      p.IncomeMin,
		IncomeMax:      p.IncomeMax,
		Configured:     true,
	}
}

// -------------------------------------------------------- 年龄 ↔ 出生年月

// intPtr 取一个 int 的地址。年龄区间两端都可为空，换算函数到处要用它。
func intPtr(v int) *int { return &v }

// ymToMonths / monthsToYM 是这两个换算的底座。
//
// 不用 time.AddDate 推：它做的是日历运算，而这里要的是「年龄」这个
// 以整月为单位的概念。自己数月数，闰年和月末都不用特殊处理。
func ymToMonths(ym int) int {
	year, month := ym/100, ym%100
	return year*12 + (month - 1)
}

func monthsToYM(m int) int {
	return (m/12)*100 + (m%12 + 1)
}

// AgesToBirthYM 把年龄区间换成 birth_ym 区间（闭区间）。
//
// 方向是反的：年龄越大出生越早，所以 minAge 决定的是 birth_ym 的**上界**。
// 这里写成一组带断言的单元测试，因为搞反了不会报错 ——
// 只会安静地把「25 到 35 岁」变成「35 到 25 岁」，然后一个人都匹配不到。
//
// 推导：令 nowM 为当前月，b 为出生月（都已换算成「月序号」）。
// 某人已活的月数是 nowM-b，年龄是 floor((nowM-b)/12)。
//
//	年龄 ≥ minAge  ⟺  b ≤ nowM - minAge*12        → birth_ym_max
//	年龄 ≤ maxAge  ⟺  b ≥ nowM - (maxAge+1)*12 + 1 → birth_ym_min
//
// 两端各自可为 nil（该端不限），返回的也是指针。
func AgesToBirthYM(minAge, maxAge *int, now time.Time) (minYM, maxYM *int) {
	nowM := ymToMonths(now.Year()*100 + int(now.Month()))

	if maxAge != nil {
		lo := nowM - (*maxAge+1)*12 + 1
		minYM = intPtr(monthsToYM(lo))
	}
	if minAge != nil {
		hi := nowM - *minAge*12
		maxYM = intPtr(monthsToYM(hi))
	}
	return minYM, maxYM
}

// BirthYMToAges 是 AgesToBirthYM 的反向换算，用于回显。
//
// 反解出来的区间会比原始输入宽一点点（因为闭区间两端各含一个整月），
// 这里刻意收窄到「在这个 birth_ym 区间里的所有人都满足」的年龄区间，
// 免得用户存 25–35、回来看到 25–36 以为系统改了他的设置。
func BirthYMToAges(minYM, maxYM *int, now time.Time) (minAge, maxAge *int) {
	nowM := ymToMonths(now.Year()*100 + int(now.Month()))

	if maxYM != nil {
		// 最年轻的可能：出生最晚的那个月
		age := (nowM - ymToMonths(*maxYM)) / 12
		minAge = intPtr(age)
	}
	if minYM != nil {
		// 最年长的可能：出生最早的那个月
		age := (nowM - ymToMonths(*minYM)) / 12
		maxAge = intPtr(age)
	}
	return minAge, maxAge
}

// ------------------------------------------------------------ 校验

func validatePreference(in *PreferenceInput) error {
	// 年龄：两头都只在填了的时候校验，允许只填一头（「35 岁以下」）
	if in.AgeMin != nil && (*in.AgeMin < prefMinAge || *in.AgeMin > prefMaxAge) {
		return apierr.ErrBadRequest.WithMessage("年龄下限需在 18 到 60 之间")
	}
	if in.AgeMax != nil && (*in.AgeMax < prefMinAge || *in.AgeMax > prefMaxAge) {
		return apierr.ErrBadRequest.WithMessage("年龄上限需在 18 到 60 之间")
	}
	if in.AgeMin != nil && in.AgeMax != nil && *in.AgeMin > *in.AgeMax {
		return apierr.ErrBadRequest.WithMessage("年龄下限不能大于上限")
	}

	// 城市：最多 5 个，去重后落库 —— 重复的城市只会让「已选 3 个」这种
	// 计数骗人，对过滤没有任何影响。
	if len(in.CityCodes) > 5 {
		return apierr.ErrBadRequest.WithMessage("期望城市最多选 5 个")
	}
	seen := make(map[int64]struct{}, len(in.CityCodes))
	deduped := make([]int64, 0, len(in.CityCodes))
	for _, c := range in.CityCodes {
		if !validCityCode(int(c)) {
			return apierr.ErrBadRequest.WithMessage("期望城市代码不正确")
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		deduped = append(deduped, c)
	}
	in.CityCodes = deduped

	if in.WantChild != nil &&
		(*in.WantChild < model.WantChildYes || *in.WantChild > model.WantChildTBD) {
		return apierr.ErrBadRequest.WithMessage("婚育意愿取值不正确")
	}
	// 是否接受有婚史：0 不接受 / 1 接受。界面上是个开关，
	// 所以这里接受 0 和 1，不接受 NULL 以外的其它值。
	if in.AcceptDivorced != nil && *in.AcceptDivorced != 0 && *in.AcceptDivorced != 1 {
		return apierr.ErrBadRequest.WithMessage("婚史接受度取值不正确")
	}
	if in.AcceptRemote != nil &&
		(*in.AcceptRemote < model.RemoteAccept || *in.AcceptRemote > model.RemoteNotAccept) {
		return apierr.ErrBadRequest.WithMessage("异地接受度取值不正确")
	}

	// 软偏好三项。范围与档案里同一项的取值域对齐（学历 1–4、收入 1–6），
	// 否则会出现「我自己是 3，但我要求对方至少 5」这种填不出来的组合。
	if in.EduMin != nil && (*in.EduMin < 1 || *in.EduMin > 4) {
		return apierr.ErrBadRequest.WithMessage("学历要求取值不正确")
	}
	if in.HeightMin != nil && (*in.HeightMin < prefMinHeight || *in.HeightMin > prefMaxHeight) {
		return apierr.ErrBadRequest.WithMessage("身高下限需在 140 到 210 之间")
	}
	if in.HeightMax != nil && (*in.HeightMax < prefMinHeight || *in.HeightMax > prefMaxHeight) {
		return apierr.ErrBadRequest.WithMessage("身高上限需在 140 到 210 之间")
	}
	if in.HeightMin != nil && in.HeightMax != nil && *in.HeightMin > *in.HeightMax {
		return apierr.ErrBadRequest.WithMessage("身高下限不能大于上限")
	}
	if in.IncomeMin != nil && (*in.IncomeMin < 1 || *in.IncomeMin > 6) {
		return apierr.ErrBadRequest.WithMessage("收入要求取值不正确")
	}
	if in.IncomeMax != nil && (*in.IncomeMax < 1 || *in.IncomeMax > 6) {
		return apierr.ErrBadRequest.WithMessage("收入要求取值不正确")
	}
	if in.IncomeMin != nil && in.IncomeMax != nil && *in.IncomeMin > *in.IncomeMax {
		return apierr.ErrBadRequest.WithMessage("收入下限不能大于上限")
	}

	return nil
}

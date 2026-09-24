package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/schools"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
)

// 文本字段长度上限。取自完整版 PRD 5.4 的字段字典，
// 不是随手定的 —— 改这里要连着那份字典一起改。
const (
	nicknameMinRunes = 2
	nicknameMaxRunes = 12
	shortTextMax     = 40  // school_name / occupation / company
	longTextMax      = 300 // intro / expectation
	maxHobbies       = 6
)

// 入池要求的最少照片数（见 §4.2「照片另算」）。
//
// 1 张就够。首次建档要在两分钟内走完，把「传够 3 张」摆在门口会把人挡在
// 门外 —— 而引荐卡只取 position = 0 那一张当封面，1 张的卡是完整的卡。
// 多传照片仍然有价值（别人更容易记住你），但它现在是建议，不是门槛：
// 前端照 PHOTO_GOAL 催，后端不认这个数。
const minPhotosForActive = 1

// ProfileInput 是建档/编辑的入参。
//
// 全部用指针：建档是分步向导，每一步只提交自己那几个字段，
// 必须能区分「这次没提交」和「提交了空值」。
type ProfileInput struct {
	// 必填 6 项
	Nickname       *string `json:"nickname"`
	Gender         *string `json:"gender"`
	BirthYM        *int    `json:"birth_ym"`
	CityCode       *int    `json:"city_code"`
	HeightCM       *int16  `json:"height_cm"`
	EducationLevel *int16  `json:"education_level"`

	// BirthDay 与 BirthYM 配对提交，单独提交会被拒（没有年月就无从判断
	// 这一天存不存在）。它不在必填 6 项里：老档案只知道年月，逼他们补一个
	// 日子等于逼人编造。
	BirthDay *int16 `json:"birth_day"`

	// 选填 12 项
	HometownCode *int    `json:"hometown_code"`
	WeightKG     *int16  `json:"weight_kg"`
	SchoolName   *string `json:"school_name"`
	Occupation   *string `json:"occupation"`
	Company      *string `json:"company"`
	IncomeBand   *int16  `json:"income_band"`
	Chronotype   *int16  `json:"chronotype"`
	Smoking      *int16  `json:"smoking"`
	Drinking     *int16  `json:"drinking"`
	Hobbies      *string `json:"hobbies"` // 顿号分隔的字符串，≤6 个
	Intro        *string `json:"intro"`
	Expectation  *string `json:"expectation"`

	// 硬条件两项。它们是「我自己的情况」，与 preferences 侧的
	// 「我要求对方怎样」是两回事，匹配时要把两边对着比。
	//
	// 不计入完整度：§4.2 的 12 项选填是逐一列过的，这两项不在其中，
	// 加进来会把所有人的完成度改掉，也会让那 12 项的清单失去意义。
	//
	// 异地接受度不在这里 —— 它属于偏好，走 /me/preferences。
	WantChild     *int16 `json:"want_child"`
	MaritalStatus *int16 `json:"marital_status"`
}

// ProfileView 是返回给本人的完整资料。
type ProfileView struct {
	UserID        int64    `json:"user_id"`
	Status        string   `json:"status"`
	NextStep      string   `json:"next_step"`
	Completeness  int16    `json:"completeness"`
	MissingFields []string `json:"missing_required"`

	Nickname       *string  `json:"nickname"`
	Gender         *string  `json:"gender"`
	BirthYM        *int     `json:"birth_ym"`
	BirthDay       *int16   `json:"birth_day"`
	Age            *int     `json:"age"`
	CityCode       *int     `json:"city_code"`
	HeightCM       *int16   `json:"height_cm"`
	EducationLevel *int16   `json:"education_level"`
	HometownCode   *int     `json:"hometown_code"`
	WeightKG       *int16   `json:"weight_kg"`
	SchoolName     string   `json:"school_name"`
	Occupation     string   `json:"occupation"`
	Company        string   `json:"company"`
	IncomeBand     *int16   `json:"income_band"`
	Chronotype     *int16   `json:"chronotype"`
	Smoking        *int16   `json:"smoking"`
	Drinking       *int16   `json:"drinking"`
	Hobbies        []string `json:"hobbies"`
	Intro          string   `json:"intro"`
	Expectation    string   `json:"expectation"`

	WantChild     *int16 `json:"want_child"`
	MaritalStatus *int16 `json:"marital_status"`

	AvatarKey  string `json:"avatar_key"`
	AvatarURL  string `json:"avatar_url"`
	PhotoCount int    `json:"photo_count"`
}

// GetProfile 读取本人资料。
func (s *Service) GetProfile(ctx context.Context, user *model.User) (*ProfileView, error) {
	var p model.Profile
	if err := s.Repo.DB.WithContext(ctx).First(&p, "user_id = ?", user.ID).Error; err != nil {
		s.Log.Error("读取资料失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}
	return s.buildView(user, &p, s.photoCount(ctx, user.ID)), nil
}

// UpdateProfile 保存资料。建档向导每一步都调它，字段可以分批提交。
func (s *Service) UpdateProfile(ctx context.Context, user *model.User, in *ProfileInput) (*ProfileView, error) {
	var p model.Profile
	if err := s.Repo.DB.WithContext(ctx).First(&p, "user_id = ?", user.ID).Error; err != nil {
		s.Log.Error("读取资料失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	// 资料审核只针对文本内容：身高/城市/学历这类枚举与数值字段没有
	// 可审核的文本，改它们不该产生审核任务。先记下改动前的文本签名，
	// applyInput 归一化之后再比较是否真的变了。
	before := textSigOf(&p)

	// active 用户首次改动文本（approved → pending）需要快照，供
	// 审核拒绝时回退。先算出来，确认文本真的变了才写入。
	var snap []byte
	if user.Status == model.StatusActive && p.ProfileReviewState == model.ReviewApproved {
		var err error
		snap, err = snapshotOf(&p)
		if err != nil {
			s.Log.Error("生成资料快照失败", "err", err, "uid", user.ID)
			return nil, apierr.ErrInternal
		}
	}

	if err := applyInput(&p, in); err != nil {
		return nil, err
	}

	photoCount := s.photoCount(ctx, user.ID)
	p.Completeness = int16(completeness(&p))

	// 建档向导本身不触发审核，只改枚举数值字段也不触发
	if user.Status == model.StatusActive && before != textSigOf(&p) {
		if snap != nil {
			p.ProfileSnapshot = snap
		}
		p.ProfileReviewState = model.ReviewPending
	}

	if err := s.Repo.Tx(func(tx *gorm.DB) error {
		return tx.Save(&p).Error
	}); err != nil {
		s.Log.Error("保存资料失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	// 存完统一判一次入池条件，和媒体接口走同一条路径
	s.applyProfileState(ctx, user, &p, photoCount)

	return s.buildView(user, &p, photoCount), nil
}

// applyProfileState 重算完整度，并在入池条件齐备时把 status 翻成 active。
//
// 两件事必须在一起做，而且必须被资料接口和媒体接口共用：
// 入池条件 = 6 项必填 + 头像 + ≥1 张照片，其中头像和照片不是资料接口设的。
// 只在 UpdateProfile 里判翻牌的话，「先填资料、再传头像和照片」这条
// 最自然的路径永远翻不了牌 —— 用户明明什么都填完了，还是卡在建档流程里。
//
// 调用方传 photoCount 是为了避免重复 COUNT；p 必须是已经落库的那一行。
func (s *Service) applyProfileState(ctx context.Context, user *model.User, p *model.Profile, photoCount int) {
	if want := int16(completeness(p)); want != p.Completeness {
		if err := s.Repo.DB.WithContext(ctx).Model(&model.Profile{}).
			Where("user_id = ?", p.UserID).
			Update("completeness", want).Error; err != nil {
			s.Log.Warn("更新完整度失败", "err", err, "uid", p.UserID)
		} else {
			p.Completeness = want
		}
	}

	// 只有 onboarding 会翻牌。已经是 active 的不动它 —— 用户删张照片
	// 就被踢回建档流程的话，已经建立的引荐与会话会突然失效。
	if user.Status != model.StatusOnboarding {
		return
	}
	if len(admissionMissing(p, photoCount)) > 0 {
		return
	}

	// WHERE 带上 status 条件：两个请求同时判定成功时只有一个能改到行，
	// 不需要额外加锁，也不会打出两条「建档完成」的日志。
	res := s.Repo.DB.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND status = ?", user.ID, model.StatusOnboarding).
		Update("status", model.StatusActive)
	if res.Error != nil {
		s.Log.Warn("翻转入池状态失败", "err", res.Error, "uid", user.ID)
		return
	}
	if res.RowsAffected > 0 {
		user.Status = model.StatusActive
		s.Log.Info("建档完成，用户进入推荐池", "uid", user.ID)
	}
}

// syncAfterMedia 供媒体接口调用：照片或头像变动之后重算门槛。
func (s *Service) syncAfterMedia(ctx context.Context, user *model.User) {
	var p model.Profile
	if err := s.Repo.DB.WithContext(ctx).First(&p, "user_id = ?", user.ID).Error; err != nil {
		s.Log.Warn("媒体变更后读取资料失败", "err", err, "uid", user.ID)
		return
	}
	s.applyProfileState(ctx, user, &p, s.photoCount(ctx, user.ID))
}

// applyInput 逐字段校验并写入。校验全部在这里，handler 不做业务判断。
func applyInput(p *model.Profile, in *ProfileInput) error {
	if in.Nickname != nil {
		v := strings.TrimSpace(*in.Nickname)
		if n := len([]rune(v)); n < nicknameMinRunes || n > nicknameMaxRunes {
			return apierr.ErrBadRequest.WithMessage("昵称需要 2 到 12 个字")
		}
		if looksLikeContact(v) {
			return apierr.ErrBadRequest.WithMessage("昵称里不能包含联系方式")
		}
		p.Nickname = &v
	}

	if in.Gender != nil {
		v := strings.ToUpper(strings.TrimSpace(*in.Gender))
		if v != model.GenderMale && v != model.GenderFemale {
			return apierr.ErrGenderImmutable.WithMessage("性别只能是 M 或 F")
		}
		// 性别设定后不可修改：允许来回切换等于让人免费刷两侧候选池
		if p.Gender != nil && *p.Gender != v {
			return apierr.ErrGenderImmutable
		}
		p.Gender = &v
	}

	if in.BirthYM != nil {
		if err := checkBirthYM(*in.BirthYM); err != nil {
			return err
		}
		p.BirthYM = in.BirthYM
		// 年月换了，存量的「日」可能在新月份里不存在（1 月 31 日 → 2 月）。
		// 必须在这里把它清掉：前端**没有办法**表达「清空」—— ProfileInput
		// 全是指针，JSON 的 null 与「字段缺失」都解成 nil，buildPatch 又跳过
		// null。不清的话库里会留下 1995-02-31，而 p_birth_day_chk 会直接
		// 挡住这次写入，用户看到的是一句数据库约束错误。
		if p.BirthDay != nil && !validDay(*p.BirthYM, *p.BirthDay) {
			p.BirthDay = nil
		}
	}
	if in.BirthDay != nil {
		if err := checkBirthDay(p.BirthYM, *in.BirthDay); err != nil {
			return err
		}
		p.BirthDay = in.BirthDay
	}

	if in.CityCode != nil {
		if !validCityCode(*in.CityCode) {
			return apierr.ErrBadRequest.WithMessage("所在城市不正确")
		}
		p.CityCode = in.CityCode
	}
	if in.HometownCode != nil {
		if !validCityCode(*in.HometownCode) {
			return apierr.ErrBadRequest.WithMessage("家乡城市不正确")
		}
		p.HometownCode = in.HometownCode
	}

	if in.HeightCM != nil {
		if *in.HeightCM < 140 || *in.HeightCM > 210 {
			return apierr.ErrBadRequest.WithMessage("身高需要在 140 到 210 厘米之间")
		}
		p.HeightCM = in.HeightCM
	}
	if in.WeightKG != nil {
		if *in.WeightKG < 35 || *in.WeightKG > 150 {
			return apierr.ErrBadRequest.WithMessage("体重需要在 35 到 150 公斤之间")
		}
		p.WeightKG = in.WeightKG
	}
	if in.EducationLevel != nil {
		if *in.EducationLevel < 1 || *in.EducationLevel > 4 {
			return apierr.ErrBadRequest.WithMessage("学历取值不正确")
		}
		p.EducationLevel = in.EducationLevel
	}

	// school_tier 不接受前端传值：它由服务端按院校库（internal/pkg/schools）
	// 归一。建档页的校名只能从联想列表里选，但后端不依赖这一点 ——
	// PATCH 是公开的接口，直接打接口塞一个任意校名照样会被这里归一。
	//
	// 清空校名要显式把 tier 置回 NULL：只改 school_name 的话，
	// 用户删掉学校之后 tier 还留着上一次的值，档案会继续按 985 参与筛选。
	if in.SchoolName != nil {
		v, err := shortText(*in.SchoolName, "毕业院校")
		if err != nil {
			return err
		}
		p.SchoolName = v
		if v == "" {
			p.SchoolTier = nil
		} else {
			tier := schools.TierOf(v)
			p.SchoolTier = &tier
		}
	}
	if in.Occupation != nil {
		v, err := shortText(*in.Occupation, "职业")
		if err != nil {
			return err
		}
		p.Occupation = v
	}
	if in.Company != nil {
		v, err := shortText(*in.Company, "工作单位")
		if err != nil {
			return err
		}
		p.Company = v
	}

	if in.IncomeBand != nil {
		if *in.IncomeBand < 1 || *in.IncomeBand > 6 {
			return apierr.ErrBadRequest.WithMessage("收入区间取值不正确")
		}
		p.IncomeBand = in.IncomeBand
	}
	if in.Chronotype != nil {
		if *in.Chronotype < 1 || *in.Chronotype > 3 {
			return apierr.ErrBadRequest.WithMessage("作息取值不正确")
		}
		p.Chronotype = in.Chronotype
	}
	if in.Smoking != nil {
		if *in.Smoking < 0 || *in.Smoking > 2 {
			return apierr.ErrBadRequest.WithMessage("吸烟取值不正确")
		}
		p.Smoking = in.Smoking
	}
	if in.Drinking != nil {
		if *in.Drinking < 0 || *in.Drinking > 2 {
			return apierr.ErrBadRequest.WithMessage("饮酒取值不正确")
		}
		p.Drinking = in.Drinking
	}

	if in.WantChild != nil {
		if *in.WantChild < model.WantChildYes || *in.WantChild > model.WantChildTBD {
			return apierr.ErrBadRequest.WithMessage("婚育意愿取值不正确")
		}
		p.WantChild = in.WantChild
	}
	if in.MaritalStatus != nil {
		if *in.MaritalStatus < model.MaritalSingle || *in.MaritalStatus > model.MaritalDivorced {
			return apierr.ErrBadRequest.WithMessage("婚史取值不正确")
		}
		p.MaritalStatus = in.MaritalStatus
	}

	if in.Hobbies != nil {
		tags, err := parseHobbies(*in.Hobbies)
		if err != nil {
			return err
		}
		p.Hobbies = strings.Join(tags, "、")
	}
	if in.Intro != nil {
		v, err := longText(*in.Intro, "自我介绍")
		if err != nil {
			return err
		}
		p.Intro = v
	}
	if in.Expectation != nil {
		v, err := longText(*in.Expectation, "对另一半的期待")
		if err != nil {
			return err
		}
		p.Expectation = v
	}

	return nil
}

// shortText 校验并清理 ≤40 字的短文本。空串合法 —— 清空选填项要能生效。
func shortText(raw, label string) (string, error) {
	v := strings.TrimSpace(raw)
	if len([]rune(v)) > shortTextMax {
		return "", apierr.ErrBadRequest.
			WithMessage(label + "不能超过 40 个字")
	}
	return v, nil
}

// longText 校验并清理 ≤300 字的长文本。
func longText(raw, label string) (string, error) {
	v := strings.TrimSpace(raw)
	if len([]rune(v)) > longTextMax {
		return "", apierr.ErrBadRequest.
			WithMessage(label + "不能超过 300 个字")
	}
	return v, nil
}

// parseHobbies 解析顿号分隔的兴趣标签。
//
// 同时接受中英文顿号与逗号：用户从别处粘贴时经常是逗号，
// 因为一个标点把整段内容拒掉是没必要的摩擦。
func parseHobbies(raw string) ([]string, error) {
	normalized := strings.NewReplacer(",", "、", "，", "、", ";", "、", "；", "、").Replace(raw)
	out := make([]string, 0, maxHobbies)
	seen := make(map[string]bool, maxHobbies)
	for _, p := range strings.Split(normalized, "、") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		if len([]rune(p)) > 8 {
			return nil, apierr.ErrBadRequest.WithMessage("单个兴趣标签不能超过 8 个字")
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) > maxHobbies {
		return nil, apierr.ErrBadRequest.WithMessage("兴趣标签最多 6 个")
	}
	return out, nil
}

// checkBirthYM 校验出生年月，并把年龄卡在 18–60 岁。
//
// 年龄在这里卡而不是只靠前端：它是硬条件过滤的基础，
// 也是「未成年人不得进入婚恋产品」这条合规底线的落点。
func checkBirthYM(ym int) error {
	age, err := ageFromBirthYM(ym)
	if err != nil {
		return err
	}
	if age < 18 {
		return apierr.ErrBadRequest.WithMessage("本产品仅面向 18 岁以上用户")
	}
	if age > 60 {
		return apierr.ErrBadRequest.WithMessage("年龄需要在 18 到 60 岁之间")
	}
	return nil
}

// checkBirthDay 校验「日」在给定的年月里真实存在。
//
// 与 checkBirthYM 分开，是因为两者管的不是一回事：ym 是入池门槛（18–60）
// 的落点，day 不参与任何筛选与打分，只影响展示。所以这里不重复判年龄 ——
// 1995-02-29 被拒是因为那一天不存在，不是因为他年龄不对。
func checkBirthDay(ym *int, day int16) error {
	if ym == nil {
		return apierr.ErrBadRequest.WithMessage("请先选择出生年月")
	}
	if !validDay(*ym, day) {
		return apierr.ErrBadRequest.WithMessage("出生日期不正确")
	}
	return nil
}

// validDay 判断 day 是不是该年该月里真实存在的一天。
// 年月本身非法时一律返回 false。
func validDay(ym int, day int16) bool {
	year, month := ym/100, ym%100
	if year < 1900 || year > 2999 || month < 1 || month > 12 {
		return false
	}
	return day >= 1 && int(day) <= daysInMonth(year, month)
}

// daysInMonth 返回该年该月的天数。
func daysInMonth(year, month int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		// 四年一闰、百年不闰、四百年再闰
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 31
	}
}

// requiredMissing 是入池门槛里 profiles 表能表达的那部分：必填 6 项 + 头像。
// 照片数不入这张单子，因为它不在 profiles 里。
//
// 一共 7 项，也是完整度算法里 required 的基数（见 §4.2）。
func requiredMissing(p *model.Profile) []string {
	// 返回空切片而不是 nil：JSON 里是 []，前端 .length 不会炸，
	// 也不必在每处调用点写 ?? []
	missing := []string{}
	add := func(cond bool, name string) {
		if cond {
			missing = append(missing, name)
		}
	}
	add(p.Nickname == nil || *p.Nickname == "", "nickname")
	add(p.Gender == nil, "gender")
	add(p.BirthYM == nil, "birth_ym")
	add(p.CityCode == nil, "city_code")
	add(p.HeightCM == nil, "height_cm")
	add(p.EducationLevel == nil, "education_level")
	add(p.AvatarKey == "", "avatar_key")
	return missing
}

// admissionMissing 是翻 status = active 的全部条件：7 项必填再加照片数。
//
// 与完整度分开：照片不参与 60/40 的完整度计算（§4.2），
// 但它是入池的必要条件。两件事混在一起会让「完整度」这个数字
// 既表示进度又表示门槛，前端没法用它。
func admissionMissing(p *model.Profile, photoCount int) []string {
	missing := requiredMissing(p)
	if photoCount < minPhotosForActive {
		missing = append(missing, "photos")
	}
	return missing
}

// completeness 算 0..100 的资料完整度。不参与匹配打分，只做「可被引荐」的门槛。
// 必填占 60 分，选填占 40 分 —— 选填给足权重，是为了让用户有动力补。
//
// 刻意不收照片数：照片不属于 profiles 表，也不该影响这个数字
// （见 admissionMissing 的说明）。算法是文档 §4.2 的原样实现。
func completeness(p *model.Profile) int {
	const totalRequired = 7 // 6 个标量 + 头像
	filledRequired := totalRequired - len(requiredMissing(p))

	optional := []bool{
		p.HometownCode != nil, p.WeightKG != nil, p.SchoolName != "",
		p.Occupation != "", p.Company != "", p.IncomeBand != nil,
		p.Chronotype != nil, p.Smoking != nil, p.Drinking != nil,
		p.Hobbies != "", p.Intro != "", p.Expectation != "",
	}
	filledOptional := 0
	for _, ok := range optional {
		if ok {
			filledOptional++
		}
	}

	score := float64(filledRequired)/float64(totalRequired)*60 +
		float64(filledOptional)/float64(len(optional))*40
	return int(score + 0.5)
}

func (s *Service) buildView(user *model.User, p *model.Profile, photoCount int) *ProfileView {
	v := &ProfileView{
		UserID:         user.ID,
		Status:         user.Status,
		NextStep:       NextStep(user.Status),
		Completeness:   p.Completeness,
		MissingFields:  admissionMissing(p, photoCount),
		Nickname:       p.Nickname,
		Gender:         p.Gender,
		BirthYM:        p.BirthYM,
		BirthDay:       p.BirthDay,
		CityCode:       p.CityCode,
		HeightCM:       p.HeightCM,
		EducationLevel: p.EducationLevel,
		HometownCode:   p.HometownCode,
		WeightKG:       p.WeightKG,
		SchoolName:     p.SchoolName,
		Occupation:     p.Occupation,
		Company:        p.Company,
		IncomeBand:     p.IncomeBand,
		Chronotype:     p.Chronotype,
		Smoking:        p.Smoking,
		Drinking:       p.Drinking,
		Hobbies:        splitTags(p.Hobbies),
		Intro:          p.Intro,
		Expectation:    p.Expectation,

		WantChild:     p.WantChild,
		MaritalStatus: p.MaritalStatus,

		AvatarKey:  p.AvatarKey,
		AvatarURL:  s.ImgURL(p.AvatarKey, storage.VariantCard),
		PhotoCount: photoCount,
	}
	if p.BirthYM != nil {
		if age, err := ageFromBirthYM(*p.BirthYM); err == nil {
			v.Age = &age
		}
	}
	return v
}

// NextStep 告诉前端下一步去哪。onboarding 之外一律 home ——
// M1 只有首页，引荐列表在 M2。
//
// 导出是因为 handler 的 /me 也要用它，而它必须只有一个实现：
// 登录接口和 /me 给出不同的下一步，前端会在两个页面之间弹跳。
func NextStep(status string) string {
	if status == model.StatusOnboarding {
		return StepOnboarding
	}
	return StepHome
}

// ageFromBirthYM 由 YYYYMM 算周岁。生日当月即算长一岁 ——
// 精确到日的周龄要存完整生日，而入池门槛只需要年龄区间。
func ageFromBirthYM(ym int) (int, error) {
	year, month := ym/100, ym%100
	if year < 1900 || year > 2999 || month < 1 || month > 12 {
		return 0, apierr.ErrBadRequest.WithMessage("出生年月格式应为 YYYYMM")
	}
	now := time.Now()
	age := now.Year() - year
	if int(now.Month()) < month {
		age--
	}
	return age, nil
}

// validCityCode 校验国标行政区划代码（6 位）。
// 只做位数与范围校验，不比对完整码表 —— 前端用的是标准三级联动
// 数据源，真正的错值只会来自手动构造的请求。
//
// 上界从 659999 放到 829999，是因为建档页的城市改成了全国省市联动，
// 港澳台（710000/810000/820000）也在里面、且各自只有省级一条 ——
// 上游的民政部数据里它们没有地级条目，码本身就落在旧上界之外。
// 不放宽的话，选香港会被这一行静默拒掉，而前端只看到一句
// 「所在城市不正确」；测试池子里永远碰不到。
func validCityCode(code int) bool {
	return code >= 110000 && code <= 829999
}

// looksLikeContact 粗略识别昵称里的联系方式。
//
// 资料文本里的联系方式是「识别但不拦截」，昵称是唯一的例外 ——
// 昵称出现在卡片正面，是引流成本最低的位置。
func looksLikeContact(s string) bool {
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits >= 8 {
		return true
	}
	lower := strings.ToLower(s)
	for _, kw := range []string{"微信", "vx", "wx", "qq", "weixin", "telegram"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// splitTags 把顿号分隔的字符串拆成去空后的标签数组。
// 返回空切片而不是 nil：JSON 里是 []，前端 .length 不会炸。
func splitTags(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, "、")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) photoCount(ctx context.Context, userID int64) int {
	var n int64
	if err := s.Repo.DB.WithContext(ctx).Model(&model.Photo{}).
		Where("user_id = ?", userID).Count(&n).Error; err != nil {
		s.Log.Warn("统计照片数失败", "err", err, "uid", userID)
		return 0
	}
	return int(n)
}

// profileSnapshotData 是资料快照，存所有可被审核拒绝后回退的文本字段。
// 数值与枚举字段不进快照：它们没有审核路径，回退时也不需要还原。
type profileSnapshotData struct {
	Nickname    *string `json:"nickname"`
	SchoolName  string  `json:"school_name"`
	Occupation  string  `json:"occupation"`
	Company     string  `json:"company"`
	Hobbies     string  `json:"hobbies"`
	Intro       string  `json:"intro"`
	Expectation string  `json:"expectation"`
}

func snapshotOf(p *model.Profile) ([]byte, error) {
	return json.Marshal(profileSnapshotData{
		Nickname:    p.Nickname,
		SchoolName:  p.SchoolName,
		Occupation:  p.Occupation,
		Company:     p.Company,
		Hobbies:     p.Hobbies,
		Intro:       p.Intro,
		Expectation: p.Expectation,
	})
}

// textSig 是文本字段的签名，用来判断一次提交是否真的改动了文本。
type textSig struct {
	Nickname    string
	SchoolName  string
	Occupation  string
	Company     string
	Hobbies     string
	Intro       string
	Expectation string
}

func textSigOf(p *model.Profile) textSig {
	sig := textSig{
		SchoolName:  p.SchoolName,
		Occupation:  p.Occupation,
		Company:     p.Company,
		Hobbies:     p.Hobbies,
		Intro:       p.Intro,
		Expectation: p.Expectation,
	}
	if p.Nickname != nil {
		sig.Nickname = *p.Nickname
	}
	return sig
}

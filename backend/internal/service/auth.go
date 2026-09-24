package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
	"github.com/davisu-china/tidings/backend/internal/pkg/password"
)

// 一次性邮箱域名黑名单。邮箱不验证之后，这是注册侧仅剩的一道自动门槛。
//
// 不求全，挡掉最常见的即可 —— 真要防批量注册还得靠行为风控。
// 按域名而不是完整邮箱匹配，所以不会误伤正常用户。
var disposableDomains = map[string]bool{
	"mailinator.com": true, "guerrillamail.com": true, "10minutemail.com": true,
	"tempmail.com": true, "temp-mail.org": true, "throwawaymail.com": true,
	"yopmail.com": true, "trashmail.com": true, "sharklasers.com": true,
	"getnada.com": true, "maildrop.cc": true, "mailnesia.com": true,
	"dispostable.com": true, "fakeinbox.com": true, "tempinbox.com": true,
}

// maxRegistersPerIPPerDay 是单 IP 每日注册上限。教师办公室、公司、
// 运营商 NAT 后面可能坐着多个真实用户，所以这个数字不能压得太低 ——
// 宁可漏掉一些批量注册，也不要让同办公室的人互相挡住。
const maxRegistersPerIPPerDay = 10

// TokenPair 是签发给客户端的一组凭据。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access token 剩余秒数
}

// LoginResult 是注册/登录的返回。
type LoginResult struct {
	TokenPair
	UserID int64 `json:"user_id"`
	// NextStep 告诉前端该去哪：onboarding（未建档）或 home。
	NextStep string `json:"next_step"`
	IsNew    bool   `json:"is_new_user"`
}

const (
	StepOnboarding = "onboarding"
	StepHome       = "home"
)

// Register 邮箱 + 密码建号。
//
// 邮箱已占用时返回 EMAIL_TAKEN（409）—— 这是绕不开的泄露：不告诉用户
// 邮箱被占了，他就不知道该怎么办。压住枚举速度靠 IP 限流（见 §4.1）。
func (s *Service) Register(ctx context.Context, rawEmail, plainPassword, ip string) (*LoginResult, error) {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return nil, err
	}
	if err := s.checkPassword(plainPassword); err != nil {
		return nil, err
	}
	if err := s.checkRegisterIP(ctx, ip); err != nil {
		return nil, err
	}

	hash, err := password.Hash(plainPassword, s.Cfg.Auth.BcryptCost)
	if err != nil {
		s.Log.Error("生成密码哈希失败", "err", err)
		return nil, apierr.ErrInternal
	}

	var user model.User
	err = s.Repo.Tx(func(tx *gorm.DB) error {
		u := model.User{Email: email, PasswordHash: hash, Status: model.StatusOnboarding}
		// 这里不能用 OnConflict{DoNothing}：那样「邮箱被抢注」与
		// 「注册成功」都是 RowsAffected = 0，两者区分不开，
		// 会退化成对已有账号的静默登录。让唯一约束报错，再翻译。
		if err := tx.Create(&u).Error; err != nil {
			if isUniqueViolation(err) {
				return apierr.ErrEmailTaken
			}
			return err
		}
		// profile 与 user 一比一，建号时就把空行建好，
		// 后续建档向导每一步都是 UPDATE，不用处理「行还不存在」的分支
		if err := tx.Create(&model.Profile{UserID: u.ID}).Error; err != nil {
			return err
		}
		user = u
		return nil
	})
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		s.Log.Error("创建用户失败", "err", err, "email", maskEmail(email))
		return nil, apierr.ErrInternal
	}

	// 注册成功后直接签发凭据，不让用户再填一遍刚填过的邮箱密码
	pair, err := s.issueTokens(ctx, &user)
	if err != nil {
		return nil, err
	}
	s.touchActive(ctx, user.ID)
	if err := s.bumpRegisterIP(ctx, ip); err != nil {
		s.Log.Warn("注册 IP 计数失败", "err", err, "ip", ip)
	}

	s.Log.Info("注册成功", "uid", user.ID, "email", maskEmail(email), "ip", ip)
	return &LoginResult{TokenPair: *pair, UserID: user.ID, NextStep: StepOnboarding, IsNew: true}, nil
}

// Login 邮箱 + 密码登录。
//
// 「邮箱不存在」与「密码错误」返回同一句话，且邮箱不存在时也走一次
// bcrypt 比对 —— 否则响应耗时的差异就能枚举出哪些邮箱注册过。
func (s *Service) Login(ctx context.Context, rawEmail, plainPassword, ip string) (*LoginResult, error) {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		// 邮箱格式不对也要烧掉一次比对的时间，保持三条失败路径耗时一致
		s.Equalizer.Burn(plainPassword)
		return nil, apierr.ErrInvalidCredentials
	}

	if err := s.checkLoginLock(ctx, email); err != nil {
		return nil, err
	}

	var user model.User
	err = s.Repo.DB.WithContext(ctx).Where("lower(email) = ?", email).First(&user).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Log.Error("查询用户失败", "err", err, "email", maskEmail(email))
			return nil, apierr.ErrInternal
		}
		s.Equalizer.Burn(plainPassword)
		if s.noteLoginFailure(ctx, email, ip) {
			return nil, lockedErr(s.Cfg.Auth.LockDuration)
		}
		return nil, apierr.ErrInvalidCredentials
	}

	if !password.Verify(user.PasswordHash, plainPassword) {
		if s.noteLoginFailure(ctx, email, ip) {
			return nil, lockedErr(s.Cfg.Auth.LockDuration)
		}
		return nil, apierr.ErrInvalidCredentials
	}
	s.clearLoginFailure(ctx, email)

	switch user.Status {
	case model.StatusBanned:
		return nil, apierr.ErrAccountBanned
	case model.StatusUnderReview:
		// 复核中仍允许登录 —— 让用户能看到自己的资料与申诉入口，
		// 直接踢出登录会让误封用户完全失联。能不能用别的功能由
		// RequireActive 那一级拦。
	case model.StatusDeactivated:
		return nil, apierr.ErrForbidden.WithMessage("账号已注销，如需恢复请联系客服")
	}

	pair, err := s.issueTokens(ctx, &user)
	if err != nil {
		return nil, err
	}
	s.touchActive(ctx, user.ID)

	s.Log.Info("登录成功", "uid", user.ID)
	return &LoginResult{
		TokenPair: *pair,
		UserID:    user.ID,
		NextStep:  NextStep(user.Status),
	}, nil
}

// Refresh 用 refresh token 换一组新凭据，并轮换 refresh token。
//
// 轮换而非复用：refresh token 生命周期长达 90 天，一旦泄露危害远大于
// access token。每次使用即失效，能把泄露窗口压到一次。
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.JWT.Parse(refreshToken, jwtutil.TypeRefresh)
	if err != nil {
		if errors.Is(err, jwtutil.ErrExpiredToken) {
			return nil, apierr.ErrTokenExpired
		}
		return nil, apierr.ErrUnauthorized
	}

	// 白名单校验：登出和轮换都靠它，光验签是不够的。
	// 用 Del 而不是 Get：这个 jti 无论后续成功与否都该失效，
	// 否则同一个 refresh token 可以被重放，反复换出新凭据。
	ok, err := s.Repo.Redis.Del(ctx, keyRefresh(claims.UserID, claims.ID)).Result()
	if err != nil {
		s.Log.Error("读取 refresh 白名单失败", "err", err, "uid", claims.UserID)
		return nil, apierr.ErrInternal
	}
	if ok == 0 {
		return nil, apierr.ErrUnauthorized.WithMessage("登录状态已失效，请重新登录")
	}

	var user model.User
	if err := s.Repo.DB.WithContext(ctx).First(&user, claims.UserID).Error; err != nil {
		return nil, apierr.ErrUnauthorized
	}
	if user.TokenVersion != claims.TokenVersion {
		return nil, apierr.ErrUnauthorized.WithMessage("登录状态已失效，请重新登录")
	}
	if user.Status == model.StatusBanned {
		return nil, apierr.ErrAccountBanned
	}

	return s.issueTokens(ctx, &user)
}

// Logout 让 refresh token 立即失效。access token 因为是无状态的，
// 会自然过期；需要立刻全端下线时用 token_version 递增。
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	claims, err := s.JWT.Parse(refreshToken, jwtutil.TypeRefresh)
	if err != nil {
		// 登出对无效 token 也返回成功：客户端的目标是清掉本地状态，
		// 这时报错只会让它陷入无法登出的死循环。
		return nil
	}
	if err := s.Repo.Redis.Del(ctx, keyRefresh(claims.UserID, claims.ID)).Err(); err != nil {
		s.Log.Warn("删除 refresh 白名单失败", "err", err, "uid", claims.UserID)
	}
	return nil
}

// LoadUser 供鉴权中间件使用。每次都回查 users 行，不加缓存：
// 需要它的 status 与 token_version，过早缓存会让封禁生效变得不可预测。
func (s *Service) LoadUser(ctx context.Context, userID int64) (*model.User, error) {
	var user model.User
	if err := s.Repo.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierr.ErrUnauthorized
		}
		return nil, apierr.ErrInternal
	}
	return &user, nil
}

func (s *Service) issueTokens(ctx context.Context, user *model.User) (*TokenPair, error) {
	access, _, _, err := s.JWT.Issue(user.ID, user.TokenVersion, jwtutil.TypeAccess)
	if err != nil {
		s.Log.Error("签发 access token 失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	refresh, jti, _, err := s.JWT.Issue(user.ID, user.TokenVersion, jwtutil.TypeRefresh)
	if err != nil {
		s.Log.Error("签发 refresh token 失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	if err := s.Repo.Redis.Set(ctx, keyRefresh(user.ID, jti), "1", s.JWT.RefreshTTL()).Err(); err != nil {
		s.Log.Error("写入 refresh 白名单失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.JWT.AccessTTL().Seconds()),
	}, nil
}

// touchActive 更新最后活跃时间。它只影响匹配打分里的活跃度因子，
// 写失败不该挡住登录。
func (s *Service) touchActive(ctx context.Context, userID int64) {
	if err := s.Repo.DB.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("last_active_at", time.Now().UTC()).Error; err != nil {
		s.Log.Warn("更新最后活跃时间失败", "err", err, "uid", userID)
	}
}

// checkPassword 校验密码并把底层错误翻成带提示的业务错误。
func (s *Service) checkPassword(plain string) error {
	err := password.Validate(plain, s.Cfg.Auth.MinPasswordLen)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, password.ErrTooLong):
		return apierr.ErrPasswordTooLong
	case errors.Is(err, password.ErrEmpty):
		return apierr.ErrWeakPassword.WithMessage("请填写密码")
	default:
		return apierr.ErrWeakPassword.
			WithMessage(fmt.Sprintf("密码至少 %d 位", s.Cfg.Auth.MinPasswordLen))
	}
}

func keyRefresh(userID int64, jti string) string {
	return fmt.Sprintf("auth:rt:%d:%s", userID, jti)
}

func keyFail(email string) string { return "auth:fail:" + email }
func keyLock(email string) string { return "auth:lock:" + email }
func keyFailIP(ip string) string  { return "auth:fail:ip:" + ip }
func keyRegIP(ip string) string   { return "auth:reg:ip:" + ip }

// noteLoginFailure 记一次密码错误。返回值表示这一击是否触发了锁定，
// 调用方据此直接告诉用户「被锁了」—— 让用户在下一次尝试才知道自己被锁，
// 只会让他再白试一次。
//
// 按邮箱与 IP 两个维度分别计：只按邮箱计的话，攻击者遍历一份邮箱列表
// 逐个试错，就能把全站账号锁死 —— 那是把防护变成了拒绝服务。
func (s *Service) noteLoginFailure(ctx context.Context, email, ip string) bool {
	lockFor := s.Cfg.Auth.LockDuration
	locked := false

	// 邮箱维度：达到上限就设锁
	n, err := s.Repo.Redis.Incr(ctx, keyFail(email)).Result()
	if err != nil {
		s.Log.Warn("累加登录失败计数失败", "err", err)
	} else {
		// ExpireNX：只在键还没有 TTL 时设置，否则每错一次窗口就往后推，
		// 30 分钟会变成一个永远到不了的滑动窗口 —— 慢速撞库（每 29 分钟
		// 试一次）就永远触发不了锁定。注册的 IP 日限用的是同一套写法。
		s.Repo.Redis.ExpireNX(ctx, keyFail(email), lockFor)
		if n >= int64(s.Cfg.Auth.MaxAttempts) {
			if err := s.Repo.Redis.Set(ctx, keyLock(email), "1", lockFor).Err(); err != nil {
				s.Log.Warn("写入登录锁失败", "err", err)
			}
			// 锁已落下，失败计数清零，避免解锁瞬间又被旧计数顶回去
			s.Repo.Redis.Del(ctx, keyFail(email))
			s.Log.Warn("账号连续密码错误已锁定", "email", maskEmail(email), "attempts", n)
			locked = true
		}
	}

	// IP 维度：只计数不设锁。这个计数是给运营看的，
	// 也是将来加严时唯一有历史数据可依据的信号。
	if ip == "" {
		return locked
	}
	if err := s.Repo.Redis.Incr(ctx, keyFailIP(ip)).Err(); err != nil {
		s.Log.Warn("累加 IP 失败计数失败", "err", err, "ip", ip)
		return locked
	}
	s.Repo.Redis.ExpireNX(ctx, keyFailIP(ip), lockFor)
	return locked
}

func (s *Service) clearLoginFailure(ctx context.Context, email string) {
	s.Repo.Redis.Del(ctx, keyFail(email))
}

// checkLoginLock 在比对密码之前拦掉已锁定的账号。
// 锁定期间不比对密码：省下 bcrypt 的开销，也避免用响应反推密码对错。
func (s *Service) checkLoginLock(ctx context.Context, email string) error {
	ttl, err := s.Repo.Redis.TTL(ctx, keyLock(email)).Result()
	if err != nil {
		s.Log.Error("读取登录锁失败", "err", err, "email", maskEmail(email))
		return apierr.ErrInternal
	}
	if ttl > 0 {
		// 用剩余 TTL 而不是配置时长：用户在锁定期的第 20 分钟来试，
		// 提示「再等 30 分钟」会让他以为越试越久
		return lockedErr(ttl)
	}
	return nil
}

// lockedErr 把剩余锁定时长写进提示与 Retry-After。
//
// 文案不写死「30 分钟」：LockDuration 是配置项，写死之后一改配置
// 就会出现「提示 30 分钟、实际锁 10 分钟」这种用户一眼能看出不对的话。
func lockedErr(d time.Duration) *apierr.Error {
	mins := int(d.Minutes() + 0.5)
	if mins < 1 {
		mins = 1
	}
	return apierr.ErrAccountLocked.
		WithMessage(fmt.Sprintf("密码错误次数过多，请 %d 分钟后再试", mins)).
		WithRetryAfter(int(d.Seconds()) + 1)
}

// checkRegisterIP 限制单 IP 每日注册量。邮箱不验证之后，这是唯一
// 能拖慢批量注册的地方。
func (s *Service) checkRegisterIP(ctx context.Context, ip string) error {
	if ip == "" {
		return nil
	}
	n, err := s.Repo.Redis.Get(ctx, keyRegIP(ip)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		// 读不到就放行：Redis 抖一下不该挡住真实用户注册
		s.Log.Warn("读取注册 IP 计数失败", "err", err, "ip", ip)
		return nil
	}
	if n >= maxRegistersPerIPPerDay {
		return apierr.ErrRateLimited.WithMessage("当前网络注册过于频繁，请稍后再试")
	}
	return nil
}

func (s *Service) bumpRegisterIP(ctx context.Context, ip string) error {
	if ip == "" {
		return nil
	}
	key := keyRegIP(ip)
	pipe := s.Repo.Redis.TxPipeline()
	pipe.Incr(ctx, key)
	// ExpireNX：只在键还没有 TTL 时设置，否则每次注册都会把窗口往后推，
	// 日限会变成一个永远到不了的滑动窗口。
	pipe.ExpireNX(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// normalizeEmail 校验并归一化邮箱：小写、去空白。
// 归一化很重要 —— 不然 Foo@x.com 和 foo@x.com 会变成两个账号。
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", apierr.ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", apierr.ErrInvalidEmail
	}
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return "", apierr.ErrInvalidEmail
	}
	if disposableDomains[email[at+1:]] {
		return "", apierr.ErrEmailRejected
	}
	return email, nil
}

// maskEmail 打日志用。邮箱是个人信息，不该整个写进日志。
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return "***"
	}
	local := email[:at]
	if len(local) <= 2 {
		return local[:1] + "***" + email[at:]
	}
	return local[:2] + "***" + email[at:]
}

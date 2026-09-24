// Package password 封装 bcrypt 哈希与密码强度校验。
//
// MVP 没有验证码、没有找回密码、没有邮箱验证 —— 密码是账号唯一的
// 持有凭据，所以这里的两个决定都很保守：哈希用 bcrypt（不是 SHA 系列），
// 长度上限卡在 bcrypt 自己的 72 字节硬上限上并显式报错。
package password

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// MaxLen 是 bcrypt 的硬上限。超过 72 字节的部分会被 bcrypt 静默截断 ——
// 那意味着「前 72 字节相同」的两个密码等价，用户以为改长了其实没改。
// 所以这里显式拒绝，而不是让它悄悄发生。
const MaxLen = 72

// Hash 生成 bcrypt 哈希。cost 由配置传入，启动时已断言在 10–15。
func Hash(plain string, cost int) (string, error) {
	if err := Validate(plain, 1); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(h), nil
}

// Verify 比对明文与哈希。hash 为空串时必然失败（见迁移文件里
// password_hash 的 DEFAULT ” 说明），调用方不需要特判。
func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

var (
	ErrTooShort = errors.New("密码太短")
	ErrTooLong  = errors.New("密码太长")
	ErrEmpty    = errors.New("密码为空")
)

// Validate 校验密码强度。只限长度，不强制字符类别 ——
// 复杂度规则会把用户逼向 Passw0rd! 这种更难记又没更强的密码。
func Validate(plain string, minLen int) error {
	if plain == "" {
		return ErrEmpty
	}
	// 按字节数算上限（bcrypt 的口径），按字符数算下限（用户的口径）
	if len(plain) > MaxLen {
		return ErrTooLong
	}
	if utf8.RuneCountInString(plain) < minLen {
		return ErrTooShort
	}
	return nil
}

// Equalizer 抹平「邮箱不存在」这条分支的耗时。
//
// 不比对直接返回，和跑一次 bcrypt 再返回，差着两三百毫秒 ——
// 这个时序差足以枚举出哪些邮箱注册过，那就把 INVALID_CREDENTIALS
// 那句统一提示白写了。
//
// 假哈希必须是真生成出来的，且 cost 与真实哈希一致：写死一个字符串
// 的话 bcrypt 会在解析阶段就报错返回，反而更快，等于把时序差放大。
type Equalizer struct {
	dummy []byte
}

// NewEqualizer 生成一个与真实哈希同 cost 的假哈希。
// 密码取自 crypto/rand，没有对应的明文，因此不可能被撞上。
func NewEqualizer(cost int) (*Equalizer, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("生成假哈希随机数失败: %w", err)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(base64.StdEncoding.EncodeToString(buf)), cost)
	if err != nil {
		return nil, fmt.Errorf("生成假哈希失败: %w", err)
	}
	return &Equalizer{dummy: h}, nil
}

// Burn 跑一次注定失败的比对，耗时与真实比对相当。返回值恒为 false，
// 调用方不要依赖它，它只是为了消耗时间。
func (e *Equalizer) Burn(plain string) bool {
	if e == nil {
		return false
	}
	return bcrypt.CompareHashAndPassword(e.dummy, []byte(plain)) == nil
}

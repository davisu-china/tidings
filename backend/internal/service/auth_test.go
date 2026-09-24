package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
)

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr *apierr.Error
	}{
		{"正常邮箱", "user@example.com", "user@example.com", nil},
		// 大小写不归一的话 Foo@x.com 和 foo@x.com 会变成两个账号
		{"大写归一", "User@Example.COM", "user@example.com", nil},
		{"去空白", "  user@example.com  ", "user@example.com", nil},
		{"加号别名保留", "user+tag@example.com", "user+tag@example.com", nil},
		{"中文域名", "user@例子.中国", "user@例子.中国", nil},

		{"空", "", "", apierr.ErrInvalidEmail},
		{"没有 @", "userexample.com", "", apierr.ErrInvalidEmail},
		{"没有域名", "user@", "", apierr.ErrInvalidEmail},
		{"没有用户名", "@example.com", "", apierr.ErrInvalidEmail},
		{"带显示名", "张三 <user@example.com>", "", apierr.ErrInvalidEmail},
		{"带空格", "user name@example.com", "", apierr.ErrInvalidEmail},
		{"超长", strings.Repeat("a", 250) + "@example.com", "", apierr.ErrInvalidEmail},

		{"一次性邮箱", "x@mailinator.com", "", apierr.ErrEmailRejected},
		{"一次性邮箱子域不匹配", "x@sub.mailinator.com", "x@sub.mailinator.com", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeEmail(tc.raw)
			if tc.wantErr != nil {
				var apiErr *apierr.Error
				if !errors.As(err, &apiErr) || apiErr.Code != tc.wantErr.Code {
					t.Fatalf("得到 %v，期望 %s", err, tc.wantErr.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got != tc.want {
				t.Fatalf("得到 %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestNormalizeEmailDisposableListIsLowercase 是个防回归的小锁：
// 域名比对发生在 lowercase 之后的邮箱上，往里加规则时必须全小写，
// 否则那条规则永远不会命中，而且不会有任何报错提示。
func TestNormalizeEmailDisposableListIsLowercase(t *testing.T) {
	for domain := range disposableDomains {
		if domain != strings.ToLower(domain) {
			t.Fatalf("黑名单域名 %q 含有大写字母，永远匹配不上", domain)
		}
	}
}

// TestMaskEmail 保证日志里不会出现完整邮箱（个人信息）。
func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"user@example.com": "us***@example.com",
		"ab@example.com":   "a***@example.com",
		"a@example.com":    "***",
		"":                 "***",
		"not-an-email":     "***",
	}
	for in, want := range cases {
		if got := maskEmail(in); got != want {
			t.Fatalf("maskEmail(%q) = %q，期望 %q", in, got, want)
		}
	}

	// 无论怎么切，本地部分的核心字符都不该原样出现在日志里
	for _, in := range []string{"alice@example.com", "bob@x.cn"} {
		masked := maskEmail(in)
		local := in[:strings.LastIndex(in, "@")]
		if len(local) > 2 && strings.Contains(masked, local) {
			t.Fatalf("maskEmail(%q) = %q 泄露了完整本地部分", in, masked)
		}
	}
}

// TestLooksLikeContact 只用来判断昵称。资料正文里的联系方式是
// 「识别但不拦截」，昵称是唯一会被直接拒绝的位置 ——
// 它出现在引荐卡正面，是引流成本最低的地方。
func TestLooksLikeContact(t *testing.T) {
	cases := map[string]bool{
		"阿信":        false,
		"信1234":     false,
		"信12345678": true, // 8 位数字起判定为联系方式
		"微信abc":     true,
		"VX abc":    true,
		"QQ 123":    true,
		"weixin":    true,
		"telegram":  true,
		"夜航船":       false,
	}
	for in, want := range cases {
		if got := looksLikeContact(in); got != want {
			t.Fatalf("looksLikeContact(%q) = %v，期望 %v", in, got, want)
		}
	}
}

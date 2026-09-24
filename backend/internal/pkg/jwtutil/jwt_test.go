package jwtutil

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/davisu-china/tidings/backend/internal/config"
)

func testManager(accessTTL, refreshTTL time.Duration) *Manager {
	return NewManager(config.JWTConfig{
		Secret:     strings.Repeat("s", 32),
		AccessTTL:  accessTTL,
		RefreshTTL: refreshTTL,
		Issuer:     "tidings-test",
	})
}

func TestIssueAndParse(t *testing.T) {
	m := testManager(time.Hour, 24*time.Hour)

	token, jti, expiresAt, err := m.Issue(42, 7, TypeAccess)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if jti == "" {
		t.Fatal("jti 不能为空 —— refresh 白名单靠它做吊销")
	}
	if time.Until(expiresAt) < 59*time.Minute {
		t.Fatalf("过期时间不对: %v", expiresAt)
	}

	claims, err := m.Parse(token, TypeAccess)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.UserID != 42 || claims.TokenVersion != 7 {
		t.Fatalf("claims 不对: %+v", claims)
	}
}

// TestParseRejectsWrongType 是这套 token 最关键的一条不变式：
// refresh token 生命周期长达 90 天，如果能当 access token 用，
// 等于把一次泄露的危害放大到三个月。
func TestParseRejectsWrongType(t *testing.T) {
	m := testManager(time.Hour, 24*time.Hour)

	refresh, _, _, err := m.Issue(1, 0, TypeRefresh)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Parse(refresh, TypeAccess); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("refresh 不该被当作 access 接受，得到 %v", err)
	}

	access, _, _, err := m.Issue(1, 0, TypeAccess)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Parse(access, TypeRefresh); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("access 不该被当作 refresh 接受，得到 %v", err)
	}
}

func TestParseRejectsExpired(t *testing.T) {
	m := testManager(-time.Minute, -time.Minute)

	token, _, _, err := m.Issue(1, 0, TypeAccess)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Parse(token, TypeAccess); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("过期 token 应返回 ErrExpiredToken，得到 %v", err)
	}
}

func TestParseRejectsOtherSecret(t *testing.T) {
	issuer := testManager(time.Hour, time.Hour)
	token, _, _, err := issuer.Issue(1, 0, TypeAccess)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	other := NewManager(config.JWTConfig{
		Secret:    strings.Repeat("t", 32),
		AccessTTL: time.Hour, RefreshTTL: time.Hour, Issuer: "tidings-test",
	})
	if _, err := other.Parse(token, TypeAccess); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("换了密钥就该验不过，得到 %v", err)
	}
}

// TestParseRejectsOtherIssuer 挡住跨环境串用：
// 测试环境和生产用了同一个密钥时，issuer 是唯一还能区分它们的东西。
func TestParseRejectsOtherIssuer(t *testing.T) {
	m := testManager(time.Hour, time.Hour)
	token, _, _, err := m.Issue(1, 0, TypeAccess)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	other := NewManager(config.JWTConfig{
		Secret:    strings.Repeat("s", 32),
		AccessTTL: time.Hour, RefreshTTL: time.Hour, Issuer: "someone-else",
	})
	if _, err := other.Parse(token, TypeAccess); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("issuer 不匹配就该验不过，得到 %v", err)
	}
}

// TestParseRejectsAlgNone 是算法混淆攻击的经典场景：
// 攻击者把 alg 改成 none 并去掉签名，期望服务端跳过验签。
func TestParseRejectsAlgNone(t *testing.T) {
	m := testManager(time.Hour, time.Hour)

	// {"alg":"none","typ":"JWT"} . {"uid":1,"ver":0,"typ":"access","iss":"tidings-test"} . 空签名
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJ1aWQiOjEsInZlciI6MCwidHlwIjoiYWNjZXNzIiwiaXNzIjoidGlkaW5ncy10ZXN0In0."

	if _, err := m.Parse(forged, TypeAccess); err == nil {
		t.Fatal("alg=none 的伪造 token 不该通过")
	}
}

func TestIssueRejectsUnknownType(t *testing.T) {
	m := testManager(time.Hour, time.Hour)
	if _, _, _, err := m.Issue(1, 0, TokenType("nope")); err == nil {
		t.Fatal("未知 token 类型应当报错")
	}
}

func TestWSTicketIsShortLived(t *testing.T) {
	m := testManager(time.Hour, 24*time.Hour)
	_, _, expiresAt, err := m.Issue(1, 0, TypeWSTicket)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	// 票据出现在 URL 与日志里，必须短命
	if d := time.Until(expiresAt); d > 2*time.Minute || d <= 0 {
		t.Fatalf("ws 票据有效期应为 1 分钟，得到 %v", d)
	}
}

func TestTTLAccessors(t *testing.T) {
	m := testManager(15*time.Minute, 90*24*time.Hour)
	if m.AccessTTL() != 15*time.Minute {
		t.Fatalf("AccessTTL = %v", m.AccessTTL())
	}
	// refresh 白名单的 Redis TTL 直接取自这里，取错会让登出提前失效
	if m.RefreshTTL() != 90*24*time.Hour {
		t.Fatalf("RefreshTTL = %v", m.RefreshTTL())
	}
}

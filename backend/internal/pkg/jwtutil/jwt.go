// Package jwtutil 负责 access / refresh token 的签发与校验。
//
// token_version 的用途：users.token_version 递增即可让该用户已签发的
// 所有 token 立即失效，用于封禁、改密与全端下线（见方案 §17.1）。
package jwtutil

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/davisu-china/tidings/backend/internal/config"
)

var (
	ErrInvalidToken = errors.New("token 无效")
	ErrExpiredToken = errors.New("token 已过期")
)

type TokenType string

const (
	TypeAccess  TokenType = "access"
	TypeRefresh TokenType = "refresh"
	// TypeWSTicket 是给 WebSocket 握手用的一次性短期票据。
	// 不让长期 access token 出现在 URL 和日志里（M3 用）。
	TypeWSTicket TokenType = "ws"
)

type Claims struct {
	UserID       int64     `json:"uid"`
	TokenVersion int       `json:"ver"`
	Type         TokenType `json:"typ"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewManager(cfg config.JWTConfig) *Manager {
	return &Manager{
		secret:     []byte(cfg.Secret),
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		issuer:     cfg.Issuer,
	}
}

func (m *Manager) Issue(userID int64, tokenVersion int, typ TokenType) (string, string, time.Time, error) {
	var ttl time.Duration
	switch typ {
	case TypeAccess:
		ttl = m.accessTTL
	case TypeRefresh:
		ttl = m.refreshTTL
	case TypeWSTicket:
		ttl = time.Minute
	default:
		return "", "", time.Time{}, fmt.Errorf("未知 token 类型 %q", typ)
	}

	now := time.Now()
	expiresAt := now.Add(ttl)
	// jti 让 refresh token 能被单独吊销（登出、轮换），
	// 而不必等它自然过期或整体递增 token_version。
	jti := uuid.NewString()

	claims := Claims{
		UserID:       userID,
		TokenVersion: tokenVersion,
		Type:         typ,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    m.issuer,
			Subject:   fmt.Sprint(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("签发 token 失败: %w", err)
	}
	return signed, jti, expiresAt, nil
}

// RefreshTTL 供调用方设置白名单过期时间。
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// AccessTTL 供响应体返回 expires_in。
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// Parse 校验签名与有效期，并确认 token 类型符合预期。
// 类型必须校验，否则 refresh token 可以被当作 access token 用。
func (m *Manager) Parse(tokenStr string, expected TokenType) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期的签名算法 %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	if claims.Type != expected {
		return nil, fmt.Errorf("%w: 期望 %q 但拿到 %q", ErrInvalidToken, expected, claims.Type)
	}
	return claims, nil
}

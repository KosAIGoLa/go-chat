package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type contextKey struct{}

type Claims struct {
	TenantID  uint64   `json:"tenant_id"`
	UserID    uint64   `json:"user_id"`
	DeviceID  string   `json:"device_id"`
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"exp"`
}

type TokenService struct {
	secret []byte
	now    func() time.Time
}

func NewTokenService(secret string) *TokenService {
	return &TokenService{secret: []byte(secret), now: time.Now}
}

func (s *TokenService) Issue(principal Principal, ttl time.Duration) (string, error) {
	claims := Claims{TenantID: principal.TenantID, UserID: principal.UserID, DeviceID: principal.DeviceID, Scopes: principal.Scopes, ExpiresAt: s.now().Add(ttl).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.sign(payloadPart)
	return payloadPart + "." + sig, nil
}

func (s *TokenService) Verify(token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Principal{}, errors.New("invalid token format")
	}
	if !hmac.Equal([]byte(parts[1]), []byte(s.sign(parts[0]))) {
		return Principal{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, err
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Principal{}, err
	}
	if claims.ExpiresAt <= s.now().Unix() {
		return Principal{}, errors.New("token expired")
	}
	return Principal{TenantID: claims.TenantID, UserID: claims.UserID, DeviceID: claims.DeviceID, Scopes: claims.Scopes}, nil
}

func (s *TokenService) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

func BearerToken(header string) (string, error) {
	if header == "" {
		return "", errors.New("authorization header is required")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("authorization header must use bearer scheme")
	}
	return parts[1], nil
}

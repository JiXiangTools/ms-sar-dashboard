package service

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type authTestRepository struct {
	admin domain.Admin
}

func (r *authTestRepository) GetAdminByName(_ context.Context, name string) (domain.Admin, error) {
	if name != r.admin.Name {
		return domain.Admin{}, repository.ErrNotFound
	}
	return r.admin, nil
}

func (r *authTestRepository) GetAdminByID(_ context.Context, id int64) (domain.Admin, error) {
	if id != r.admin.ID {
		return domain.Admin{}, repository.ErrNotFound
	}
	return r.admin, nil
}

type authTestRedis struct {
	redis.UniversalClient
	values map[string]string
	err    error
}

func (r *authTestRedis) Get(_ context.Context, key string) *redis.StringCmd {
	if r.err != nil {
		return redis.NewStringResult("", r.err)
	}
	value, ok := r.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (r *authTestRedis) Set(_ context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	if r.err != nil {
		return redis.NewStatusResult("", r.err)
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value.(string)
	return redis.NewStatusResult("OK", nil)
}

func TestAdminLogoutRevokesIssuedJWT(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	admin := domain.Admin{ID: 42, Name: "admin", Nickname: "Admin", LastUpdateTime: now}
	tokens := auth.NewService(config.AuthConfig{
		JWTSecret:      strings.Repeat("x", 32),
		Issuer:         "test",
		AccessTokenTTL: time.Hour,
	})
	cache := &authTestRedis{values: make(map[string]string)}
	service := &AdminAuthService{
		repo:   &authTestRepository{admin: admin},
		tokens: tokens,
		audit:  audit.NewService(nil),
		logger: log.New(io.Discard, "", 0),
		redis:  cache,
		prefix: "dashboard",
	}
	token, err := tokens.IssueAccessToken(admin)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), token); err != nil {
		t.Fatalf("authenticate before logout: %v", err)
	}
	if err := service.Logout(context.Background(), admin, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), token); err == nil {
		t.Fatal("expected token to be rejected after logout")
	}
	if len(cache.values) != 1 {
		t.Fatalf("expected one revocation record, got %#v", cache.values)
	}
	for key := range cache.values {
		if !strings.HasPrefix(key, "dashboard:admin_auth_revoked:") {
			t.Fatalf("unexpected revocation key %q", key)
		}
	}
}

func TestAdminAuthenticationFailsClosedWhenRevocationStoreIsUnavailable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	admin := domain.Admin{ID: 42, Name: "admin", LastUpdateTime: now}
	tokens := auth.NewService(config.AuthConfig{JWTSecret: strings.Repeat("x", 32), Issuer: "test"})
	token, err := tokens.IssueAccessToken(admin)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	service := &AdminAuthService{
		repo:   &authTestRepository{admin: admin},
		tokens: tokens,
		audit:  audit.NewService(nil),
		logger: log.New(io.Discard, "", 0),
		redis:  &authTestRedis{err: errors.New("redis unavailable")},
	}
	if _, err := service.AuthenticateAccessToken(context.Background(), token); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected authentication infrastructure failure, got %v", err)
	}
}

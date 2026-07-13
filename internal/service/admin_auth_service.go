package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type AdminAuthService struct {
	repo               adminAuthRepository
	tokens             *auth.Service
	audit              *audit.Service
	logger             *log.Logger
	redis              redis.UniversalClient
	prefix             string
	revocationRequired bool
}

type AdminAuthSettings struct {
	Redis     redis.UniversalClient
	KeyPrefix string
}

type adminAuthRepository interface {
	GetAdminByName(ctx context.Context, name string) (domain.Admin, error)
	GetAdminByID(ctx context.Context, id int64) (domain.Admin, error)
}

func NewAdminAuthService(repo *repository.Repository, tokens *auth.Service, auditSvc *audit.Service, logger *log.Logger, settings ...AdminAuthSettings) *AdminAuthService {
	var authRepo adminAuthRepository
	if repo != nil {
		authRepo = repo
	}
	service := &AdminAuthService{repo: authRepo, tokens: tokens, audit: auditSvc, logger: logger}
	if len(settings) > 0 {
		service.redis = settings[0].Redis
		service.prefix = strings.TrimSpace(settings[0].KeyPrefix)
		service.revocationRequired = true
	}
	return service
}

func (s *AdminAuthService) Login(ctx context.Context, input LoginInput) (LoginOutput, error) {
	startedAt := time.Now()
	name := normalizeText(input.Name)
	password := input.Password
	if s.repo == nil || s.tokens == nil {
		return LoginOutput{}, apperror.Internal("admin authentication service is not configured", nil)
	}
	if name == "" || strings.TrimSpace(password) == "" {
		recordLoginFailed(ctx, s.audit, s.logger, 0, map[string]any{
			"name":   name,
			"reason": "name_or_password_required",
		})
		return LoginOutput{}, apperror.BadRequest("name and password are required", nil)
	}

	admin, err := s.repo.GetAdminByName(ctx, name)
	if err != nil {
		recordLoginFailed(ctx, s.audit, s.logger, 0, map[string]any{
			"name":   name,
			"reason": "invalid_credentials",
		})
		logx.Warn(s.logger, ctx, startedAt, "admin.login.failed", "admin login failed",
			logx.String("name", name),
			logx.Err(err),
		)
		return LoginOutput{}, apperror.Unauthorized("invalid admin credentials", err)
	}
	if admin.Disabled {
		recordLoginFailed(ctx, s.audit, s.logger, 0, map[string]any{
			"name":   name,
			"reason": "disabled",
		})
		return LoginOutput{}, apperror.Unauthorized("invalid admin credentials", nil)
	}
	if err := s.tokens.ComparePassword(admin.Password, password); err != nil {
		recordLoginFailed(ctx, s.audit, s.logger, 0, map[string]any{
			"name":   name,
			"reason": "invalid_credentials",
		})
		logx.Warn(s.logger, ctx, startedAt, "admin.login.failed", "admin login failed",
			logx.String("name", name),
			logx.Err(err),
		)
		return LoginOutput{}, apperror.Unauthorized("invalid admin credentials", err)
	}

	output, err := issueLoginOutput(s.tokens, admin)
	if err != nil {
		recordLoginFailed(ctx, s.audit, s.logger, admin.ID, map[string]any{
			"name":   name,
			"reason": "token_issue_failed",
		})
		return LoginOutput{}, apperror.Internal("internal server error", err)
	}
	recordLoginSuccess(ctx, s.audit, s.logger, admin.ID, map[string]any{
		"name": name,
	})
	logx.Info(s.logger, ctx, startedAt, "admin.login.success", "admin login success",
		logx.Int64("admin_id", admin.ID),
		logx.String("name", name),
	)
	return output, nil
}

func (s *AdminAuthService) AuthenticateAccessToken(ctx context.Context, token string) (domain.Admin, error) {
	claims, err := s.tokens.ParseAccessToken(token)
	if err != nil {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", err)
	}
	if claims.ID == "" {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", nil)
	}
	if s.revocationRequired && s.redis == nil {
		return domain.Admin{}, apperror.ServiceUnavailable("admin authentication service unavailable", errors.New("redis client is not configured"))
	}
	if s.redis != nil {
		_, err := s.redis.Get(ctx, s.revokedTokenKey(claims.ID)).Result()
		switch {
		case err == nil:
			return domain.Admin{}, apperror.Unauthorized("invalid access token", nil)
		case errors.Is(err, redis.Nil):
		case err != nil:
			return domain.Admin{}, apperror.ServiceUnavailable("admin authentication service unavailable", err)
		}
	}
	if s.repo == nil {
		return domain.Admin{}, apperror.ServiceUnavailable("admin authentication service unavailable", errors.New("admin repository is not configured"))
	}
	admin, err := s.repo.GetAdminByID(ctx, claims.AdminID)
	if err != nil {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", err)
	}
	if admin.Disabled || admin.LastUpdateTime.UTC().UnixNano() != claims.AdminUpdatedAt {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", nil)
	}
	return admin, nil
}

func (s *AdminAuthService) Logout(ctx context.Context, admin domain.Admin, token string) error {
	claims, err := s.tokens.ParseAccessToken(token)
	if err != nil {
		return apperror.Unauthorized("invalid access token", err)
	}
	if s.redis == nil {
		return apperror.ServiceUnavailable("admin authentication service unavailable", errors.New("redis client is not configured"))
	}
	if claims.ExpiresAt == nil {
		return apperror.Unauthorized("invalid access token", nil)
	}
	if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
		if err := s.redis.Set(ctx, s.revokedTokenKey(claims.ID), "1", ttl).Err(); err != nil {
			return apperror.ServiceUnavailable("admin authentication service unavailable", err)
		}
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if s.audit == nil {
		return nil
	}
	if err := s.audit.Record(auditCtx, admin.ID, "AUTH", "LOGOUT", map[string]any{
		"name": admin.Name,
	}); err != nil {
		logx.Error(s.logger, auditCtx, time.Now(), "audit.record.failed", "logout audit record failed", logx.Int64("admin_id", admin.ID), logx.Err(err))
		return apperror.Internal("record logout audit failed", err)
	}
	return nil
}

func (s *AdminAuthService) revokedTokenKey(tokenID string) string {
	return redisKey(s.prefix, "admin_auth_revoked:"+tokenID)
}

func (s *AdminAuthService) HashPassword(password string) (string, error) {
	return s.tokens.HashPassword(password)
}

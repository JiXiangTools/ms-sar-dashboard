package service

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type AdminAuthService struct {
	repo   *repository.Repository
	tokens *auth.Service
	audit  *audit.Service
	logger *log.Logger
}

func NewAdminAuthService(repo *repository.Repository, tokens *auth.Service, auditSvc *audit.Service, logger *log.Logger) *AdminAuthService {
	return &AdminAuthService{repo: repo, tokens: tokens, audit: auditSvc, logger: logger}
}

func (s *AdminAuthService) Login(ctx context.Context, input LoginInput) (LoginOutput, error) {
	startedAt := time.Now()
	name := normalizeText(input.Name)
	password := input.Password
	if name == "" || strings.TrimSpace(password) == "" {
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
			"name":   name,
			"reason": "name_or_password_required",
		})
		return LoginOutput{}, apperror.BadRequest("name and password are required", nil)
	}

	admin, err := s.repo.GetAdminByName(ctx, name)
	if err != nil {
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
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
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
			"name":   name,
			"reason": "disabled",
		})
		return LoginOutput{}, apperror.Unauthorized("invalid admin credentials", nil)
	}
	if err := s.tokens.ComparePassword(admin.Password, password); err != nil {
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
			"name":   name,
			"reason": "invalid_credentials",
		})
		logx.Warn(s.logger, ctx, startedAt, "admin.login.failed", "admin login failed",
			logx.String("name", name),
			logx.Err(err),
		)
		return LoginOutput{}, apperror.Unauthorized("invalid admin credentials", err)
	}

	token, err := s.tokens.IssueAccessToken(admin)
	if err != nil {
		_ = s.audit.Record(ctx, admin.ID, "AUTH", "LOGIN_FAILED", map[string]any{
			"name":   name,
			"reason": "token_issue_failed",
		})
		return LoginOutput{}, apperror.Internal("internal server error", err)
	}
	output := LoginOutput{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.AccessTokenTTL().Seconds()),
	}
	_ = s.audit.Record(ctx, admin.ID, "AUTH", "LOGIN_SUCCESS", map[string]any{
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
	admin, err := s.repo.GetAdminByID(ctx, claims.AdminID)
	if err != nil {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", err)
	}
	if admin.Disabled || admin.LastUpdateTime.UTC().UnixNano() != claims.AdminUpdatedAt {
		return domain.Admin{}, apperror.Unauthorized("invalid access token", nil)
	}
	return admin, nil
}

func (s *AdminAuthService) Logout(ctx context.Context, admin domain.Admin) error {
	_ = s.audit.Record(ctx, admin.ID, "AUTH", "LOGOUT", map[string]any{
		"name": admin.Name,
	})
	return nil
}

func (s *AdminAuthService) HashPassword(password string) (string, error) {
	return s.tokens.HashPassword(password)
}

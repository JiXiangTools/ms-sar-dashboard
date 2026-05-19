package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type AppService struct {
	repo   *repository.Repository
	redis  redis.UniversalClient
	audit  *audit.Service
	logger *log.Logger
}

func NewAppService(repo *repository.Repository, redisClient redis.UniversalClient, auditSvc *audit.Service, logger *log.Logger) *AppService {
	return &AppService{repo: repo, redis: redisClient, audit: auditSvc, logger: logger}
}

func (s *AppService) List(ctx context.Context, query AppListQuery) (domain.Page[domain.App], error) {
	return s.repo.ListApps(ctx, query.AppID, query.Name, query.Page, query.PageSize, false)
}

func (s *AppService) Create(ctx context.Context, adminID int64, input AppCreateInput) (domain.App, error) {
	startedAt := time.Now()
	name := normalizeText(input.Name)
	if name == "" {
		return domain.App{}, apperror.BadRequest("name is required", nil)
	}
	secret := strings.TrimSpace(input.Secret)
	if secret == "" {
		secret = generateSecret()
	}

	now := time.Now().UTC()
	app, err := s.repo.CreateApp(ctx, domain.App{
		Name:           name,
		Secret:         secret,
		Remark:         strings.TrimSpace(input.Remark),
		Disabled:       false,
		CreateTime:     now,
		LastUpdateTime: now,
	})
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "APP", "CREATE", map[string]any{
			"name":    name,
			"remark":  strings.TrimSpace(input.Remark),
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, mapRepoError(err, "create app failed")
	}

	if err := s.syncAppAuthorization(ctx, app); err != nil {
		_ = s.repo.HardDeleteApp(ctx, app.ID)
		_ = s.audit.Record(ctx, adminID, "APP", "CREATE", map[string]any{
			"app_id":  app.ID,
			"name":    app.Name,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, apperror.Internal("sync app authorization failed", err)
	}

	_ = s.audit.Record(ctx, adminID, "APP", "CREATE", map[string]any{
		"app_id":  app.ID,
		"name":    app.Name,
		"success": true,
	})
	logx.Info(s.logger, ctx, startedAt, "app.create", "app created",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", app.ID),
		logx.String("name", app.Name),
	)
	return app, nil
}

func (s *AppService) Update(ctx context.Context, adminID int64, appID int64, input AppUpdateInput) (domain.App, error) {
	startedAt := time.Now()
	if appID <= 0 {
		return domain.App{}, apperror.BadRequest("invalid app_id", nil)
	}
	if input.Name == nil && input.Secret == nil && input.Remark == nil {
		return domain.App{}, apperror.BadRequest("at least one field is required", nil)
	}

	existing, err := s.repo.GetAppByID(ctx, appID)
	if err != nil {
		return domain.App{}, mapRepoError(err, "app not found")
	}

	updated := existing
	if input.Name != nil {
		name := normalizeText(*input.Name)
		if name == "" {
			return domain.App{}, apperror.BadRequest("name cannot be empty", nil)
		}
		updated.Name = name
	}
	if input.Secret != nil {
		secret := strings.TrimSpace(*input.Secret)
		if secret == "" {
			secret = generateSecret()
		}
		updated.Secret = secret
	}
	if input.Remark != nil {
		updated.Remark = strings.TrimSpace(*input.Remark)
	}
	updated.LastUpdateTime = time.Now().UTC()

	updated, err = s.repo.UpdateApp(ctx, updated)
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "APP", "UPDATE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, mapRepoError(err, "update app failed")
	}

	if err := s.syncAppAuthorization(ctx, updated); err != nil {
		_ = s.repo.RestoreApp(ctx, existing)
		_ = s.audit.Record(ctx, adminID, "APP", "UPDATE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, apperror.Internal("sync app authorization failed", err)
	}

	_ = s.audit.Record(ctx, adminID, "APP", "UPDATE", map[string]any{
		"app_id":  appID,
		"success": true,
	})
	logx.Info(s.logger, ctx, startedAt, "app.update", "app updated",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
	)
	return updated, nil
}

func (s *AppService) Delete(ctx context.Context, adminID int64, appID int64) error {
	startedAt := time.Now()
	if appID <= 0 {
		return apperror.BadRequest("invalid app_id", nil)
	}

	existing, err := s.repo.GetAppByID(ctx, appID)
	if err != nil {
		return mapRepoError(err, "app not found")
	}

	if err := s.repo.DeleteApp(ctx, appID); err != nil {
		return mapRepoError(err, "delete app failed")
	}

	if err := s.syncAppDeletion(ctx, appID); err != nil {
		_ = s.repo.RestoreApp(ctx, existing)
		_ = s.audit.Record(ctx, adminID, "APP", "DELETE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return apperror.Internal("sync app authorization failed", err)
	}

	_ = s.audit.Record(ctx, adminID, "APP", "DELETE", map[string]any{
		"app_id":  appID,
		"success": true,
	})
	logx.Info(s.logger, ctx, startedAt, "app.delete", "app deleted",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
	)
	return nil
}

func (s *AppService) Authorize(ctx context.Context, appID string, secret string) (domain.App, error) {
	rawAppID := strings.TrimSpace(appID)
	rawSecret := strings.TrimSpace(secret)
	if rawAppID == "" || rawSecret == "" {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", nil)
	}
	return s.authorizeFromRedis(ctx, rawAppID, rawSecret)
}

func (s *AppService) syncAppAuthorization(ctx context.Context, app domain.App) error {
	if s.redis == nil {
		return fmt.Errorf("redis client is not configured")
	}
	key := appAuthKey(app.ID)
	return s.redis.HSet(ctx, key, map[string]any{
		"id":         fmt.Sprintf("%d", app.ID),
		"secret":     app.Secret,
		"disabled":   fmt.Sprintf("%t", app.Disabled),
		"updated_at": app.LastUpdateTime.UTC().Format(time.RFC3339Nano),
	}).Err()
}

func (s *AppService) syncAppDeletion(ctx context.Context, appID int64) error {
	if s.redis == nil {
		return fmt.Errorf("redis client is not configured")
	}
	return s.redis.Del(ctx, appAuthKey(appID)).Err()
}

func (s *AppService) authorizeFromRedis(ctx context.Context, rawAppID string, rawSecret string) (domain.App, error) {
	if s.redis == nil {
		return domain.App{}, apperror.Internal("redis client is not configured", nil)
	}
	appID, err := parseInt64(rawAppID)
	if err != nil || appID <= 0 {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", err)
	}
	values, err := s.redis.HGetAll(ctx, appAuthKey(appID)).Result()
	if err != nil {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", err)
	}
	if len(values) == 0 {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", nil)
	}
	disabled := strings.EqualFold(values["disabled"], "true")
	secret := strings.TrimSpace(values["secret"])
	if disabled || secret == "" || secret != rawSecret {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", nil)
	}
	updatedAt, _ := time.Parse(time.RFC3339Nano, values["updated_at"])
	return domain.App{
		ID:             appID,
		Secret:         secret,
		Disabled:       disabled,
		LastUpdateTime: updatedAt,
	}, nil
}

func parseInt64(value string) (int64, error) {
	var parsed int64
	_, err := fmt.Sscan(strings.TrimSpace(value), &parsed)
	return parsed, err
}

func mapRepoError(err error, message string) error {
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(err.Error(), repository.ErrNotFound.Error()):
		return apperror.NotFound(message, err)
	case strings.Contains(err.Error(), repository.ErrConflict.Error()):
		return apperror.Conflict(message, err)
	default:
		return apperror.Internal(message, err)
	}
}

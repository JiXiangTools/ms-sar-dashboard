package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
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
	repo   appRepository
	redis  redis.UniversalClient
	audit  *audit.Service
	logger *log.Logger
}

const (
	appAuthReasonUnknown          = "unknown"
	appAuthReasonInvalidAppID     = "appid_invalid"
	appAuthReasonRedisReadFailed  = "redis_read_failed"
	appAuthReasonRedisKeyNotFound = "redis_key_not_found"
	appAuthReasonAppDisabled      = "app_disabled"
	appAuthReasonSecretEmpty      = "secret_empty"
	appAuthReasonSecretMismatch   = "secret_mismatch"
)

type appAuthFailure struct {
	reason string
	cause  error
}

func (e *appAuthFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.cause == nil {
		return e.reason
	}
	return e.cause.Error()
}

func (e *appAuthFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type appRepository interface {
	ListApps(ctx context.Context, appID *int64, name string, page int, pageSize int, includeDisabled bool) (domain.Page[domain.App], error)
	ListEnabledApps(ctx context.Context) ([]domain.App, error)
	CreateApp(ctx context.Context, app domain.App) (domain.App, error)
	UpdateApp(ctx context.Context, app domain.App) (domain.App, error)
	DeleteApp(ctx context.Context, appID int64) error
	RestoreApp(ctx context.Context, app domain.App) error
	HardDeleteApp(ctx context.Context, appID int64) error
	GetAppByID(ctx context.Context, appID int64) (domain.App, error)
}

func NewAppService(repo *repository.Repository, redisClient redis.UniversalClient, auditSvc *audit.Service, logger *log.Logger) *AppService {
	return &AppService{repo: repo, redis: redisClient, audit: auditSvc, logger: logger}
}

func (s *AppService) List(ctx context.Context, query AppListQuery) (domain.Page[domain.App], error) {
	return s.repo.ListApps(ctx, query.AppID, query.Name, query.Page, query.PageSize, false)
}

func (s *AppService) ListAuthorizedApps(ctx context.Context) ([]AppAuthSummary, error) {
	if s.redis == nil {
		return nil, apperror.Internal("redis client is not configured", nil)
	}

	cached, err := s.redis.Get(ctx, appAuthAllAppIDsKey()).Result()
	switch {
	case err == nil:
		return parseAppAuthSummariesCache(cached)
	case errors.Is(err, redis.Nil):
		return s.listAuthorizedAppsFromRepository(ctx)
	default:
		return nil, apperror.Internal("read app authorization index failed", err)
	}
}

func (s *AppService) Create(ctx context.Context, adminID int64, input AppCreateInput) (domain.App, error) {
	startedAt := time.Now()
	draft, err := newAppDraft(input)
	if err != nil {
		return domain.App{}, err
	}

	app, err := s.repo.CreateApp(ctx, draft)
	if err != nil {
		s.recordAppAudit(ctx, adminID, "CREATE", map[string]any{
			"name":    draft.Name,
			"remark":  draft.Remark,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, mapRepoError(err, "create app failed")
	}

	if err := s.syncAllAppAuthorizations(ctx); err != nil {
		_ = s.repo.HardDeleteApp(ctx, app.ID)
		_ = s.syncAppDeletion(ctx, app.ID)
		_ = s.syncAllAuthorizedAppIDs(ctx)
		s.recordAppAudit(ctx, adminID, "CREATE", map[string]any{
			"app_id":  app.ID,
			"name":    app.Name,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, apperror.Internal("sync app authorization failed", err)
	}

	s.recordAppAudit(ctx, adminID, "CREATE", map[string]any{
		"app_id":  app.ID,
		"name":    app.Name,
		"success": true,
	})
	s.logAppChange(ctx, startedAt, "app.create", "app created", adminID, app.ID, logx.String("name", app.Name))
	return app, nil
}

func (s *AppService) Update(ctx context.Context, adminID int64, appID int64, input AppUpdateInput) (domain.App, error) {
	startedAt := time.Now()
	existing, updated, err := s.buildUpdatedApp(ctx, appID, input)
	if err != nil {
		return domain.App{}, err
	}

	updated, err = s.repo.UpdateApp(ctx, updated)
	if err != nil {
		s.recordAppAudit(ctx, adminID, "UPDATE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, mapRepoError(err, "update app failed")
	}

	if err := s.syncAllAppAuthorizations(ctx); err != nil {
		_ = s.repo.RestoreApp(ctx, existing)
		_ = s.syncAppAuthorization(ctx, existing)
		_ = s.syncAllAuthorizedAppIDs(ctx)
		s.recordAppAudit(ctx, adminID, "UPDATE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return domain.App{}, apperror.Internal("sync app authorization failed", err)
	}

	s.recordAppAudit(ctx, adminID, "UPDATE", map[string]any{
		"app_id":  appID,
		"success": true,
	})
	s.logAppChange(ctx, startedAt, "app.update", "app updated", adminID, appID)
	return updated, nil
}

func (s *AppService) Delete(ctx context.Context, adminID int64, appID int64) error {
	startedAt := time.Now()
	if err := validateAppID(appID); err != nil {
		return err
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
		s.recordAppAudit(ctx, adminID, "DELETE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return apperror.Internal("sync app authorization failed", err)
	}
	if err := s.syncAllAuthorizedAppIDs(ctx); err != nil {
		_ = s.repo.RestoreApp(ctx, existing)
		_ = s.syncAppAuthorization(ctx, existing)
		_ = s.syncAllAuthorizedAppIDs(ctx)
		s.recordAppAudit(ctx, adminID, "DELETE", map[string]any{
			"app_id":  appID,
			"success": false,
			"error":   err.Error(),
		})
		return apperror.Internal("sync app authorization failed", err)
	}

	s.recordAppAudit(ctx, adminID, "DELETE", map[string]any{
		"app_id":  appID,
		"success": true,
	})
	s.logAppChange(ctx, startedAt, "app.delete", "app deleted", adminID, appID)
	return nil
}

func (s *AppService) Authorize(ctx context.Context, appID string, secret string) (domain.App, error) {
	rawAppID := strings.TrimSpace(appID)
	rawSecret := strings.TrimSpace(secret)
	if rawAppID == "" || rawSecret == "" {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonInvalidAppID})
	}
	return s.authorizeFromRedis(ctx, rawAppID, rawSecret)
}

func (s *AppService) syncAppAuthorization(ctx context.Context, app domain.App) error {
	if s.redis == nil {
		return errors.New("redis client is not configured")
	}
	key := appAuthKey(app.ID)
	return s.redis.HSet(ctx, key, map[string]any{
		"id":         strconv.FormatInt(app.ID, 10),
		"secret":     app.Secret,
		"disabled":   strconv.FormatBool(app.Disabled),
		"updated_at": app.LastUpdateTime.UTC().Format(time.RFC3339Nano),
	}).Err()
}

func (s *AppService) syncAllAppAuthorizations(ctx context.Context) error {
	startedAt := time.Now()
	if s.repo == nil {
		err := errors.New("app repository is not configured")
		s.logAppAuthorizationRefresh(ctx, startedAt, nil, nil, 0, false, err)
		return err
	}
	apps, err := s.repo.ListEnabledApps(ctx)
	if err != nil {
		s.logAppAuthorizationRefresh(ctx, startedAt, nil, nil, 0, false, err)
		return err
	}
	targetAppIDs := appIDs(apps)
	refreshedAppIDs := make([]int64, 0, len(apps))
	for _, app := range apps {
		if err := s.syncAppAuthorization(ctx, app); err != nil {
			s.logAppAuthorizationRefresh(ctx, startedAt, targetAppIDs, refreshedAppIDs, app.ID, false, err)
			return err
		}
		refreshedAppIDs = append(refreshedAppIDs, app.ID)
	}
	if err := s.syncAuthorizedAppIDs(ctx, buildAppAuthSummaries(apps)); err != nil {
		s.logAppAuthorizationRefresh(ctx, startedAt, targetAppIDs, refreshedAppIDs, 0, false, err)
		return err
	}
	s.logAppAuthorizationRefresh(ctx, startedAt, targetAppIDs, refreshedAppIDs, 0, true, nil)
	return nil
}

func (s *AppService) syncAllAuthorizedAppIDs(ctx context.Context) error {
	apps, err := s.listEnabledApps(ctx)
	if err != nil {
		return err
	}
	return s.syncAuthorizedAppIDs(ctx, buildAppAuthSummaries(apps))
}

func (s *AppService) syncAppDeletion(ctx context.Context, appID int64) error {
	if s.redis == nil {
		return errors.New("redis client is not configured")
	}
	return s.redis.Del(ctx, appAuthKey(appID)).Err()
}

func (s *AppService) syncAuthorizedAppIDs(ctx context.Context, summaries []AppAuthSummary) error {
	if s.redis == nil {
		return errors.New("redis client is not configured")
	}
	payload, err := json.Marshal(normalizeAppAuthSummaries(summaries))
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, appAuthAllAppIDsKey(), string(payload), 0).Err()
}

func (s *AppService) authorizeFromRedis(ctx context.Context, rawAppID string, rawSecret string) (domain.App, error) {
	if s.redis == nil {
		return domain.App{}, apperror.Internal("redis client is not configured", nil)
	}
	appID, err := parseInt64(rawAppID)
	if err != nil || appID <= 0 {
		return domain.App{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonInvalidAppID, cause: err})
	}
	record, err := s.readAppAuthRecord(ctx, appID)
	if err != nil {
		return domain.App{}, err
	}
	switch {
	case record.Disabled:
		return domain.App{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonAppDisabled})
	case record.Secret == "":
		return domain.App{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonSecretEmpty})
	case record.Secret != rawSecret:
		return domain.App{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonSecretMismatch})
	}
	return record.toApp(), nil
}

func (s *AppService) listAuthorizedAppsFromRepository(ctx context.Context) ([]AppAuthSummary, error) {
	apps, err := s.listEnabledApps(ctx)
	if err != nil {
		return nil, err
	}
	summaries := buildAppAuthSummaries(apps)
	if err := s.syncAuthorizedAppIDs(ctx, summaries); err != nil {
		return nil, apperror.Internal("sync app authorization index failed", err)
	}
	return summaries, nil
}

func (s *AppService) listEnabledApps(ctx context.Context) ([]domain.App, error) {
	if s.repo == nil {
		return nil, errors.New("app repository is not configured")
	}
	return s.repo.ListEnabledApps(ctx)
}

func newAppDraft(input AppCreateInput) (domain.App, error) {
	name := normalizeText(input.Name)
	if name == "" {
		return domain.App{}, apperror.BadRequest("name is required", nil)
	}

	now := time.Now().UTC()
	return domain.App{
		Name:           name,
		Secret:         normalizeSecret(input.Secret),
		Remark:         strings.TrimSpace(input.Remark),
		Disabled:       false,
		CreateTime:     now,
		LastUpdateTime: now,
	}, nil
}

func (s *AppService) buildUpdatedApp(ctx context.Context, appID int64, input AppUpdateInput) (domain.App, domain.App, error) {
	if err := validateAppID(appID); err != nil {
		return domain.App{}, domain.App{}, err
	}
	if input.Name == nil && input.Secret == nil && input.Remark == nil {
		return domain.App{}, domain.App{}, apperror.BadRequest("at least one field is required", nil)
	}

	existing, err := s.repo.GetAppByID(ctx, appID)
	if err != nil {
		return domain.App{}, domain.App{}, mapRepoError(err, "app not found")
	}
	updated, err := applyAppUpdate(existing, input)
	return existing, updated, err
}

func applyAppUpdate(app domain.App, input AppUpdateInput) (domain.App, error) {
	if input.Name != nil {
		name := normalizeText(*input.Name)
		if name == "" {
			return domain.App{}, apperror.BadRequest("name cannot be empty", nil)
		}
		app.Name = name
	}
	if input.Secret != nil {
		app.Secret = normalizeSecret(*input.Secret)
	}
	if input.Remark != nil {
		app.Remark = strings.TrimSpace(*input.Remark)
	}
	app.LastUpdateTime = time.Now().UTC()
	return app, nil
}

func normalizeSecret(value string) string {
	secret := strings.TrimSpace(value)
	if secret == "" {
		return generateSecret()
	}
	return secret
}

func validateAppID(appID int64) error {
	if appID <= 0 {
		return apperror.BadRequest("invalid app_id", nil)
	}
	return nil
}

func (s *AppService) readAppAuthRecord(ctx context.Context, appID int64) (AppAuthRecord, error) {
	values, err := s.redis.HGetAll(ctx, appAuthKey(appID)).Result()
	if err != nil {
		return AppAuthRecord{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonRedisReadFailed, cause: err})
	}
	return parseAppAuthRecord(appID, values)
}

func parseAppAuthRecord(appID int64, values map[string]string) (AppAuthRecord, error) {
	if len(values) == 0 {
		return AppAuthRecord{}, apperror.Unauthorized("invalid app authorization", &appAuthFailure{reason: appAuthReasonRedisKeyNotFound})
	}
	updatedAt, _ := time.Parse(time.RFC3339Nano, values["updated_at"])
	return AppAuthRecord{
		ID:        appID,
		Secret:    strings.TrimSpace(values["secret"]),
		Disabled:  isTruthy(values["disabled"]),
		UpdatedAt: updatedAt,
	}, nil
}

func (record AppAuthRecord) allows(secret string) bool {
	return !record.Disabled && record.Secret != "" && record.Secret == secret
}

func (record AppAuthRecord) toApp() domain.App {
	return domain.App{
		ID:             record.ID,
		Secret:         record.Secret,
		Disabled:       record.Disabled,
		LastUpdateTime: record.UpdatedAt,
	}
}

func (s *AppService) recordAppAudit(ctx context.Context, adminID int64, action string, content map[string]any) {
	_ = s.audit.Record(ctx, adminID, "APP", action, content)
}

func (s *AppService) logAppChange(ctx context.Context, startedAt time.Time, event string, message string, adminID int64, appID int64, fields ...logx.Field) {
	fields = append([]logx.Field{
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
	}, fields...)
	logx.Info(s.logger, ctx, startedAt, event, message, fields...)
}

func (s *AppService) logAppAuthorizationRefresh(ctx context.Context, startedAt time.Time, targetAppIDs []int64, refreshedAppIDs []int64, failedAppID int64, success bool, err error) {
	fields := []logx.Field{
		logx.String("refresh_app_ids", joinAppIDs(targetAppIDs)),
		logx.String("refreshed_app_ids", joinAppIDs(refreshedAppIDs)),
		logx.Bool("refresh_success", success),
	}
	if failedAppID > 0 {
		fields = append(fields, logx.Int64("failed_refresh_app_id", failedAppID))
	}
	if err != nil {
		fields = append(fields, logx.Err(err))
	}
	logx.Info(s.logger, ctx, startedAt, "app.auth_refresh", "app authorization refresh finished", fields...)
}

func appIDs(apps []domain.App) []int64 {
	ids := make([]int64, 0, len(apps))
	for _, app := range apps {
		ids = append(ids, app.ID)
	}
	return ids
}

func buildAppAuthSummaries(apps []domain.App) []AppAuthSummary {
	summaries := make([]AppAuthSummary, 0, len(apps))
	for _, app := range apps {
		summaries = append(summaries, AppAuthSummary{
			AppID:    app.ID,
			Disabled: app.Disabled,
		})
	}
	return summaries
}

func normalizeAppAuthSummaries(summaries []AppAuthSummary) []AppAuthSummary {
	if len(summaries) == 0 {
		return make([]AppAuthSummary, 0)
	}
	return summaries
}

func parseAppAuthSummariesCache(raw string) ([]AppAuthSummary, error) {
	var summaries []AppAuthSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &summaries); err != nil {
		return nil, apperror.Internal("invalid app authorization index cache", err)
	}
	return normalizeAppAuthSummaries(summaries), nil
}

func joinAppIDs(appIDs []int64) string {
	if len(appIDs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(appIDs))
	for _, appID := range appIDs {
		parts = append(parts, strconv.FormatInt(appID, 10))
	}
	return strings.Join(parts, ",")
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

func AppAuthFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if appErr, ok := apperror.As(err); ok && appErr.Cause != nil {
		err = appErr.Cause
	}
	var target *appAuthFailure
	if errors.As(err, &target) && strings.TrimSpace(target.reason) != "" {
		return target.reason
	}
	return appAuthReasonUnknown
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

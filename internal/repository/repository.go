package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kely-jian/ms-sar-dashboard/internal/domain"
)

var (
	ErrNotFound = errors.New("repository: not found")
	ErrConflict = errors.New("repository: conflict")
)

type Repository struct {
	orm *gorm.DB
}

func New(orm *gorm.DB) *Repository {
	if orm == nil {
		panic("repository requires a PostgreSQL/MySQL GORM database; initialize local PostgreSQL before starting")
	}
	return &Repository{orm: orm}
}

func nowValue() time.Time {
	return time.Now().UTC()
}

func normalizePagination(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrConflict
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate key") || strings.Contains(lower, "unique constraint") || strings.Contains(lower, "duplicate entry") {
		return ErrConflict
	}
	return err
}

func (r *Repository) transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

func (r *Repository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := r.orm.WithContext(ctx).Model(&adminModel{}).Count(&count).Error
	return count, mapSQLError(err)
}

func (r *Repository) CountApps(ctx context.Context) (int64, error) {
	var count int64
	err := r.orm.WithContext(ctx).Model(&appModel{}).Count(&count).Error
	return count, mapSQLError(err)
}

func (r *Repository) CountAdminLogs(ctx context.Context) (int64, error) {
	var count int64
	err := r.orm.WithContext(ctx).Model(&adminLogModel{}).Count(&count).Error
	return count, mapSQLError(err)
}

func (r *Repository) GetAdminByName(ctx context.Context, name string) (domain.Admin, error) {
	var model adminModel
	err := r.orm.WithContext(ctx).Where("name = ?", strings.TrimSpace(name)).First(&model).Error
	if err != nil {
		return domain.Admin{}, mapSQLError(err)
	}
	return toAdmin(model), nil
}

func (r *Repository) GetAdminByID(ctx context.Context, id int64) (domain.Admin, error) {
	var model adminModel
	err := r.orm.WithContext(ctx).Where("id = ?", id).First(&model).Error
	if err != nil {
		return domain.Admin{}, mapSQLError(err)
	}
	return toAdmin(model), nil
}

func (r *Repository) ListApps(ctx context.Context, appID *int64, name string, page int, pageSize int, includeDisabled bool) (domain.Page[domain.App], error) {
	page, pageSize = normalizePagination(page, pageSize)
	query := r.orm.WithContext(ctx).Model(&appModel{})
	if appID != nil && *appID > 0 {
		query = query.Where("id = ?", *appID)
	}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		query = query.Where("name ILIKE ?", "%"+trimmed+"%")
	}
	if !includeDisabled {
		query = query.Where("disabled = false")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.Page[domain.App]{}, mapSQLError(err)
	}

	var rows []appModel
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return domain.Page[domain.App]{}, mapSQLError(err)
	}

	items := make([]domain.App, 0, len(rows))
	for _, row := range rows {
		items = append(items, toApp(row))
	}
	return domain.Page[domain.App]{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func (r *Repository) CreateApp(ctx context.Context, app domain.App) (domain.App, error) {
	model := appModel{
		Name:           app.Name,
		Secret:         app.Secret,
		Remark:         app.Remark,
		Disabled:       app.Disabled,
		CreateTime:     app.CreateTime,
		LastUpdateTime: app.LastUpdateTime,
	}
	if err := r.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return domain.App{}, mapSQLError(err)
	}
	return toApp(model), nil
}

func (r *Repository) UpdateApp(ctx context.Context, app domain.App) (domain.App, error) {
	var updated domain.App
	err := r.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model appModel
		if err := tx.Where("id = ?", app.ID).First(&model).Error; err != nil {
			return mapSQLError(err)
		}
		model.Name = app.Name
		model.Secret = app.Secret
		model.Remark = app.Remark
		model.Disabled = app.Disabled
		model.LastUpdateTime = app.LastUpdateTime
		if err := tx.Save(&model).Error; err != nil {
			return mapSQLError(err)
		}
		updated = toApp(model)
		return nil
	})
	return updated, mapSQLError(err)
}

func (r *Repository) DeleteApp(ctx context.Context, appID int64) error {
	return mapSQLError(r.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&appModel{}).Where("id = ?", appID).Updates(map[string]any{
			"disabled":         true,
			"last_update_time": nowValue(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

func (r *Repository) RestoreApp(ctx context.Context, app domain.App) error {
	return mapSQLError(r.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&appModel{}).Where("id = ?", app.ID).Updates(map[string]any{
			"name":             app.Name,
			"secret":           app.Secret,
			"remark":           app.Remark,
			"disabled":         app.Disabled,
			"last_update_time": app.LastUpdateTime,
			"create_time":      app.CreateTime,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

func (r *Repository) HardDeleteApp(ctx context.Context, appID int64) error {
	result := r.orm.WithContext(ctx).Where("id = ?", appID).Delete(&appModel{})
	if result.Error != nil {
		return mapSQLError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetAppByID(ctx context.Context, appID int64) (domain.App, error) {
	var model appModel
	err := r.orm.WithContext(ctx).Where("id = ?", appID).First(&model).Error
	if err != nil {
		return domain.App{}, mapSQLError(err)
	}
	return toApp(model), nil
}

func (r *Repository) CreateAdminLog(ctx context.Context, log domain.AdminLog) error {
	model := adminLogModel{
		AdminID:    log.AdminID,
		Cate:       log.Cate,
		Type:       log.Type,
		Content:    mustJSON(log.Content),
		CreateTime: log.CreateTime,
	}
	return mapSQLError(r.orm.WithContext(ctx).Create(&model).Error)
}

func (r *Repository) ListAdminLogs(ctx context.Context, cate string, typ string, page int, pageSize int) (domain.Page[domain.AdminLog], error) {
	page, pageSize = normalizePagination(page, pageSize)
	query := r.orm.WithContext(ctx).Model(&adminLogModel{})
	if trimmed := strings.TrimSpace(cate); trimmed != "" {
		query = query.Where("cate = ?", trimmed)
	}
	if trimmed := strings.TrimSpace(typ); trimmed != "" {
		query = query.Where("type = ?", trimmed)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.Page[domain.AdminLog]{}, mapSQLError(err)
	}

	var rows []adminLogModel
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return domain.Page[domain.AdminLog]{}, mapSQLError(err)
	}

	items := make([]domain.AdminLog, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.AdminLog{
			ID:         row.ID,
			AdminID:    row.AdminID,
			Cate:       row.Cate,
			Type:       row.Type,
			Content:    json.RawMessage(row.Content),
			CreateTime: row.CreateTime,
		})
	}
	return domain.Page[domain.AdminLog]{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func mustJSON(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`null`)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(raw)
}

func toAdmin(model adminModel) domain.Admin {
	return domain.Admin{
		ID:             model.ID,
		Name:           model.Name,
		Nickname:       model.Nickname,
		Password:       model.Password,
		Disabled:       model.Disabled,
		CreateTime:     model.CreateTime,
		LastUpdateTime: model.LastUpdateTime,
	}
}

func toApp(model appModel) domain.App {
	return domain.App{
		ID:             model.ID,
		Name:           model.Name,
		Secret:         model.Secret,
		Remark:         model.Remark,
		Disabled:       model.Disabled,
		CreateTime:     model.CreateTime,
		LastUpdateTime: model.LastUpdateTime,
	}
}

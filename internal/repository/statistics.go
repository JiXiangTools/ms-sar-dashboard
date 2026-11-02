package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"gorm.io/gorm"
)

// PutStatistics replaces a complete day atomically. The app row serializes
// ingestion, so stale delivery and equal-version payload conflicts are explicit.
func (r *Repository) PutStatistics(ctx context.Context, input domain.StatisticsDay) (bool, error) {
	appid, _ := strconv.ParseInt(input.AppID, 10, 64)
	changed := false
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		var app appModel
		if err := tx.Raw("SELECT * FROM t_app WHERE id = ? FOR UPDATE", appid).Scan(&app).Error; err != nil {
			return err
		}
		if app.ID == 0 {
			return ErrNotFound
		}
		var stored struct {
			Revision int64
			Payload  json.RawMessage
		}
		if err := tx.Raw("SELECT revision, payload FROM t_app_statistics_day WHERE appid = ? AND day = ?", appid, input.Date).Scan(&stored).Error; err != nil {
			return err
		}
		if stored.Revision > input.Revision {
			return ErrConflict
		}
		if stored.Revision == input.Revision {
			var previous domain.StatisticsDay
			if err := json.Unmarshal(stored.Payload, &previous); err != nil {
				return err
			}
			if !reflect.DeepEqual(previous, input) {
				return ErrConflict
			}
			return nil
		}
		body, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO t_app_statistics_day(appid, day, revision, payload) VALUES (?, ?, ?, ?::jsonb)
		ON CONFLICT (appid, day) DO UPDATE SET revision = EXCLUDED.revision, payload = EXCLUDED.payload, updated_at = NOW()`, appid, input.Date, input.Revision, string(body)).Error; err != nil {
			return err
		}
		snapshot, err := json.Marshal(input.Snapshot)
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO t_app_statistics_snapshot(appid, revision, payload) VALUES (?, ?, ?::jsonb)
		ON CONFLICT (appid) DO UPDATE SET revision = EXCLUDED.revision, payload = EXCLUDED.payload, updated_at = NOW()
		WHERE t_app_statistics_snapshot.revision < EXCLUDED.revision`, appid, input.Revision, string(snapshot)).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, mapSQLError(err)
}

func (r *Repository) Statistics(ctx context.Context, appid int64, from, to string) ([]domain.StatisticsDay, *domain.StatisticsSnapshot, error) {
	if _, err := r.GetAppByID(ctx, appid); err != nil {
		return nil, nil, err
	}
	var rows []struct{ Payload json.RawMessage }
	if err := r.orm.WithContext(ctx).Raw("SELECT payload FROM t_app_statistics_day WHERE appid = ? AND day >= ? AND day <= ? ORDER BY day", appid, from, to).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	days := make([]domain.StatisticsDay, 0, len(rows))
	for _, row := range rows {
		var day domain.StatisticsDay
		if err := json.Unmarshal(row.Payload, &day); err != nil {
			return nil, nil, err
		}
		days = append(days, day)
	}
	var row struct{ Payload json.RawMessage }
	if err := r.orm.WithContext(ctx).Raw("SELECT payload FROM t_app_statistics_snapshot WHERE appid = ?", appid).Scan(&row).Error; err != nil {
		return nil, nil, err
	}
	if len(row.Payload) == 0 {
		return days, nil, nil
	}
	var snapshot domain.StatisticsSnapshot
	if err := json.Unmarshal(row.Payload, &snapshot); err != nil {
		return nil, nil, errors.New("invalid statistics snapshot")
	}
	return days, &snapshot, nil
}

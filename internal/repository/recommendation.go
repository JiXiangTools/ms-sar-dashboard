package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"gorm.io/gorm"
)

var ErrInvalidRecommendationConfiguration = errors.New("invalid recommendation configuration")

func lockRecommendationApp(tx *gorm.DB, appid string) error {
	var row appModel
	if err := tx.Raw("SELECT * FROM t_app WHERE id = ? FOR UPDATE", appid).Scan(&row).Error; err != nil {
		return err
	}
	if row.ID == 0 || row.Disabled {
		return ErrNotFound
	}
	return nil
}

// Definition mutations share a lock so creation and reference checks cannot race
// with deletion. Deleted IDs stay reserved; there is no update operation.
func lockRecommendationDefinitions(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext('rec-definitions'))").Error
}
func recommendationSources(tx *gorm.DB) ([]domain.RecallSource, error) {
	var rows []struct{ Payload json.RawMessage }
	if err := tx.Raw("SELECT payload FROM t_rec_recall WHERE deleted_at IS NULL ORDER BY recall_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.RecallSource, 0, len(rows))
	for _, row := range rows {
		var v domain.RecallSource
		if err := json.Unmarshal(row.Payload, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *Repository) RecommendationSources(ctx context.Context) ([]domain.RecallSource, error) {
	return recommendationSources(r.orm.WithContext(ctx))
}
func (r *Repository) SaveRecommendationSource(ctx context.Context, v domain.RecallSource) (domain.RecallSource, error) {
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationDefinitions(tx); err != nil {
			return err
		}
		v.CreatedAt = nowValue()
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_recall(recall_id,payload,created_at) VALUES (?,?::jsonb,?)", v.RecallID, string(raw), v.CreatedAt).Error
	})
	return v, mapSQLError(err)
}
func (r *Repository) RecommendationStrategies(ctx context.Context, appid string) ([]domain.RecommendationStrategy, error) {
	var rows []struct{ Payload json.RawMessage }
	if err := r.orm.WithContext(ctx).Raw("SELECT payload FROM t_rec_strategy WHERE appid=? AND deleted_at IS NULL ORDER BY strategy_id", appid).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.RecommendationStrategy, 0, len(rows))
	for _, row := range rows {
		var v domain.RecommendationStrategy
		if err := json.Unmarshal(row.Payload, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *Repository) SaveRecommendationStrategy(ctx context.Context, v domain.RecommendationStrategy) (domain.RecommendationStrategy, error) {
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationDefinitions(tx); err != nil {
			return err
		}
		if err := lockRecommendationApp(tx, v.AppID); err != nil {
			return err
		}
		recalls, err := recommendationSources(tx)
		if err != nil {
			return err
		}
		byID := map[string]domain.RecallSource{}
		for _, recall := range recalls {
			byID[recall.RecallID] = recall
		}
		if err := domain.ValidateRecommendationStrategy(v, byID); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRecommendationConfiguration, err)
		}
		v.CreatedAt = nowValue()
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_strategy(appid,strategy_id,payload,created_at) VALUES (?,?,?::jsonb,?)", v.AppID, v.StrategyID, string(raw), v.CreatedAt).Error
	})
	return v, mapSQLError(err)
}
func (r *Repository) SaveRecommendationExperiment(ctx context.Context, e domain.RecommendationExperiment) (domain.ExperimentDefinition, error) {
	var saved domain.ExperimentDefinition
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationDefinitions(tx); err != nil {
			return err
		}
		if err := lockRecommendationApp(tx, e.AppID); err != nil {
			return err
		}
		recalls, err := recommendationSources(tx)
		if err != nil {
			return err
		}
		byID := map[string]domain.RecallSource{}
		for _, v := range recalls {
			byID[v.RecallID] = v
		}
		strategies := []domain.RecommendationStrategy{}
		strategyMap := map[string]domain.RecommendationStrategy{}
		used := map[string]bool{}
		for _, assignment := range e.Assignments {
			var row struct{ Payload json.RawMessage }
			if err := tx.Raw("SELECT payload FROM t_rec_strategy WHERE appid=? AND strategy_id=? AND deleted_at IS NULL", e.AppID, assignment.StrategyID).Scan(&row).Error; err != nil {
				return err
			}
			if len(row.Payload) == 0 {
				return fmt.Errorf("%w: unknown strategy", ErrInvalidRecommendationConfiguration)
			}
			var strategy domain.RecommendationStrategy
			if err := json.Unmarshal(row.Payload, &strategy); err != nil {
				return err
			}
			if err := domain.ValidateRecommendationStrategy(strategy, byID); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidRecommendationConfiguration, err)
			}
			strategies = append(strategies, strategy)
			strategyMap[strategy.StrategyID] = strategy
			for _, routes := range [][]domain.StrategyRecall{strategy.Recall, strategy.Fallback} {
				for _, route := range routes {
					used[route.RecallID] = true
				}
			}
		}
		if err := domain.ValidateRecommendationAssignments(e, strategyMap); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRecommendationConfiguration, err)
		}
		snapshotRecalls := []domain.RecallSource{}
		for _, v := range recalls {
			if used[v.RecallID] {
				snapshotRecalls = append(snapshotRecalls, v)
			}
		}
		saved = domain.ExperimentDefinition{RecommendationExperiment: e, Status: "draft", CreatedAt: nowValue(), Snapshot: domain.RecommendationConfig{SchemaVersion: domain.RecommendationSchemaVersion, AppID: e.AppID, Experiment: &e, Recalls: snapshotRecalls, Strategies: strategies}}
		raw, err := json.Marshal(saved.Snapshot)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_experiment(appid,experiment_id,payload,created_at) VALUES (?,?,?::jsonb,?)", e.AppID, e.ExperimentID, string(raw), saved.CreatedAt).Error
	})
	return saved, mapSQLError(err)
}
func (r *Repository) RecommendationExperiments(ctx context.Context, appid string) ([]domain.ExperimentDefinition, error) {
	var rows []struct {
		Payload   json.RawMessage
		Status    string
		CreatedAt time.Time
		StartedAt *time.Time
		EndedAt   *time.Time
	}
	if err := r.orm.WithContext(ctx).Raw("SELECT payload,status,created_at,started_at,ended_at FROM t_rec_experiment WHERE appid=? AND deleted_at IS NULL ORDER BY created_at DESC", appid).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ExperimentDefinition, 0, len(rows))
	for _, row := range rows {
		var c domain.RecommendationConfig
		if err := json.Unmarshal(row.Payload, &c); err != nil {
			return nil, err
		}
		if c.Experiment == nil {
			return nil, errors.New("invalid experiment snapshot")
		}
		out = append(out, domain.ExperimentDefinition{RecommendationExperiment: *c.Experiment, Status: row.Status, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, EndedAt: row.EndedAt, Snapshot: c})
	}
	return out, nil
}
func (r *Repository) DeleteRecommendationDefinition(ctx context.Context, kind, appid, id string) error {
	return mapSQLError(r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationDefinitions(tx); err != nil {
			return err
		}
		var count int64
		var result *gorm.DB
		switch kind {
		case "recall":
			needle, _ := json.Marshal([]map[string]string{{"recall_id": id}})
			if err := tx.Raw("SELECT COUNT(*) FROM t_rec_strategy WHERE deleted_at IS NULL AND (payload->'recall' @> ?::jsonb OR payload->'fallback' @> ?::jsonb)", string(needle), string(needle)).Scan(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			result = tx.Exec("UPDATE t_rec_recall SET deleted_at=NOW() WHERE recall_id=? AND deleted_at IS NULL", id)
		case "strategy":
			if err := lockRecommendationApp(tx, appid); err != nil {
				return err
			}
			needle, _ := json.Marshal([]map[string]string{{"strategy_id": id}})
			if err := tx.Raw("SELECT COUNT(*) FROM t_rec_experiment WHERE appid=? AND deleted_at IS NULL AND payload->'experiment'->'assignments' @> ?::jsonb", appid, string(needle)).Scan(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			result = tx.Exec("UPDATE t_rec_strategy SET deleted_at=NOW() WHERE appid=? AND strategy_id=? AND deleted_at IS NULL", appid, id)
		case "experiment":
			if err := lockRecommendationApp(tx, appid); err != nil {
				return err
			}
			if err := tx.Raw("SELECT COUNT(*) FROM t_rec_publication WHERE appid=? AND payload->'experiment'->>'experiment_id'=?", appid, id).Scan(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConflict
			}
			result = tx.Exec("UPDATE t_rec_experiment SET deleted_at=NOW() WHERE appid=? AND experiment_id=? AND status<>'running' AND deleted_at IS NULL", appid, id)
		default:
			return ErrNotFound
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}))
}
func (r *Repository) RecommendationPublication(ctx context.Context, appid string) (domain.RecommendationPublication, error) {
	var row struct {
		Pending bool
		Payload json.RawMessage
	}
	err := r.orm.WithContext(ctx).Raw("SELECT pending,payload FROM t_rec_publication WHERE appid=?", appid).Scan(&row).Error
	out := domain.RecommendationPublication{AppID: appid, Pending: row.Pending}
	if err == nil && len(row.Payload) > 0 {
		err = json.Unmarshal(row.Payload, &out.Config)
	}
	return out, err
}
func (r *Repository) QueueRecommendationPublication(ctx context.Context, appid, experimentID string) error {
	return mapSQLError(r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, appid); err != nil {
			return err
		}
		c := domain.RecommendationConfig{SchemaVersion: domain.RecommendationSchemaVersion, AppID: appid, Recalls: []domain.RecallSource{}, Strategies: []domain.RecommendationStrategy{}}
		if experimentID != "" {
			var row struct {
				Payload json.RawMessage
				Status  string
			}
			if err := tx.Raw("SELECT payload,status FROM t_rec_experiment WHERE appid=? AND experiment_id=? AND deleted_at IS NULL", appid, experimentID).Scan(&row).Error; err != nil {
				return err
			}
			if len(row.Payload) == 0 {
				return ErrNotFound
			}
			if row.Status != "draft" {
				return ErrConflict
			}
			if err := json.Unmarshal(row.Payload, &c); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO t_rec_publication(appid,pending,payload) VALUES (?,TRUE,?::jsonb)
   ON CONFLICT(appid) DO UPDATE SET pending=TRUE,payload=EXCLUDED.payload,updated_at=NOW()`, appid, string(raw)).Error
	}))
}
func (r *Repository) PublishRecommendation(ctx context.Context, appid string, publish func(domain.RecommendationConfig) (int64, error)) error {
	return r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, appid); err != nil {
			return err
		}
		var row struct {
			Pending bool
			Payload json.RawMessage
		}
		if err := tx.Raw("SELECT pending,payload FROM t_rec_publication WHERE appid=? FOR UPDATE", appid).Scan(&row).Error; err != nil {
			return err
		}
		if len(row.Payload) == 0 {
			return nil
		}
		var c domain.RecommendationConfig
		if err := json.Unmarshal(row.Payload, &c); err != nil {
			return err
		}
		at, err := publish(c)
		if err != nil {
			return err
		}
		c.PublishedAt = at
		publishedAt := time.Unix(at, 0).UTC()
		if row.Pending {
			if err := tx.Exec("UPDATE t_rec_experiment SET status='ended',ended_at=? WHERE appid=? AND status='running'", publishedAt, appid).Error; err != nil {
				return err
			}
			if c.Experiment != nil {
				if err := tx.Exec("UPDATE t_rec_experiment SET status='running',started_at=? WHERE appid=? AND experiment_id=? AND status='draft'", publishedAt, appid, c.Experiment.ExperimentID).Error; err != nil {
					return err
				}
			}
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Exec("UPDATE t_rec_publication SET pending=FALSE,payload=?::jsonb WHERE appid=?", string(raw), appid).Error
	})
}
func (r *Repository) RecommendationPublicationApps(ctx context.Context, pendingOnly bool) ([]string, error) {
	var rows []int64
	query := "SELECT p.appid FROM t_rec_publication p JOIN t_app a ON a.id=p.appid WHERE NOT a.disabled"
	if pendingOnly {
		query += " AND p.pending"
	}
	if err := r.orm.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, id := range rows {
		out[i] = strconv.FormatInt(id, 10)
	}
	return out, nil
}

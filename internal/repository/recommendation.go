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

func recommendationSources(tx *gorm.DB) ([]domain.RecallSource, error) {
	var rows []struct{ Payload json.RawMessage }
	if err := tx.Raw("SELECT DISTINCT ON (source_id) payload FROM t_rec_source_version ORDER BY source_id, version DESC").Scan(&rows).Error; err != nil {
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

func (r *Repository) SaveRecommendationSource(ctx context.Context, source domain.RecallSource) (domain.RecallSource, error) {
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "rec-source:"+source.SourceID).Error; err != nil {
			return err
		}
		var version int64
		if err := tx.Raw("SELECT COALESCE(MAX(version),0) FROM t_rec_source_version WHERE source_id = ?", source.SourceID).Scan(&version).Error; err != nil {
			return err
		}
		if source.Version != version {
			return ErrConflict
		}
		source.Version = version + 1
		source.CreatedAt = nowValue()
		body, err := json.Marshal(source)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_source_version(source_id,version,payload,created_at) VALUES (?,?,?::jsonb,?)", source.SourceID, source.Version, string(body), source.CreatedAt).Error
	})
	return source, mapSQLError(err)
}

func (r *Repository) RecommendationStrategies(ctx context.Context, appid string) ([]domain.RecommendationStrategy, error) {
	var rows []struct{ Payload json.RawMessage }
	if err := r.orm.WithContext(ctx).Raw("SELECT DISTINCT ON (strategy_id) payload FROM t_rec_strategy_version WHERE appid = ? ORDER BY strategy_id, version DESC", appid).Scan(&rows).Error; err != nil {
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

func (r *Repository) SaveRecommendationStrategy(ctx context.Context, s domain.RecommendationStrategy) (domain.RecommendationStrategy, error) {
	if s.RecommendationType == "" {
		s.RecommendationType = domain.RecommendationPersonalized
	}
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, s.AppID); err != nil {
			return err
		}
		sources, err := recommendationSources(tx)
		if err != nil {
			return err
		}
		byID := map[string]domain.RecallSource{}
		for _, v := range sources {
			byID[v.SourceID] = v
		}
		if err := domain.ValidateRecommendationStrategy(s, byID); err != nil {
			return err
		}
		var version int64
		if err := tx.Raw("SELECT COALESCE(MAX(version),0) FROM t_rec_strategy_version WHERE appid=? AND strategy_id=?", s.AppID, s.StrategyID).Scan(&version).Error; err != nil {
			return err
		}
		if s.Version != version {
			return ErrConflict
		}
		s.Version = version + 1
		s.CreatedAt = nowValue()
		body, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_strategy_version(appid,strategy_id,version,payload,created_at) VALUES (?,?,?,?::jsonb,?)", s.AppID, s.StrategyID, s.Version, string(body), s.CreatedAt).Error
	})
	return s, mapSQLError(err)
}

func (r *Repository) SaveRecommendationExperiment(ctx context.Context, e domain.RecommendationExperiment) (domain.ExperimentVersion, error) {
	var saved domain.ExperimentVersion
	e.Assignments = append([]domain.ExperimentAssignment(nil), e.Assignments...)
	e.Controls = append([]domain.ExperimentControl(nil), e.Controls...)
	err := r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, e.AppID); err != nil {
			return err
		}
		var version int64
		if err := tx.Raw("SELECT COALESCE(MAX(version),0) FROM t_rec_experiment_version WHERE appid=? AND experiment_id=?", e.AppID, e.ExperimentID).Scan(&version).Error; err != nil {
			return err
		}
		if e.Version != version {
			return ErrConflict
		}
		e.Version = version + 1
		sources, err := recommendationSources(tx)
		if err != nil {
			return err
		}
		sourceMap := map[string]domain.RecallSource{}
		for _, s := range sources {
			sourceMap[s.SourceID] = s
		}
		strategies := make([]domain.RecommendationStrategy, 0, len(e.Assignments)+len(e.Controls))
		strategyMap := map[string]domain.RecommendationStrategy{}
		usedSources := map[string]bool{}
		references := make([]domain.ExperimentControl, 0, len(e.Assignments)+len(e.Controls))
		for _, assignment := range e.Assignments {
			references = append(references, domain.ExperimentControl{StrategyID: assignment.StrategyID, StrategyVersion: assignment.StrategyVersion})
		}
		references = append(references, e.Controls...)
		for i, a := range references {
			var row struct{ Payload json.RawMessage }
			query := "SELECT payload FROM t_rec_strategy_version WHERE appid=? AND strategy_id=?"
			args := []any{e.AppID, a.StrategyID}
			if a.StrategyVersion > 0 {
				query += " AND version=?"
				args = append(args, a.StrategyVersion)
			}
			query += " ORDER BY version DESC LIMIT 1"
			if err := tx.Raw(query, args...).Scan(&row).Error; err != nil {
				return err
			}
			if len(row.Payload) == 0 {
				return errors.New("strategy does not exist in this app")
			}
			var s domain.RecommendationStrategy
			if err := json.Unmarshal(row.Payload, &s); err != nil {
				return err
			}
			if err := domain.ValidatePublishedRecommendationStrategy(s, sourceMap); err != nil {
				return err
			}
			strategies = append(strategies, s)
			strategyMap[s.StrategyID] = s
			for _, recall := range s.Recall {
				usedSources[recall.SourceID] = true
			}
			usedSources[s.FallbackSourceID] = true
			usedSources[s.Newest.SourceID] = true
			if i < len(e.Assignments) {
				e.Assignments[i].StrategyVersion = s.Version
			} else {
				e.Controls[i-len(e.Assignments)].StrategyVersion = s.Version
			}
		}
		if err := domain.ValidateRecommendationAssignments(e, strategyMap); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRecommendationConfiguration, err)
		}
		if err := domain.ValidateRecommendationControls(e, strategyMap); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRecommendationConfiguration, err)
		}
		// Keep snapshots independent of unrelated global definitions. An app only
		// publishes the source versions referenced by its selected strategies.
		snapshotSources := make([]domain.RecallSource, 0, len(usedSources))
		for _, source := range sources {
			if usedSources[source.SourceID] {
				snapshotSources = append(snapshotSources, source)
			}
		}
		saved = domain.ExperimentVersion{RecommendationExperiment: e, Status: "draft", CreatedAt: nowValue(), Snapshot: domain.RecommendationConfig{SchemaVersion: domain.RecommendationSchemaVersion, AppID: e.AppID, Experiment: &e, Sources: snapshotSources, Strategies: strategies}}
		body, err := json.Marshal(saved.Snapshot)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO t_rec_experiment_version(appid,experiment_id,version,payload,created_at) VALUES (?,?,?,?::jsonb,?)", e.AppID, e.ExperimentID, e.Version, string(body), saved.CreatedAt).Error
	})
	return saved, mapSQLError(err)
}

func (r *Repository) RecommendationExperiments(ctx context.Context, appid string) ([]domain.ExperimentVersion, error) {
	var rows []struct {
		Payload   json.RawMessage
		Status    string
		CreatedAt time.Time
		StartedAt *time.Time
		EndedAt   *time.Time
	}
	if err := r.orm.WithContext(ctx).Raw("SELECT payload,status,created_at,started_at,ended_at FROM t_rec_experiment_version WHERE appid=? ORDER BY created_at DESC LIMIT 500", appid).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ExperimentVersion, 0, len(rows))
	for _, row := range rows {
		var c domain.RecommendationConfig
		if err := json.Unmarshal(row.Payload, &c); err != nil {
			return nil, err
		}
		if c.Experiment == nil {
			return nil, errors.New("invalid experiment snapshot")
		}
		out = append(out, domain.ExperimentVersion{RecommendationExperiment: *c.Experiment, Status: row.Status, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, EndedAt: row.EndedAt, Snapshot: c})
	}
	return out, nil
}

func (r *Repository) RecommendationPublication(ctx context.Context, appid string) (domain.RecommendationPublication, error) {
	var row struct {
		Revision          int64
		PublishedRevision int64
		Payload           json.RawMessage
	}
	err := r.orm.WithContext(ctx).Raw("SELECT revision,published_revision,payload FROM t_rec_publication WHERE appid=?", appid).Scan(&row).Error
	p := domain.RecommendationPublication{AppID: appid, Revision: row.Revision, PublishedRevision: row.PublishedRevision}
	if err == nil && len(row.Payload) > 0 {
		err = json.Unmarshal(row.Payload, &p.Config)
	}
	return p, err
}

func (r *Repository) QueueRecommendationPublication(ctx context.Context, appid, experimentID string, version, expectedRevision int64) error {
	return mapSQLError(r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, appid); err != nil {
			return err
		}
		var head struct{ Revision, PublishedRevision int64 }
		if err := tx.Raw("SELECT revision,published_revision FROM t_rec_publication WHERE appid=?", appid).Scan(&head).Error; err != nil {
			return err
		}
		if head.Revision != expectedRevision || head.Revision != head.PublishedRevision {
			return ErrConflict
		}
		config := domain.RecommendationConfig{SchemaVersion: domain.RecommendationSchemaVersion, AppID: appid, Revision: head.Revision + 1, Sources: []domain.RecallSource{}, Strategies: []domain.RecommendationStrategy{}}
		if experimentID != "" {
			var row struct {
				Payload json.RawMessage
				Status  string
			}
			if err := tx.Raw("SELECT payload,status FROM t_rec_experiment_version WHERE appid=? AND experiment_id=? AND version=?", appid, experimentID, version).Scan(&row).Error; err != nil {
				return err
			}
			if len(row.Payload) == 0 {
				return ErrNotFound
			}
			if row.Status != "draft" {
				return ErrConflict
			}
			if err := json.Unmarshal(row.Payload, &config); err != nil {
				return err
			}
			config.Revision = head.Revision + 1
		}
		body, err := json.Marshal(config)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO t_rec_publication(appid,revision,payload) VALUES (?,?,?::jsonb)
		ON CONFLICT(appid) DO UPDATE SET revision=EXCLUDED.revision,payload=EXCLUDED.payload,updated_at=NOW()`, appid, config.Revision, string(body)).Error
	}))
}

// Serialize publication with edits for this app. The callback performs a bounded
// single-key Redis CAS; a failed commit is retried with the same revision.
func (r *Repository) PublishRecommendation(ctx context.Context, appid string, publish func(domain.RecommendationConfig) (int64, error)) error {
	return r.transaction(ctx, func(tx *gorm.DB) error {
		if err := lockRecommendationApp(tx, appid); err != nil {
			return err
		}
		var row struct {
			Revision          int64
			PublishedRevision int64
			Payload           json.RawMessage
		}
		if err := tx.Raw("SELECT revision,published_revision,payload FROM t_rec_publication WHERE appid=? FOR UPDATE", appid).Scan(&row).Error; err != nil {
			return err
		}
		if row.Revision == 0 {
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
		if row.Revision > row.PublishedRevision {
			if err := tx.Exec("UPDATE t_rec_experiment_version SET status='ended',ended_at=? WHERE appid=? AND status='running'", publishedAt, appid).Error; err != nil {
				return err
			}
			if c.Experiment != nil {
				if err := tx.Exec("UPDATE t_rec_experiment_version SET status='running',started_at=? WHERE appid=? AND experiment_id=? AND version=? AND status='draft'", publishedAt, appid, c.Experiment.ExperimentID, c.Experiment.Version).Error; err != nil {
					return err
				}
			}
		}
		body, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Exec("UPDATE t_rec_publication SET published_revision=revision,payload=?::jsonb WHERE appid=?", string(body), appid).Error
	})
}

func (r *Repository) RecommendationPublicationApps(ctx context.Context, pendingOnly bool) ([]string, error) {
	var rows []int64
	query := "SELECT p.appid FROM t_rec_publication p JOIN t_app a ON a.id=p.appid WHERE NOT a.disabled"
	if pendingOnly {
		query += " AND p.revision>p.published_revision"
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

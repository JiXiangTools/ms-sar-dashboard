package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
	"github.com/redis/go-redis/v9"
)

type RecommendationService struct {
	repo   *repository.Repository
	redis  redis.UniversalClient
	prefix string
	audit  *audit.Service
	logger *log.Logger
	// A failed startup replay includes already acknowledged publications.
	// Keep retrying that full replay until it succeeds.
	recoveryPending atomic.Bool
}

func NewRecommendationService(repo *repository.Repository, client redis.UniversalClient, prefix string, auditSvc *audit.Service, logger *log.Logger) *RecommendationService {
	return &RecommendationService{repo: repo, redis: client, prefix: prefix, audit: auditSvc, logger: logger}
}

func recommendationError(err error) error {
	if errors.Is(err, repository.ErrInvalidRecommendationConfiguration) {
		return apperror.BadRequest(err.Error(), nil)
	}
	if errors.Is(err, repository.ErrConflict) {
		return apperror.Conflict("ID already exists, definition is referenced, or experiment is not publishable", err)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return apperror.NotFound("app, experiment or strategy not found", err)
	}
	if err != nil {
		return apperror.Internal("recommendation configuration operation failed", err)
	}
	return nil
}

func (s *RecommendationService) Sources(ctx context.Context) ([]domain.RecallSource, error) {
	v, err := s.repo.RecommendationSources(ctx)
	return v, recommendationError(err)
}
func (s *RecommendationService) SaveSource(ctx context.Context, adminID int64, v domain.RecallSource) (domain.RecallSource, error) {
	if err := domain.ValidateRecallSource(v); err != nil {
		return v, apperror.BadRequest(err.Error(), nil)
	}
	v, err := s.repo.SaveRecommendationSource(ctx, v)
	if err != nil {
		return v, recommendationError(err)
	}
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "CREATE_RECALL", v)
}

type RecommendationWorkspace struct {
	Recalls         []domain.RecallSource            `json:"recalls"`
	Strategies      []domain.RecommendationStrategy  `json:"strategies"`
	Experiments     []domain.ExperimentDefinition    `json:"experiments"`
	Publication     domain.RecommendationPublication `json:"publication"`
	StrategyHistory []RecommendationStrategyHistory  `json:"strategy_history"`
}

// A strategy keeps one execution interval per experiment that references it.
type RecommendationStrategyHistory struct {
	RecommendationType string     `json:"recommendation_type"`
	StrategyID         string     `json:"strategy_id"`
	ExperimentID       string     `json:"experiment_id"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at"`
}

func validRecommendationAppID(v string) bool {
	n, e := strconv.ParseInt(v, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == v
}

func (s *RecommendationService) Workspace(ctx context.Context, appid string) (RecommendationWorkspace, error) {
	var out RecommendationWorkspace
	if !validRecommendationAppID(appid) {
		return out, apperror.BadRequest("invalid appid", nil)
	}
	id, _ := strconv.ParseInt(appid, 10, 64)
	if _, err := s.repo.GetAppByID(ctx, id); err != nil {
		return out, recommendationError(err)
	}
	var err error
	if out.Recalls, err = s.repo.RecommendationSources(ctx); err != nil {
		return out, recommendationError(err)
	}
	if out.Strategies, err = s.repo.RecommendationStrategies(ctx, appid); err != nil {
		return out, recommendationError(err)
	}
	if out.Experiments, err = s.repo.RecommendationExperiments(ctx, appid); err != nil {
		return out, recommendationError(err)
	}
	out.StrategyHistory = []RecommendationStrategyHistory{}
	for _, e := range out.Experiments {
		for _, strategy := range e.Snapshot.Strategies {
			out.StrategyHistory = append(out.StrategyHistory, RecommendationStrategyHistory{RecommendationType: strategy.EffectiveType(), StrategyID: strategy.StrategyID, ExperimentID: e.ExperimentID, CreatedAt: strategy.CreatedAt, StartedAt: e.StartedAt, EndedAt: e.EndedAt})
		}
	}
	out.Publication, err = s.repo.RecommendationPublication(ctx, appid)
	return out, recommendationError(err)
}

func (s *RecommendationService) SaveStrategy(ctx context.Context, adminID int64, v domain.RecommendationStrategy) (domain.RecommendationStrategy, error) {
	if !validRecommendationAppID(v.AppID) {
		return v, apperror.BadRequest("invalid appid", nil)
	}
	sources, err := s.repo.RecommendationSources(ctx)
	if err != nil {
		return v, recommendationError(err)
	}
	byID := map[string]domain.RecallSource{}
	for _, source := range sources {
		byID[source.RecallID] = source
	}
	if err := domain.ValidateRecommendationStrategy(v, byID); err != nil {
		return v, apperror.BadRequest(err.Error(), nil)
	}
	v, err = s.repo.SaveRecommendationStrategy(ctx, v)
	if err != nil {
		return v, recommendationError(err)
	}
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "CREATE_STRATEGY", v)
}

func (s *RecommendationService) SaveExperiment(ctx context.Context, adminID int64, e domain.RecommendationExperiment) (domain.ExperimentDefinition, error) {
	var zero domain.ExperimentDefinition
	if !validRecommendationAppID(e.AppID) {
		return zero, apperror.BadRequest("invalid appid", nil)
	}
	if err := domain.ValidateRecommendationExperiment(e); err != nil {
		return zero, apperror.BadRequest(err.Error(), nil)
	}
	strategies, err := s.repo.RecommendationStrategies(ctx, e.AppID)
	if err != nil {
		return zero, recommendationError(err)
	}
	byID := map[string]domain.RecommendationStrategy{}
	for _, v := range strategies {
		byID[v.StrategyID] = v
	}
	if err := domain.ValidateRecommendationAssignments(e, byID); err != nil {
		return zero, apperror.BadRequest(err.Error(), nil)
	}
	v, err := s.repo.SaveRecommendationExperiment(ctx, e)
	if err != nil {
		return v, recommendationError(err)
	}
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "CREATE_EXPERIMENT", map[string]any{"appid": e.AppID, "experiment_id": e.ExperimentID})
}

func (s *RecommendationService) publish(ctx context.Context, appid string) error {
	return s.repo.PublishRecommendation(ctx, appid, func(c domain.RecommendationConfig) (int64, error) {
		if c.PublishedAt == 0 {
			c.PublishedAt = time.Now().Unix()
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return 0, err
		}
		if err := s.redis.Set(ctx, s.prefix+"rec:appconf:{"+appid+"}", raw, 0).Err(); err != nil {
			return 0, err
		}
		return c.PublishedAt, nil
	})
}
func (s *RecommendationService) Publish(ctx context.Context, adminID int64, appid, experimentID string) (domain.RecommendationPublication, error) {
	var out domain.RecommendationPublication
	if !validRecommendationAppID(appid) || (experimentID != "" && !domain.RecommendationIDPattern.MatchString(experimentID)) {
		return out, apperror.BadRequest("invalid publication request", nil)
	}
	if err := s.repo.QueueRecommendationPublication(ctx, appid, experimentID); err != nil {
		return out, recommendationError(err)
	}
	if err := s.audit.Record(ctx, adminID, "RECOMMENDATION", "QUEUE_PUBLICATION", map[string]any{"appid": appid, "experiment_id": experimentID}); err != nil {
		return out, err
	}
	if err := s.publish(ctx, appid); err != nil {
		return out, apperror.ServiceUnavailable("configuration saved; publication pending and will retry automatically", err)
	}
	return s.repo.RecommendationPublication(ctx, appid)
}
func (s *RecommendationService) Delete(ctx context.Context, adminID int64, kind, appid, id string) error {
	if !domain.RecommendationIDPattern.MatchString(id) || (kind != "recall" && !validRecommendationAppID(appid)) {
		return apperror.BadRequest("invalid definition id", nil)
	}
	if err := s.repo.DeleteRecommendationDefinition(ctx, kind, appid, id); err != nil {
		return recommendationError(err)
	}
	return s.audit.Record(ctx, adminID, "RECOMMENDATION", "DELETE_DEFINITION", map[string]any{"kind": kind, "appid": appid, "id": id})
}

func (s *RecommendationService) Reconcile(ctx context.Context, all bool) error {
	if all {
		s.recoveryPending.Store(true)
	}
	apps, err := s.repo.RecommendationPublicationApps(ctx, !all)
	if err != nil {
		return err
	}
	var failures []error
	for _, appid := range apps {
		if err := s.publish(ctx, appid); err != nil {
			failures = append(failures, err)
		}
	}
	err = errors.Join(failures...)
	if all && err == nil {
		s.recoveryPending.Store(false)
	}
	return err
}

func (s *RecommendationService) Run(ctx context.Context) {
	s.run(ctx, 5*time.Second, time.Minute)
}

func (s *RecommendationService) run(ctx context.Context, retryEvery, replayEvery time.Duration) {
	ticker := time.NewTicker(retryEvery)
	defer ticker.Stop()
	replay := time.NewTicker(replayEvery)
	defer replay.Stop()
	for {
		all := s.recoveryPending.Load()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-replay.C:
			all = true
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.Reconcile(refreshCtx, all)
		cancel()
		if err != nil && s.logger != nil {
			logx.Error(s.logger, ctx, time.Now(), "recommendation.projection_retry_failed", "recommendation publication retry failed", logx.Err(err))
		}
	}
}

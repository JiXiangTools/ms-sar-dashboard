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
		return apperror.Conflict("configuration changed; reload before creating or publishing a new version", err)
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
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "SAVE_SOURCE_VERSION", v)
}

type RecommendationWorkspace struct {
	Sources         []domain.RecallSource            `json:"sources"`
	Strategies      []domain.RecommendationStrategy  `json:"strategies"`
	Experiments     []domain.ExperimentVersion       `json:"experiments"`
	Publication     domain.RecommendationPublication `json:"publication"`
	StrategyHistory []RecommendationStrategyHistory  `json:"strategy_history"`
}

// A reusable definition has several execution intervals, one for each experiment
// version that references it. An ended interval never changes on later reuse.
type RecommendationStrategyHistory struct {
	RecommendationType string     `json:"recommendation_type"`
	StrategyID         string     `json:"strategy_id"`
	StrategyVersion    int64      `json:"strategy_version"`
	GroupID            string     `json:"group_id"`
	ExperimentID       string     `json:"experiment_id"`
	ExperimentVersion  int64      `json:"experiment_version"`
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
	if out.Sources, err = s.repo.RecommendationSources(ctx); err != nil {
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
			group := strategy.StrategyID
			for _, control := range e.Controls {
				if control.StrategyID == strategy.StrategyID {
					group = "default"
				}
			}
			out.StrategyHistory = append(out.StrategyHistory, RecommendationStrategyHistory{RecommendationType: strategy.EffectiveType(), StrategyID: strategy.StrategyID, StrategyVersion: strategy.Version, GroupID: group, ExperimentID: e.ExperimentID, ExperimentVersion: e.Version, CreatedAt: strategy.CreatedAt, StartedAt: e.StartedAt, EndedAt: e.EndedAt})
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
		byID[source.SourceID] = source
	}
	if err := domain.ValidateRecommendationStrategy(v, byID); err != nil {
		return v, apperror.BadRequest(err.Error(), nil)
	}
	v, err = s.repo.SaveRecommendationStrategy(ctx, v)
	if err != nil {
		return v, recommendationError(err)
	}
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "SAVE_STRATEGY_VERSION", v)
}

func (s *RecommendationService) SaveExperiment(ctx context.Context, adminID int64, e domain.RecommendationExperiment) (domain.ExperimentVersion, error) {
	var zero domain.ExperimentVersion
	if e.BucketAlgorithm == "" {
		e.BucketAlgorithm = "sha256"
	}
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
	references := append([]domain.ExperimentControl(nil), e.Controls...)
	for _, a := range e.Assignments {
		references = append(references, domain.ExperimentControl{StrategyID: a.StrategyID, StrategyVersion: a.StrategyVersion})
	}
	for _, a := range references {
		v, ok := byID[a.StrategyID]
		if !ok || a.StrategyVersion < 0 || a.StrategyVersion > v.Version {
			return zero, apperror.BadRequest("unknown strategy version", nil)
		}
	}
	v, err := s.repo.SaveRecommendationExperiment(ctx, e)
	if err != nil {
		return v, recommendationError(err)
	}
	return v, s.audit.Record(ctx, adminID, "RECOMMENDATION", "CREATE_EXPERIMENT_VERSION", map[string]any{"appid": e.AppID, "experiment_id": e.ExperimentID, "experiment_version": v.Version})
}

var publishRecommendationScript = redis.NewScript(`
local old = false
if redis.call('TYPE', KEYS[1]).ok == 'string' then old = redis.call('GET', KEYS[1]) end
if old then
  local ok, current = pcall(cjson.decode, old)
  if ok and type(current) == 'table' and current.appid == ARGV[4] and type(current.revision) == 'number' then
    if current.revision > tonumber(ARGV[1]) then return redis.error_reply('newer configuration already published') end
    if ARGV[5] == '1' and current.revision == tonumber(ARGV[1]) and type(current.published_at) == 'number' and current.published_at > 0 and current.published_at ~= tonumber(ARGV[3]) then
      return tostring(current.published_at)
    end
  end
  if old == ARGV[2] then return ARGV[3] end
end
redis.call('SET', KEYS[1], ARGV[2])
return ARGV[3]
`)

func (s *RecommendationService) publish(ctx context.Context, appid string) error {
	return s.repo.PublishRecommendation(ctx, appid, func(c domain.RecommendationConfig) (int64, error) {
		preserveUnconfirmedAt := 0
		if c.PublishedAt == 0 {
			preserveUnconfirmedAt = 1
			c.PublishedAt = time.Now().Unix()
		}
		for attempt := 0; attempt < 2; attempt++ {
			raw, err := json.Marshal(c)
			if err != nil {
				return 0, err
			}
			value, err := publishRecommendationScript.Run(ctx, s.redis, []string{s.prefix + "rec:appconf:{" + appid + "}"}, c.Revision, string(raw), c.PublishedAt, appid, preserveUnconfirmedAt).Text()
			if err != nil {
				return 0, err
			}
			at, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, err
			}
			if at == c.PublishedAt {
				return at, nil
			}
			c.PublishedAt = at
		}
		return 0, errors.New("publication timestamp changed during retry")
	})
}

func (s *RecommendationService) Publish(ctx context.Context, adminID int64, appid, experimentID string, version, expectedRevision int64) (domain.RecommendationPublication, error) {
	var out domain.RecommendationPublication
	if !validRecommendationAppID(appid) || expectedRevision < 0 || expectedRevision >= 9007199254740991 || (experimentID != "" && (!domain.RecommendationIDPattern.MatchString(experimentID) || version < 1)) {
		return out, apperror.BadRequest("invalid publication request", nil)
	}
	if err := s.repo.QueueRecommendationPublication(ctx, appid, experimentID, version, expectedRevision); err != nil {
		return out, recommendationError(err)
	}
	if err := s.audit.Record(ctx, adminID, "RECOMMENDATION", "QUEUE_PUBLICATION", map[string]any{"appid": appid, "experiment_id": experimentID, "experiment_version": version, "revision": expectedRevision + 1}); err != nil {
		return out, err
	}
	if err := s.publish(ctx, appid); err != nil {
		return out, apperror.ServiceUnavailable("configuration saved; publication pending and will retry automatically", err)
	}
	return s.repo.RecommendationPublication(ctx, appid)
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
			s.logger.Printf("recommendation publication retry failed: %v", err)
		}
	}
}

package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/elasticsearch"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
)

type DebugService struct {
	cfg    config.Config
	redis  redis.UniversalClient
	es     *elasticsearch.Client
	audit  *audit.Service
	logger *log.Logger
}

func NewDebugService(cfg config.Config, redisClient redis.UniversalClient, esClient *elasticsearch.Client, auditSvc *audit.Service, logger *log.Logger) *DebugService {
	return &DebugService{cfg: cfg, redis: redisClient, es: esClient, audit: auditSvc, logger: logger}
}

func (s *DebugService) IndexInfo(ctx context.Context, adminID int64, appID int64) (map[string]any, error) {
	startedAt := time.Now()
	if !s.cfg.Elasticsearch.DebugEnabled {
		return nil, apperror.Forbidden("es debug disabled", nil)
	}
	if s.es == nil {
		return nil, apperror.Internal("elasticsearch client is not configured", nil)
	}
	index := s.esIndexName(appID)
	value, err := s.es.IndexInfo(ctx, index)
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "INDEX_VIEW", map[string]any{
			"appid":   appID,
			"index":   index,
			"success": false,
			"error":   err.Error(),
		})
		return nil, apperror.Internal("es debug failed", err)
	}
	_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "INDEX_VIEW", map[string]any{
		"appid":   appID,
		"index":   index,
		"success": true,
	})
	logx.Info(s.logger, ctx, startedAt, "es_debug.index_view", "es index view",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
		logx.String("index", index),
	)
	return value, nil
}

func (s *DebugService) Document(ctx context.Context, adminID int64, appID int64, itemID string) (map[string]any, error) {
	startedAt := time.Now()
	if !s.cfg.Elasticsearch.DebugEnabled {
		return nil, apperror.Forbidden("es debug disabled", nil)
	}
	if s.es == nil {
		return nil, apperror.Internal("elasticsearch client is not configured", nil)
	}
	index := s.esIndexName(appID)
	value, err := s.es.Document(ctx, index, strings.TrimSpace(itemID))
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "DOC_VIEW", map[string]any{
			"appid":   appID,
			"index":   index,
			"item_id": itemID,
			"success": false,
			"error":   err.Error(),
		})
		return nil, apperror.Internal("es debug failed", err)
	}
	_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "DOC_VIEW", map[string]any{
		"appid":   appID,
		"index":   index,
		"item_id": itemID,
		"success": true,
	})
	logx.Info(s.logger, ctx, startedAt, "es_debug.doc_view", "es document view",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
		logx.String("index", index),
		logx.String("item_id", itemID),
	)
	return value, nil
}

func (s *DebugService) Search(ctx context.Context, adminID int64, appID int64, body []byte) (map[string]any, error) {
	startedAt := time.Now()
	if !s.cfg.Elasticsearch.DebugEnabled {
		return nil, apperror.Forbidden("es debug disabled", nil)
	}
	if s.es == nil {
		return nil, apperror.Internal("elasticsearch client is not configured", nil)
	}
	index := s.esIndexName(appID)
	value, err := s.es.Search(ctx, index, body)
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "QUERY", map[string]any{
			"appid":    appID,
			"index":    index,
			"success":  false,
			"error":    err.Error(),
			"body_len": len(body),
		})
		return nil, apperror.Internal("es debug failed", err)
	}
	_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "QUERY", map[string]any{
		"appid":    appID,
		"index":    index,
		"success":  true,
		"body_len": len(body),
	})
	logx.Info(s.logger, ctx, startedAt, "es_debug.query", "es query",
		logx.Int64("admin_id", adminID),
		logx.Int64("app_id", appID),
		logx.String("index", index),
		logx.Int("body_len", len(body)),
	)
	return value, nil
}

func (s *DebugService) Recommend(ctx context.Context, adminID int64, req RecDebugRequest) (RecDebugResult, error) {
	startedAt := time.Now()
	if !s.cfg.RecommendDebug.DebugEnabled {
		return RecDebugResult{}, apperror.Forbidden("recommend debug disabled", nil)
	}
	if s.redis == nil {
		return RecDebugResult{}, apperror.Internal("redis client is not configured", nil)
	}
	size := clampSize(req.Size, s.cfg.RecommendDebug.MaxCandidateLimit)
	key := deriveDebugKey(req.Type, req.AppID, req.ItemID, req.UserID, req.Period, req.Key)
	if key == "" {
		return RecDebugResult{}, apperror.BadRequest("invalid debug key", nil)
	}
	result, err := s.readRecommendKey(ctx, key, size, req.Exclude)
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "REC_DEBUG", "VIEW", map[string]any{
			"appid":   req.AppID,
			"key":     key,
			"success": false,
			"error":   err.Error(),
		})
		return RecDebugResult{}, apperror.Internal("recommend debug failed", err)
	}
	result.Type = req.Type
	result.AppID = req.AppID
	result.Key = key
	_ = s.audit.Record(ctx, adminID, "REC_DEBUG", "VIEW", map[string]any{
		"appid":          req.AppID,
		"key":            key,
		"raw_count":      result.RawCount,
		"parsed_count":   result.ParsedCount,
		"filtered_count": result.FilteredCount,
		"final_count":    result.FinalCount,
		"success":        true,
	})
	logx.Info(s.logger, ctx, startedAt, "rec_debug.view", "recommend debug",
		logx.Int64("admin_id", adminID),
		logx.String("appid", req.AppID),
		logx.String("key", key),
		logx.Int("final_count", result.FinalCount),
	)
	return result, nil
}

func (s *DebugService) readRecommendKey(ctx context.Context, key string, size int, exclude []string) (RecDebugResult, error) {
	kind, err := s.redis.Type(ctx, key).Result()
	if err != nil {
		return RecDebugResult{}, err
	}
	if kind == "none" {
		return RecDebugResult{Exists: false, KeyType: kind}, nil
	}
	value, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return RecDebugResult{}, err
	}
	items, rawCount, parsedCount, filteredCount, reasons := parseRecommendValue(value, size, exclude)
	return RecDebugResult{
		Exists:          true,
		KeyType:         kind,
		RawCount:        rawCount,
		ParsedCount:     parsedCount,
		FilteredCount:   filteredCount,
		FinalCount:      len(items),
		FilteredReasons: reasons,
		Items:           items,
	}, nil
}

func parseRecommendValue(value string, size int, exclude []string) ([]RecItem, int, int, int, map[string]int) {
	seen := map[string]struct{}{}
	excluded := make(map[string]struct{}, len(exclude))
	for _, item := range exclude {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			excluded[trimmed] = struct{}{}
		}
	}

	tokens := strings.Split(value, ",")
	rawCount := 0
	parsedCount := 0
	filteredCount := 0
	reasons := map[string]int{}
	items := make([]RecItem, 0, len(tokens))

	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		rawCount++
		parts := strings.SplitN(token, ":", 2)
		itemID := strings.TrimSpace(parts[0])
		if itemID == "" {
			filteredCount++
			reasons["empty_item_id"]++
			continue
		}
		if _, ok := excluded[itemID]; ok {
			filteredCount++
			reasons["excluded"]++
			continue
		}
		if _, ok := seen[itemID]; ok {
			filteredCount++
			reasons["duplicate"]++
			continue
		}
		score := 0.0
		if len(parts) == 2 {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
				score = parsed
			}
		}
		items = append(items, RecItem{
			ItemID: itemID,
			Score:  score,
			Raw:    token,
		})
		seen[itemID] = struct{}{}
		parsedCount++
		if size > 0 && len(items) >= size {
			break
		}
	}
	return items, rawCount, parsedCount, filteredCount, reasons
}

func (s *DebugService) esIndexName(appID int64) string {
	prefix := strings.TrimSpace(s.cfg.Elasticsearch.ProductIndexPrefix)
	if prefix == "" {
		prefix = "ms_search_product"
	}
	return fmt.Sprintf("%s_%d_v1", prefix, appID)
}

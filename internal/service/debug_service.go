package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/elasticsearch"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/requestid"
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

func (s *DebugService) RawES(ctx context.Context, adminID int64, req ESRawRequest) (any, error) {
	startedAt := time.Now()
	if !s.cfg.Elasticsearch.DebugEnabled {
		return nil, apperror.Forbidden("es debug disabled", nil)
	}
	if s.es == nil {
		return nil, apperror.Internal("elasticsearch client is not configured", nil)
	}
	method, path, body, err := normalizeESRawRequest(req)
	if err != nil {
		return nil, err
	}

	value, err := s.es.Raw(ctx, method, path, body)
	auditContent := map[string]any{
		"method":   method,
		"path":     path,
		"body_len": len(body),
		"success":  err == nil,
	}
	if err != nil {
		auditContent["error"] = err.Error()
		_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "RAW", auditContent)
		return nil, apperror.Internal("es debug failed", err)
	}
	_ = s.audit.Record(ctx, adminID, "ES_DEBUG", "RAW", auditContent)
	logx.Info(s.logger, ctx, startedAt, "es_debug.raw", "es raw request",
		logx.Int64("admin_id", adminID),
		logx.String("method", method),
		logx.String("path", path),
		logx.Int("body_len", len(body)),
	)
	return value, nil
}

func normalizeESRawRequest(req ESRawRequest) (string, string, []byte, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	path := strings.TrimSpace(req.Path)
	if method == "" || path == "" {
		return "", "", nil, apperror.BadRequest("method and path are required", nil)
	}
	if method != http.MethodGet && method != http.MethodHead {
		return "", "", nil, apperror.BadRequest("only GET and HEAD are allowed", nil)
	}
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") {
		return "", "", nil, apperror.BadRequest("path must be a relative ES path", nil)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return "", "", nil, apperror.BadRequest("path contains invalid whitespace", nil)
	}
	lowerPath := strings.ToLower(path)
	for _, blocked := range []string{
		"/_bulk",
		"/_delete_by_query",
		"/_update_by_query",
		"/_reindex",
		"/_scripts",
		"/_snapshot",
		"/_tasks",
		"/_security",
	} {
		if strings.Contains(lowerPath, blocked) {
			return "", "", nil, apperror.BadRequest("es write or sensitive operation is not allowed", nil)
		}
	}
	bodyText := strings.TrimSpace(req.Body)
	if bodyText == "" {
		return method, path, nil, nil
	}
	if !json.Valid([]byte(bodyText)) {
		return "", "", nil, apperror.BadRequest("request body must be valid JSON", nil)
	}
	return method, path, []byte(bodyText), nil
}

func ParseESRawConsoleInput(input string) (ESRawRequest, error) {
	trimmed := strings.TrimLeftFunc(input, unicode.IsSpace)
	if trimmed == "" {
		return ESRawRequest{}, apperror.BadRequest("request input is required", nil)
	}
	lineEnd := strings.IndexByte(trimmed, '\n')
	firstLine := trimmed
	rest := ""
	if lineEnd >= 0 {
		firstLine = trimmed[:lineEnd]
		rest = trimmed[lineEnd+1:]
	}
	fields := strings.Fields(strings.TrimSpace(firstLine))
	if len(fields) != 2 {
		return ESRawRequest{}, apperror.BadRequest("first line must be METHOD /path", nil)
	}
	body := strings.TrimSpace(rest)
	return ESRawRequest{
		Method: fields[0],
		Path:   fields[1],
		Body:   body,
	}, nil
}

func (s *DebugService) Recommend(ctx context.Context, adminID int64, req RecDebugRequest) (RecDebugResult, error) {
	startedAt := time.Now()
	if !s.cfg.RecommendDebug.DebugEnabled {
		return RecDebugResult{}, apperror.Forbidden("recommend debug disabled", nil)
	}
	if s.redis == nil {
		return RecDebugResult{}, apperror.Internal("redis client is not configured", nil)
	}
	if strings.TrimSpace(s.cfg.RecommendDebug.OnlineBaseURL) == "" {
		return RecDebugResult{}, apperror.Internal("recommend online base url is not configured", nil)
	}

	size := clampSize(req.Size, s.cfg.RecommendDebug.MaxCandidateLimit)
	endpoint, params, query, err := buildRecommendDebugRequest(req, size)
	if err != nil {
		return RecDebugResult{}, err
	}

	secret, err := s.recommendAppSecret(ctx, req.AppID)
	if err != nil {
		return RecDebugResult{}, err
	}
	targetURL, err := s.recommendURL(endpoint, query)
	if err != nil {
		return RecDebugResult{}, err
	}

	result, err := s.callRecommendOnline(ctx, targetURL, secret, req.AppID)
	if err != nil {
		_ = s.audit.Record(ctx, adminID, "REC_DEBUG", "VIEW", map[string]any{
			"appid":    req.AppID,
			"type":     strings.ToLower(strings.TrimSpace(req.Type)),
			"endpoint": endpoint,
			"success":  false,
			"error":    err.Error(),
		})
		return RecDebugResult{}, err
	}
	result.Type = strings.ToLower(strings.TrimSpace(req.Type))
	result.AppID = req.AppID
	result.Endpoint = endpoint
	result.Params = params
	_ = s.audit.Record(ctx, adminID, "REC_DEBUG", "VIEW", map[string]any{
		"appid":       req.AppID,
		"type":        result.Type,
		"endpoint":    endpoint,
		"final_count": result.Size,
		"success":     true,
	})
	logx.Info(s.logger, ctx, startedAt, "rec_debug.view", "recommend debug",
		logx.Int64("admin_id", adminID),
		logx.String("appid", req.AppID),
		logx.String("type", result.Type),
		logx.String("endpoint", endpoint),
		logx.Int("final_count", result.Size),
	)
	return result, nil
}

func buildRecommendDebugRequest(req RecDebugRequest, size int) (string, map[string]any, url.Values, error) {
	debugType := strings.ToLower(strings.TrimSpace(req.Type))
	if strings.TrimSpace(req.AppID) == "" {
		return "", nil, nil, apperror.BadRequest("appid is required", nil)
	}

	params := map[string]any{"size": size}
	query := url.Values{}
	query.Set("size", strconv.Itoa(size))

	if len(req.Exclude) > 0 {
		exclude := make([]string, 0, len(req.Exclude))
		for _, item := range req.Exclude {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				exclude = append(exclude, trimmed)
			}
		}
		if len(exclude) > 0 {
			params["exclude"] = exclude
			query.Set("exclude", strings.Join(exclude, ","))
		}
	}

	switch debugType {
	case "hot":
		period := strings.TrimSpace(req.Period)
		if period == "" {
			period = "day"
		}
		if period != "hour" && period != "day" && period != "week" {
			return "", nil, nil, apperror.BadRequest("invalid hot period", nil)
		}
		params["period"] = period
		query.Set("period", period)
		return "/api/v1/msrec/recommend/hot", params, query, nil
	case "related":
		itemID := strings.TrimSpace(req.ItemID)
		if itemID == "" {
			return "", nil, nil, apperror.BadRequest("item_id is required", nil)
		}
		params["item_id"] = itemID
		query.Set("item_id", itemID)
		return "/api/v1/msrec/recommend/related", params, query, nil
	case "personalized":
		userID := strings.TrimSpace(req.UserID)
		if userID == "" {
			return "", nil, nil, apperror.BadRequest("user_id is required", nil)
		}
		params["user_id"] = userID
		query.Set("user_id", userID)
		return "/api/v1/msrec/recommend/personalized", params, query, nil
	default:
		return "", nil, nil, apperror.BadRequest("invalid recommend type", nil)
	}
}

func (s *DebugService) recommendAppSecret(ctx context.Context, rawAppID string) (string, error) {
	appID, err := strconv.ParseInt(strings.TrimSpace(rawAppID), 10, 64)
	if err != nil || appID <= 0 {
		return "", apperror.BadRequest("invalid appid", nil)
	}

	values, err := s.redis.HGetAll(ctx, appAuthKey(appID)).Result()
	if err != nil {
		return "", apperror.Internal("read app authorization failed", err)
	}
	if len(values) == 0 {
		return "", apperror.Unauthorized("app authorization not found", nil)
	}
	if strings.EqualFold(strings.TrimSpace(values["disabled"]), "true") || strings.TrimSpace(values["disabled"]) == "1" {
		return "", apperror.Unauthorized("app authorization disabled", nil)
	}
	secret := strings.TrimSpace(values["secret"])
	if secret == "" {
		return "", apperror.Unauthorized("app authorization secret is empty", nil)
	}
	return secret, nil
}

func (s *DebugService) recommendURL(endpoint string, query url.Values) (string, error) {
	baseURL, err := url.Parse(strings.TrimSpace(s.cfg.RecommendDebug.OnlineBaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return "", apperror.Internal("invalid recommend online base url", err)
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + endpoint
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

func (s *DebugService) callRecommendOnline(ctx context.Context, targetURL string, secret string, appID string) (RecDebugResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return RecDebugResult{}, apperror.Internal("build recommend request failed", err)
	}
	request.Header.Set("x-dwzauth-appid", strings.TrimSpace(appID))
	request.Header.Set("x-dwzauth-secret", secret)
	if rid := requestid.FromContext(ctx); rid != "" {
		request.Header.Set(requestid.HeaderName, rid)
	}

	timeout := s.cfg.RecommendDebug.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return RecDebugResult{}, apperror.Internal("call recommend online failed", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return RecDebugResult{}, apperror.Internal("read recommend response failed", err)
	}

	var raw any
	_ = json.Unmarshal(body, &raw)
	var payload struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			ItemIDs []string `json:"item_ids"`
			Size    int      `json:"size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return RecDebugResult{}, apperror.Internal("decode recommend response failed", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || payload.Status != http.StatusOK {
		status := response.StatusCode
		if status < http.StatusBadRequest {
			status = http.StatusInternalServerError
		}
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = "recommend online request failed"
		}
		return RecDebugResult{}, apperror.New(status, status, message, nil)
	}

	return RecDebugResult{
		Status:  payload.Status,
		Message: payload.Message,
		ItemIDs: payload.Data.ItemIDs,
		Size:    payload.Data.Size,
		Raw:     raw,
	}, nil
}

func (s *DebugService) esIndexName(appID int64) string {
	prefix := strings.TrimSpace(s.cfg.Elasticsearch.ItemIndexPrefix)
	if prefix == "" {
		prefix = "ms_search_item"
	}
	return fmt.Sprintf("%s_%d_v1", prefix, appID)
}

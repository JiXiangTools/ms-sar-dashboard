package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

const (
	maxLocalRateLimitEntries = 4096
	overflowRateLimitKey     = "__overflow__"
)

const redisRateLimitScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
if ttl < 0 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`

type RateLimitStore interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)
}

type RateLimitKeyFunc func(c *gin.Context) string

type redisEvalClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
}

type RedisRateLimitStore struct {
	client  redisEvalClient
	prefix  string
	timeout time.Duration
}

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	entries     map[string]rateLimitEntry
	limit       int
	window      time.Duration
	lastCleanup time.Time
}

func NewRedisRateLimitStore(client redisEvalClient, prefix string, timeout time.Duration) *RedisRateLimitStore {
	return &RedisRateLimitStore{client: client, prefix: strings.TrimSpace(prefix), timeout: timeout}
}

func (s *RedisRateLimitStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if s == nil || s.client == nil {
		return false, 0, errors.New("redis rate limit client is not configured")
	}
	if limit <= 0 || window <= 0 {
		return false, 0, errors.New("rate limit and window must be positive")
	}
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	windowMilliseconds := maxInt64(1, window.Milliseconds())
	result, err := s.client.Eval(ctx, redisRateLimitScript, []string{s.redisKey(key)}, windowMilliseconds).Slice()
	if err != nil {
		return false, 0, err
	}
	if len(result) != 2 {
		return false, 0, fmt.Errorf("unexpected redis rate limit result length %d", len(result))
	}
	count, err := redisResultInt64(result[0])
	if err != nil {
		return false, 0, fmt.Errorf("parse redis rate limit count: %w", err)
	}
	ttlMilliseconds, err := redisResultInt64(result[1])
	if err != nil {
		return false, 0, fmt.Errorf("parse redis rate limit ttl: %w", err)
	}
	retryAfter := time.Duration(maxInt64(1, ttlMilliseconds)) * time.Millisecond
	return count <= int64(limit), retryAfter, nil
}

func (s *RedisRateLimitStore) redisKey(key string) string {
	digest := sha256.Sum256([]byte(key))
	suffix := "security_rate_limit:" + hex.EncodeToString(digest[:])
	if s.prefix == "" {
		return suffix
	}
	if strings.HasSuffix(s.prefix, ":") || strings.HasSuffix(s.prefix, "_") {
		return s.prefix + suffix
	}
	return s.prefix + ":" + suffix
}

func SecurityRateLimit(limit int, window time.Duration, scope string) gin.HandlerFunc {
	return SecurityRateLimitWithStore(nil, limit, window, scope, ClientIPRateLimitKey)
}

// SecurityRateLimitWithStore uses Redis when available and continuously mirrors
// attempts into a local window. Redis failures fall back to that bounded local
// window, so the endpoint neither fails open nor returns a dependency-wide 5xx.
func SecurityRateLimitWithStore(primary RateLimitStore, limit int, window time.Duration, scope string, keyFunc RateLimitKeyFunc) gin.HandlerFunc {
	local := &fixedWindowLimiter{
		entries: make(map[string]rateLimitEntry),
		limit:   limit,
		window:  window,
	}
	if keyFunc == nil {
		keyFunc = ClientIPRateLimitKey
	}
	return func(c *gin.Context) {
		key := scope + ":" + keyFunc(c)
		localAllowed, localRetryAfter := local.allow(key)
		allowed, retryAfter := localAllowed, localRetryAfter
		if primary != nil {
			sharedAllowed, sharedRetryAfter, err := primary.Allow(c.Request.Context(), key, limit, window)
			if err == nil {
				allowed, retryAfter = sharedAllowed, sharedRetryAfter
			}
		}
		if allowed {
			c.Next()
			return
		}
		retryAfterSeconds := int((retryAfter + time.Second - 1) / time.Second)
		c.Header("Retry-After", strconv.Itoa(maxInt(1, retryAfterSeconds)))
		response.Error(c, http.StatusTooManyRequests, "too many requests", nil)
		response.SetErrorLogDetails(c, map[string]any{"rate_limit_scope": scope})
		c.Abort()
	}
}

func ClientIPRateLimitKey(c *gin.Context) string {
	return "source:" + ClientIP(c)
}

func LoginAccountRateLimitKey(c *gin.Context) string {
	var request struct {
		Name string `json:"name"`
	}
	if body := bufferedRequestBody(c); len(body) > 0 && json.Unmarshal(body, &request) == nil {
		name := strings.ToLower(strings.TrimSpace(request.Name))
		if name != "" {
			digest := sha256.Sum256([]byte(name))
			return "account_sha256:" + hex.EncodeToString(digest[:])
		}
	}
	return "account_unknown:" + ClientIP(c)
}

func (l *fixedWindowLimiter) allow(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]rateLimitEntry)
	}
	cleanupInterval := minDuration(l.window, 10*time.Second)
	if len(l.entries) >= maxLocalRateLimitEntries-1 && (l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= cleanupInterval) {
		for entryKey, candidate := range l.entries {
			if now.Sub(candidate.windowStart) >= l.window {
				delete(l.entries, entryKey)
			}
		}
		l.lastCleanup = now
	}
	if _, exists := l.entries[key]; !exists && len(l.entries) >= maxLocalRateLimitEntries-1 {
		key = overflowRateLimitKey
	}
	entry := l.entries[key]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = rateLimitEntry{windowStart: now, count: 1}
		return true, 0
	}
	if entry.count >= l.limit {
		return false, l.window - now.Sub(entry.windowStart)
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}

func redisResultInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minDuration(left time.Duration, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

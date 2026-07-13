package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type sharedRedisRateLimitFake struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func (f *sharedRedisRateLimitFake) Eval(_ context.Context, _ string, keys []string, args ...any) *redis.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return redis.NewCmdResult(nil, f.err)
	}
	if f.counts == nil {
		f.counts = make(map[string]int64)
	}
	f.counts[keys[0]]++
	ttl, _ := strconv.ParseInt(strings.TrimSpace(toString(args[0])), 10, 64)
	return redis.NewCmdResult([]any{f.counts[keys[0]], ttl}, nil)
}

func TestRedisRateLimitStoreSharesAtomicCountAcrossInstances(t *testing.T) {
	backend := &sharedRedisRateLimitFake{}
	first := NewRedisRateLimitStore(backend, "test", time.Second)
	second := NewRedisRateLimitStore(backend, "test", time.Second)

	for index, testCase := range []struct {
		store *RedisRateLimitStore
		want  bool
	}{
		{store: first, want: true},
		{store: second, want: true},
		{store: first, want: false},
	} {
		allowed, retryAfter, err := testCase.store.Allow(context.Background(), "admin_login:identity", 2, time.Minute)
		if err != nil {
			t.Fatalf("call %d: %v", index+1, err)
		}
		if allowed != testCase.want {
			t.Fatalf("call %d allowed=%t want=%t", index+1, allowed, testCase.want)
		}
		if retryAfter <= 0 {
			t.Fatalf("call %d retry_after=%s", index+1, retryAfter)
		}
	}
	for key := range backend.counts {
		if strings.Contains(key, "admin_login") || strings.Contains(key, "identity") {
			t.Fatalf("redis key leaked logical identity: %s", key)
		}
	}
}

func TestSecurityRateLimitSharesRedisQuotaAcrossHTTPInstances(t *testing.T) {
	gin.SetMode(gin.TestMode)
	backend := &sharedRedisRateLimitFake{}
	newEngine := func() *gin.Engine {
		resolver, err := NewClientIPResolver(nil)
		if err != nil {
			t.Fatalf("new resolver: %v", err)
		}
		engine := gin.New()
		engine.Use(ResolveClientIP(resolver))
		store := NewRedisRateLimitStore(backend, "test", time.Second)
		engine.GET("/login", SecurityRateLimitWithStore(store, 1, time.Minute, "login", ClientIPRateLimitKey), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		return engine
	}

	firstEngine := newEngine()
	secondEngine := newEngine()
	for index, engine := range []*gin.Engine{firstEngine, secondEngine} {
		request := httptest.NewRequest(http.MethodGet, "/login", nil)
		request.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		want := http.StatusNoContent
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("instance %d status=%d want=%d body=%s", index+1, recorder.Code, want, recorder.Body.String())
		}
	}
}

func TestSecurityRateLimitFallsBackLocallyWithoutFailOpenOrGlobalFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := NewClientIPResolver(nil)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	primary := NewRedisRateLimitStore(&sharedRedisRateLimitFake{err: errors.New("redis unavailable")}, "test", 10*time.Millisecond)
	engine := gin.New()
	engine.Use(ResolveClientIP(resolver))
	engine.GET("/login", SecurityRateLimitWithStore(primary, 1, time.Minute, "login", ClientIPRateLimitKey), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	first := httptest.NewRecorder()
	engine.ServeHTTP(first, request)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/login", nil)
	request.RemoteAddr = "192.0.2.10:4321"
	second := httptest.NewRecorder()
	engine.ServeHTTP(second, request)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/login", nil)
	request.RemoteAddr = "192.0.2.11:1234"
	independent := httptest.NewRecorder()
	engine.ServeHTTP(independent, request)
	if independent.Code != http.StatusNoContent {
		t.Fatalf("independent identity status=%d body=%s", independent.Code, independent.Body.String())
	}
}

func TestLoginAccountRateLimitSeparatesAccountsBehindNATAndHashesIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := NewClientIPResolver(nil)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	capture := &captureRateLimitStore{}
	engine := gin.New()
	engine.Use(ResolveClientIP(resolver), RequestBodyLimit(1024))
	engine.POST("/login", SecurityRateLimitWithStore(capture, 1, time.Minute, "admin_login_account", LoginAccountRateLimitKey), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for index, testCase := range []struct {
		body string
		want int
	}{
		{body: `{"name":"Alice","password":"top-secret"}`, want: http.StatusNoContent},
		{body: `{"name":"Bob","password":"other-secret"}`, want: http.StatusNoContent},
		{body: `{"name":"Alice","password":"top-secret"}`, want: http.StatusTooManyRequests},
	} {
		request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(testCase.body))
		request.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != testCase.want {
			t.Fatalf("request=%d body=%s status=%d want=%d response=%s", index+1, testCase.body, recorder.Code, testCase.want, recorder.Body.String())
		}
	}
	if len(capture.keys) != 3 || capture.keys[0] == capture.keys[1] || capture.keys[0] != capture.keys[2] {
		t.Fatalf("expected separate account keys, got %#v", capture.keys)
	}
	for _, key := range capture.keys {
		for _, forbidden := range []string{"Alice", "alice", "Bob", "bob", "top-secret", "other-secret"} {
			if strings.Contains(key, forbidden) {
				t.Fatalf("rate limit key leaked %q: %s", forbidden, key)
			}
		}
	}
}

func TestUntrustedForwardedForCannotEvadeRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	engine := gin.New()
	engine.Use(ResolveClientIP(resolver))
	engine.GET("/login", SecurityRateLimit(1, time.Minute, "login"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for index, forwarded := range []string{"198.51.100.1", "198.51.100.2"} {
		request := httptest.NewRequest(http.MethodGet, "/login", nil)
		request.RemoteAddr = "203.0.113.42:1234"
		request.Header.Set("X-Forwarded-For", forwarded)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		want := http.StatusNoContent
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("request %d status=%d want=%d body=%s", index+1, recorder.Code, want, recorder.Body.String())
		}
	}
}

func TestLocalRateLimitFallbackHasBoundedIdentityStorage(t *testing.T) {
	limiter := &fixedWindowLimiter{
		entries: make(map[string]rateLimitEntry),
		limit:   1,
		window:  time.Minute,
	}
	for index := 0; index < maxLocalRateLimitEntries+100; index++ {
		limiter.allow("identity-" + strconv.Itoa(index))
	}
	if len(limiter.entries) > maxLocalRateLimitEntries {
		t.Fatalf("local rate limit entries = %d", len(limiter.entries))
	}
	if _, exists := limiter.entries[overflowRateLimitKey]; !exists {
		t.Fatal("overflow identities must share a conservative bounded bucket")
	}
}

type captureRateLimitStore struct {
	keys   []string
	counts map[string]int
}

func (s *captureRateLimitStore) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	s.keys = append(s.keys, key)
	if s.counts == nil {
		s.counts = make(map[string]int)
	}
	s.counts[key]++
	return s.counts[key] <= limit, window, nil
}

func toString(value any) string {
	switch typed := value.(type) {
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

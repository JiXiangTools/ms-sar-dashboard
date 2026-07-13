package middleware

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRateLimitStoreIntegration(t *testing.T) {
	address := os.Getenv("MSSAR_RATE_LIMIT_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("MSSAR_RATE_LIMIT_TEST_REDIS_ADDR is not set")
	}
	database := 0
	if rawDatabase := os.Getenv("MSSAR_RATE_LIMIT_TEST_REDIS_DB"); rawDatabase != "" {
		parsed, err := strconv.Atoi(rawDatabase)
		if err != nil {
			t.Fatalf("parse redis db: %v", err)
		}
		database = parsed
	}
	client := redis.NewClient(&redis.Options{
		Addr:                  address,
		Password:              os.Getenv("MSSAR_RATE_LIMIT_TEST_REDIS_PASSWORD"),
		DB:                    database,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping test redis: %v", err)
	}

	prefix := fmt.Sprintf("ms_sar_dashboard_rate_limit_test:%d", time.Now().UnixNano())
	first := NewRedisRateLimitStore(client, prefix, time.Second)
	second := NewRedisRateLimitStore(client, prefix, time.Second)
	logicalKey := "login:integration"
	t.Cleanup(func() { _ = client.Del(context.Background(), first.redisKey(logicalKey)).Err() })

	for index, testCase := range []struct {
		store *RedisRateLimitStore
		want  bool
	}{
		{store: first, want: true},
		{store: second, want: true},
		{store: first, want: false},
	} {
		allowed, retryAfter, err := testCase.store.Allow(ctx, logicalKey, 2, time.Minute)
		if err != nil {
			t.Fatalf("call %d: %v", index+1, err)
		}
		if allowed != testCase.want || retryAfter <= 0 || retryAfter > time.Minute {
			t.Fatalf("call %d allowed=%t want=%t retry_after=%s", index+1, allowed, testCase.want, retryAfter)
		}
	}
}

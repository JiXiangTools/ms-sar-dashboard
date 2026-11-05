//go:build integration

package middleware

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	platformcache "github.com/JiXiangTools/ms-sar-dashboard/internal/platform/cache"
)

func TestRedisModesSharedRateLimit(t *testing.T) {
	for _, mode := range []string{"single", "cluster"} {
		t.Run(mode, func(t *testing.T) {
			addresses := os.Getenv("SAR_TEST_REDIS_SINGLE_ADDR")
			if mode == "cluster" {
				addresses = os.Getenv("SAR_TEST_REDIS_CLUSTER_ADDRS")
			}
			if addresses == "" {
				t.Skip("run with alg-rec-offline/test/redis_modes_runner.py")
			}
			cfg := config.RedisConfig{Mode: mode, Addrs: strings.Split(addresses, ","), Password: os.Getenv("SAR_TEST_REDIS_PASSWORD"), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second}
			first, err := platformcache.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Close() })
			second, err := platformcache.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = second.Close() })
			prefix := fmt.Sprintf("integration:%d:", time.Now().UnixNano())
			stores := []*RedisRateLimitStore{NewRedisRateLimitStore(first.Client, prefix, time.Second), NewRedisRateLimitStore(second.Client, prefix, time.Second)}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for index, want := range []bool{true, true, false} {
				allowed, retry, err := stores[index%2].Allow(ctx, "fixture", 2, time.Minute)
				if err != nil || allowed != want || retry <= 0 || retry > time.Minute {
					t.Fatalf("shared rate limit call %d: allowed=%v retry=%v error=%v", index, allowed, retry, err)
				}
			}
		})
	}
}

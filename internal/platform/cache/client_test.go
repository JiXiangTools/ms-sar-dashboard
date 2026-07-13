package cache

import (
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

func TestRedisOptionsHonorContextDeadlines(t *testing.T) {
	options := buildUniversalOptions(config.RedisConfig{
		Mode:         ModeStandalone,
		Addrs:        []string{"127.0.0.1:6379"},
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	if !options.ContextTimeoutEnabled {
		t.Fatal("redis operations must honor request and rate-limit store deadlines")
	}
	if options.MaxRetries != 1 {
		t.Fatalf("max retries = %d", options.MaxRetries)
	}
}

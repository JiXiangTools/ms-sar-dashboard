package cache

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

func TestRedisClientModes(t *testing.T) {
	for _, mode := range []string{"standalone", "single", "cluster", "sentinel"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.RedisConfig{Mode: mode, Addrs: []string{"127.0.0.1:6379"}, MasterName: "mymaster"}
			client, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, cluster := client.Client.(*redis.ClusterClient)
			if cluster != (mode == "cluster") {
				t.Fatalf("mode %s constructed %T", mode, client.Client)
			}
		})
	}
	if _, err := New(config.RedisConfig{Mode: "cluster", Addrs: []string{"127.0.0.1:6379"}, DB: 8}); err == nil {
		t.Fatal("cluster must reject a non-zero database")
	}
}

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

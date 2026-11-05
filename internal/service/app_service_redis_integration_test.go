//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	platformcache "github.com/JiXiangTools/ms-sar-dashboard/internal/platform/cache"
)

func TestRedisModesAuthorizationProjection(t *testing.T) {
	for _, mode := range []string{"single", "cluster"} {
		t.Run(mode, func(t *testing.T) {
			addresses := os.Getenv("SAR_TEST_REDIS_SINGLE_ADDR")
			if mode == "cluster" {
				addresses = os.Getenv("SAR_TEST_REDIS_CLUSTER_ADDRS")
			}
			if addresses == "" {
				t.Skip("run with alg-rec-offline/test/redis_modes_runner.py")
			}
			client, err := platformcache.New(config.RedisConfig{
				Mode: mode, Addrs: strings.Split(addresses, ","), Password: os.Getenv("SAR_TEST_REDIS_PASSWORD"),
				DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			prefix := fmt.Sprintf("integration:%d:", time.Now().UnixNano())
			service := NewAppService(nil, client.Client, nil, nil, prefix, mode)
			apps := []domain.App{{ID: 100001, Name: "first", Secret: "fixture-secret"}, {ID: 100002, Name: "second", Secret: "fixture-secret-2"}}
			if err := client.Client.HSet(ctx, service.authKey(100003), "secret", "stale-fixture").Err(); err != nil {
				t.Fatal(err)
			}
			if err := service.projectionWriter(ctx, apps, []int64{100003}); err != nil {
				t.Fatalf("publish authorization projection: %v", err)
			}
			for _, app := range apps {
				secret, err := client.Client.HGet(ctx, service.authKey(app.ID), "secret").Result()
				if err != nil || secret != app.Secret {
					t.Fatal("application authorization projection mismatch")
				}
			}
			if count, err := client.Client.Exists(ctx, service.authKey(100003)).Result(); err != nil || count != 0 {
				t.Fatalf("stale authorization projection remains: %d, %v", count, err)
			}
			if index, err := client.Client.Get(ctx, service.authIndexKey()).Result(); err != nil || !strings.Contains(index, "100001") || !strings.Contains(index, "100002") {
				t.Fatal("application authorization index mismatch")
			}
		})
	}
}

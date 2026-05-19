package test

import (
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

func TestLoadTestConfig(t *testing.T) {
	cfg, err := config.Load(repoPath("configs", "test.yaml"), "test")
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}

	if cfg.App.Name != "ms-sar-dashboard" {
		t.Fatalf("unexpected app name: %s", cfg.App.Name)
	}
	if cfg.App.Env != "test" {
		t.Fatalf("unexpected app env: %s", cfg.App.Env)
	}
	if cfg.Database.DSN == "" {
		t.Fatal("database dsn must not be empty")
	}
	if len(cfg.Redis.Addrs) != 1 {
		t.Fatalf("expected one redis addr, got %d", len(cfg.Redis.Addrs))
	}
}

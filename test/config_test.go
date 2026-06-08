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
	if !cfg.SSO.Enabled {
		t.Fatal("expected test config sso to be enabled")
	}
	if cfg.SSO.APIBaseURL != "http://server.muguayun.top:8589/" {
		t.Fatalf("unexpected sso api_base_url: %s", cfg.SSO.APIBaseURL)
	}
	if cfg.SSO.RedirectURL != "http://server.muguayun.top:8588/sar-admin" {
		t.Fatalf("unexpected sso redirect_url: %s", cfg.SSO.RedirectURL)
	}
}

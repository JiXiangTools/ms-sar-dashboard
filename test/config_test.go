package test

import (
	"strings"
	"testing"
	"time"

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
	if cfg.App.ReadHeaderTimeout != 3*time.Second || cfg.App.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected app timeouts: %+v", cfg.App)
	}
	if cfg.Security.MaxRequestBodyBytes != config.DefaultMaxRequestBodyBytes {
		t.Fatalf("unexpected max request body bytes: %d", cfg.Security.MaxRequestBodyBytes)
	}
	if len(cfg.Security.TrustedProxyCIDRs) != 0 {
		t.Fatalf("unexpected trusted proxy CIDRs: %#v", cfg.Security.TrustedProxyCIDRs)
	}
	if cfg.Security.RateLimitStoreTimeout != 100*time.Millisecond {
		t.Fatalf("unexpected rate limit store timeout: %s", cfg.Security.RateLimitStoreTimeout)
	}
	if cfg.Database.DSN == "" {
		t.Fatal("database dsn must not be empty")
	}
	if cfg.Database.MigrationTimeout != 60*time.Second {
		t.Fatalf("unexpected database migration timeout: %s", cfg.Database.MigrationTimeout)
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

func TestConfigRejectsInvalidRuntimeTimeouts(t *testing.T) {
	for _, envKey := range []string{
		"MSSAR_APP_READ_HEADER_TIMEOUT",
		"MSSAR_APP_READ_TIMEOUT",
		"MSSAR_APP_WRITE_TIMEOUT",
		"MSSAR_APP_IDLE_TIMEOUT",
		"MSSAR_APP_SHUTDOWN_TIMEOUT",
		"MSSAR_DATABASE_HEALTH_CHECK_TIMEOUT",
		"MSSAR_DATABASE_MIGRATION_TIMEOUT",
		"MSSAR_REDIS_DIAL_TIMEOUT",
		"MSSAR_REDIS_READ_TIMEOUT",
		"MSSAR_REDIS_WRITE_TIMEOUT",
		"MSSAR_REDIS_HEALTH_CHECK_TIMEOUT",
		"MSSAR_SECURITY_RATE_LIMIT_STORE_TIMEOUT",
	} {
		t.Run(envKey, func(t *testing.T) {
			t.Setenv(envKey, "0s")
			if _, err := config.Load(repoPath("configs", "test.yaml"), "test"); err == nil {
				t.Fatalf("expected %s validation error", envKey)
			}
		})
	}
}

func TestSecurityConfigCanBeOverriddenFromEnvironment(t *testing.T) {
	t.Setenv("MSSAR_SECURITY_MAX_REQUEST_BODY_BYTES", "2048")
	t.Setenv("MSSAR_SECURITY_TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::/32")
	t.Setenv("MSSAR_SECURITY_RATE_LIMIT_STORE_TIMEOUT", "50ms")

	cfg, err := config.Load(repoPath("configs", "test.yaml"), "test")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Security.MaxRequestBodyBytes != 2048 {
		t.Fatalf("max request body bytes = %d", cfg.Security.MaxRequestBodyBytes)
	}
	if len(cfg.Security.TrustedProxyCIDRs) != 2 || cfg.Security.TrustedProxyCIDRs[0] != "10.0.0.0/8" || cfg.Security.TrustedProxyCIDRs[1] != "2001:db8::/32" {
		t.Fatalf("trusted proxy CIDRs = %#v", cfg.Security.TrustedProxyCIDRs)
	}
	if cfg.Security.RateLimitStoreTimeout != 50*time.Millisecond {
		t.Fatalf("rate limit store timeout = %s", cfg.Security.RateLimitStoreTimeout)
	}
}

func TestSecurityConfigRejectsInvalidValues(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "body limit", key: "MSSAR_SECURITY_MAX_REQUEST_BODY_BYTES", value: "0"},
		{name: "trusted proxy", key: "MSSAR_SECURITY_TRUSTED_PROXY_CIDRS", value: "not-a-cidr"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testCase.key, testCase.value)
			if _, err := config.Load(repoPath("configs", "test.yaml"), "test"); err == nil {
				t.Fatalf("expected %s validation error", testCase.key)
			}
		})
	}
}

func TestProductionConfigRequiresJWTSecret(t *testing.T) {
	setProductionDependencies(t)
	t.Setenv("MSSAR_AUTH_JWT_SECRET", "")
	_, err := config.Load(repoPath("configs", "prod.yaml"), "prod")
	if err == nil || !strings.Contains(err.Error(), "auth.jwt_secret") {
		t.Fatalf("expected missing production JWT secret error, got %v", err)
	}
}

func TestProductionConfigRejectsKnownJWTSecret(t *testing.T) {
	setProductionDependencies(t)
	t.Setenv("MSSAR_AUTH_JWT_SECRET", "ms-sar-dashboard-compose-secret")
	_, err := config.Load(repoPath("configs", "prod.yaml"), "prod")
	if err == nil || !strings.Contains(err.Error(), "known insecure default") {
		t.Fatalf("expected known JWT secret rejection, got %v", err)
	}
}

func TestProductionConfigAcceptsExplicitStrongJWTSecret(t *testing.T) {
	setProductionDependencies(t)
	t.Setenv("MSSAR_AUTH_JWT_SECRET", strings.Repeat("x", 32))
	cfg, err := config.Load(repoPath("configs", "prod.yaml"), "prod")
	if err != nil {
		t.Fatalf("load production config: %v", err)
	}
	if cfg.Auth.JWTSecret != strings.Repeat("x", 32) {
		t.Fatal("expected production JWT secret from environment")
	}
}

func setProductionDependencies(t *testing.T) {
	t.Helper()
	t.Setenv("MSSAR_DATABASE_DSN", "postgres://postgres:test@postgres:5432/ms_sar_dashboard?sslmode=disable")
	t.Setenv("MSSAR_REDIS_ADDRS", "redis:6379")
}

func TestStatisticsIngestTokenEnvironment(t *testing.T) {
	t.Setenv("MSSAR_STATISTICS_INGEST_TOKEN", "test-independent-ingest-token")
	cfg, err := config.Load(repoPath("configs", "test.yaml"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Statistics.IngestToken != "test-independent-ingest-token" {
		t.Fatal("statistics credential environment override was not loaded")
	}
}

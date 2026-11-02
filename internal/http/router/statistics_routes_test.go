package router

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

func TestStatisticsRoutesAuthenticationAndBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, token, body string
		limit                           int64
		want                            int
	}{
		{"admin read requires login", "GET", "/api/v1/admin/statistics?appid=100001", "", "", 1024, 401},
		{"internal read cannot use ingest credential", "GET", "/api/v1/admin/statistics?appid=100001", "internal-test-token", "", 1024, 503},
		{"ingest requires credential", "PUT", "/api/v1/internal/statistics/apps/100001/days/2026-01-01", "", "{}", 1024, 401},
		{"wrong credential", "PUT", "/api/v1/internal/statistics/apps/100001/days/2026-01-01", "wrong", "{}", 1024, 401},
		{"unknown fields", "PUT", "/api/v1/internal/statistics/apps/100001/days/2026-01-01", "internal-test-token", `{"user_id":"private"}`, 1024, 400},
		{"global limit", "PUT", "/api/v1/internal/statistics/apps/100001/days/2026-01-01", "internal-test-token", `{"too":"large"}`, 5, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{App: config.AppConfig{Env: "test"}, Security: config.SecurityConfig{MaxRequestBodyBytes: tc.limit}, Statistics: config.StatisticsConfig{IngestToken: "internal-test-token"}}
			engine := New(cfg, log.New(io.Discard, "", 0), Dependencies{Services: &service.Container{Statistics: service.NewStatisticsService(nil)}})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusOK {
				t.Fatal("untrusted request accepted")
			}
		})
	}
}

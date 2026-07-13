package test

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/router"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

func TestMetricsEndpointExposesBoundedHTTPAndAuthMetricsWithoutAuthentication(t *testing.T) {
	metricRegistry := appmetrics.New()
	logger := log.New(io.Discard, "", 0)
	apps := service.NewAppService(nil, &fakeAuthRedis{values: map[string]string{
		"id":         "100001",
		"secret":     "secret-1",
		"disabled":   "false",
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}}, audit.NewService(nil), logger)
	engine := router.New(config.Config{App: config.AppConfig{Env: "test"}}, logger, router.Dependencies{
		Services: &service.Container{Apps: apps},
		Metrics:  metricRegistry,
	})

	requests := []struct {
		method string
		target string
		body   string
	}{
		{method: http.MethodPost, target: "/api/v1/auth/app", body: `{"appid":100001,"secret":"secret-1"}`},
		{method: http.MethodPost, target: "/api/v1/auth/app", body: `{"appid":100001,"secret":"wrong"}`},
		{method: http.MethodPost, target: "/api/v1/auth/app", body: `{"appid":100001`},
		{method: http.MethodGet, target: "/api/v1/admin/app", body: ""},
		{method: http.MethodGet, target: "/unmatched/private-value?token=query-secret", body: ""},
	}
	for _, item := range requests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(item.method, item.target, strings.NewReader(item.body))
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		engine.ServeHTTP(recorder, request)
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("unexpected metrics content type: %s", contentType)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`ms_sar_dashboard_client_auth_total{outcome="success"} 1`,
		`ms_sar_dashboard_client_auth_total{outcome="denied"} 1`,
		`ms_sar_dashboard_client_auth_total{outcome="invalid_request"} 1`,
		`ms_sar_dashboard_admin_auth_total{operation="access_token",outcome="denied"} 1`,
		`route="/api/v1/auth/app"`,
		`route="/api/v1/admin/app"`,
		`route="unmatched"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics must contain %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"100001", "secret-1", "wrong", "private-value", "query-secret", "token=query"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked request data %q:\n%s", forbidden, body)
		}
	}
}

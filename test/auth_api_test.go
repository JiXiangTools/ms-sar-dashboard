package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/router"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type fakeAuthRedis struct {
	redis.UniversalClient
	values       map[string]string
	stringValues map[string]string
	err          error
}

func (r *fakeAuthRedis) HGetAll(_ context.Context, _ string) *redis.MapStringStringCmd {
	return redis.NewMapStringStringResult(r.values, r.err)
}

func (r *fakeAuthRedis) Get(_ context.Context, key string) *redis.StringCmd {
	value, ok := r.stringValues[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, r.err)
}

func TestAppAuthorizeAPI(t *testing.T) {
	engine := newAuthAPITestRouter(map[string]string{
		"id":         "100001",
		"secret":     "secret-1",
		"disabled":   "false",
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil, nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/app", strings.NewReader(`{"appid":100001,"secret":"secret-1"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	assertAuthAPIResponse(t, recorder, http.StatusOK, "success", true)

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/app", strings.NewReader(`{"appid":100001,"secret":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	assertAuthAPIResponse(t, recorder, http.StatusUnauthorized, "invalid app authorization", true)

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/app", strings.NewReader(`{"appid":100001`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	assertAuthAPIResponse(t, recorder, http.StatusBadRequest, "invalid request body", true)
}

func TestAppAuthorizeAPILogsDetailedFailureReason(t *testing.T) {
	cases := []struct {
		name        string
		values      map[string]string
		requestBody string
		wantReason  string
	}{
		{
			name:        "redis key not found",
			values:      map[string]string{},
			requestBody: `{"appid":100001,"secret":"secret-1"}`,
			wantReason:  "redis_key_not_found",
		},
		{
			name: "secret mismatch",
			values: map[string]string{
				"id":         "100001",
				"secret":     "secret-1",
				"disabled":   "false",
				"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
			},
			requestBody: `{"appid":100001,"secret":"wrong"}`,
			wantReason:  "secret_mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			engine := newAuthAPITestRouterWithLogger(tc.values, nil, nil, log.New(&logs, "", 0))

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/app", strings.NewReader(tc.requestBody))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(recorder, request)

			assertAuthAPIResponse(t, recorder, http.StatusUnauthorized, "invalid app authorization", true)
			output := logs.String()
			if !strings.Contains(output, "event=http.response_error") {
				t.Fatalf("expected response error log, got %s", output)
			}
			if !strings.Contains(output, "auth_reason="+tc.wantReason) {
				t.Fatalf("expected auth_reason %q in logs, got %s", tc.wantReason, output)
			}
		})
	}
}

func TestListAuthorizedAppsAPI(t *testing.T) {
	engine := newAuthAPITestRouter(map[string]string{
		"id":         "100001",
		"secret":     "secret-1",
		"disabled":   "false",
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, map[string]string{
		"app_auth_allappids": `[{"appid":100001,"disabled":false},{"appid":100002,"disabled":false}]`,
	}, nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/app", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    []struct {
			AppID    int64 `json:"appid"`
			Disabled bool  `json:"disabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != http.StatusOK || payload.Message != "success" {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if len(payload.Data) != 2 || payload.Data[0].AppID != 100001 || payload.Data[1].AppID != 100002 {
		t.Fatalf("unexpected data: %#v", payload.Data)
	}
}

func TestAdminSSOStatusAPI(t *testing.T) {
	engine := newSSOAPITestRouter(t, http.StatusOK, `{"status":200,"message":"success","data":{"admin_id":7,"account":"root","nickname":"管理员","permissions":["APP"]},"request_id":"0000000000000001"}`)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/sso", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Enabled  bool   `json:"enabled"`
			LoginURL string `json:"login_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != http.StatusOK || payload.Message != "success" || !payload.Data.Enabled {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if !strings.Contains(payload.Data.LoginURL, "/uc-admin?") || !strings.Contains(payload.Data.LoginURL, "redirect_url=") {
		t.Fatalf("unexpected login_url: %s", payload.Data.LoginURL)
	}
}

func TestAdminSSOLoginAPI(t *testing.T) {
	engine := newSSOAPITestRouter(t, http.StatusOK, `{"status":200,"message":"success","data":{"admin_id":7,"account":"root","nickname":"管理员","permissions":["APP"]},"request_id":"0000000000000001"}`)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/sso/login", strings.NewReader(`{"token":"cas-token-1"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			ExpiresIn   int64  `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != http.StatusOK || payload.Message != "success" {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if payload.Data.AccessToken == "" || payload.Data.TokenType != "Bearer" || payload.Data.ExpiresIn <= 0 {
		t.Fatalf("unexpected login data: %#v", payload.Data)
	}
}

func newAuthAPITestRouter(values map[string]string, stringValues map[string]string, err error) http.Handler {
	return newAuthAPITestRouterWithLogger(values, stringValues, err, log.New(io.Discard, "", 0))
}

func newAuthAPITestRouterWithLogger(values map[string]string, stringValues map[string]string, err error, logger *log.Logger) http.Handler {
	apps := service.NewAppService(nil, &fakeAuthRedis{values: values, stringValues: stringValues, err: err}, audit.NewService(nil), logger)
	return router.New(config.Config{
		App: config.AppConfig{Env: "test"},
	}, logger, router.Dependencies{
		Services: &service.Container{Apps: apps},
	})
}

func newSSOAPITestRouter(t *testing.T, statusCode int, body string) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/cas/admin" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.Close)

	tokenService := auth.NewService(config.AuthConfig{
		JWTSecret:      "ms-sar-dashboard-test-secret",
		AccessTokenTTL: 2 * time.Hour,
		Issuer:         "ms-sar-dashboard",
	})
	sso := service.NewAdminSSOService(config.SSOConfig{
		Enabled:        true,
		AdminUIURL:     upstream.URL + "/uc-admin",
		APIBaseURL:     upstream.URL,
		AppID:          "100001",
		AppSecret:      "secret-1",
		RedirectURL:    "http://127.0.0.1:18081/sar-admin",
		RequestTimeout: time.Second,
	}, tokenService, audit.NewService(nil), log.New(io.Discard, "", 0))

	return router.New(config.Config{
		App: config.AppConfig{Env: "test"},
	}, log.New(io.Discard, "", 0), router.Dependencies{
		Services: &service.Container{
			Auth: service.NewAdminAuthService(nil, tokenService, audit.NewService(nil), log.New(io.Discard, "", 0)),
			SSO:  sso,
		},
	})
}

func assertAuthAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder, expectedStatus int, expectedMessage string, expectNullData bool) {
	t.Helper()
	if recorder.Code != expectedStatus {
		t.Fatalf("expected HTTP %d, got %d body=%s", expectedStatus, recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != expectedStatus || payload.Message != expectedMessage {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if expectNullData && payload.Data != nil {
		t.Fatalf("expected null data, got %#v", payload.Data)
	}
}

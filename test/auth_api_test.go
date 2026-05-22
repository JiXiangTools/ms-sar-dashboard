package test

import (
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
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/router"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type fakeAuthRedis struct {
	redis.UniversalClient
	values map[string]string
	err    error
}

func (r *fakeAuthRedis) HGetAll(_ context.Context, _ string) *redis.MapStringStringCmd {
	return redis.NewMapStringStringResult(r.values, r.err)
}

func TestAppAuthorizeAPI(t *testing.T) {
	engine := newAuthAPITestRouter(map[string]string{
		"id":         "100001",
		"secret":     "secret-1",
		"disabled":   "false",
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil)

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

func newAuthAPITestRouter(values map[string]string, err error) http.Handler {
	apps := service.NewAppService(nil, &fakeAuthRedis{values: values, err: err}, audit.NewService(nil), log.New(io.Discard, "", 0))
	return router.New(config.Config{
		App: config.AppConfig{Env: "test"},
	}, log.New(io.Discard, "", 0), router.Dependencies{
		Services: &service.Container{Apps: apps},
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

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

func TestRouterAppliesBodyLimitBeforeAdminHandlers(t *testing.T) {
	engine := New(config.Config{
		App: config.AppConfig{Env: "test"},
		Security: config.SecurityConfig{
			MaxRequestBodyBytes: 16,
		},
	}, log.New(io.Discard, "", 0), Dependencies{Services: &service.Container{}})

	for _, chunked := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", strings.NewReader(`{"name":"alice","password":"long"}`))
		request.Header.Set("Content-Type", "application/json")
		if chunked {
			request.ContentLength = -1
			request.TransferEncoding = []string{"chunked"}
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("chunked=%t status=%d body=%s", chunked, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("x-request-id") == "" {
			t.Fatalf("chunked=%t response did not include request id", chunked)
		}
	}
}

package test

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/router"
)

func newAdminUITestRouter() http.Handler {
	return router.New(config.Config{
		App: config.AppConfig{Env: "test"},
	}, log.New(io.Discard, "", 0), router.Dependencies{})
}

func TestAdminUIEndpointServesLoginEntry(t *testing.T) {
	engine := newAdminUITestRouter()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sar-admin", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected content type text/html, got %s", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected admin ui html to disable cache, got %s", recorder.Header().Get("Cache-Control"))
	}

	body := recorder.Body.String()
	required := []string{
		"ms-sar-dashboard / sar-admin",
		`<body class="auth-backdrop-open auth-modal-open">`,
		`id="login-form"`,
		`id="secret-reveal-panel"`,
		`role="dialog"`,
		"管理员登录",
		`id="login-error"`,
		`id="session-panel"`,
		`/sar-admin/assets/app.js?v=20260520-secret`,
	}
	for _, value := range required {
		if !strings.Contains(body, value) {
			t.Fatalf("expected admin ui body to contain %q", value)
		}
	}
	if strings.Contains(body, "管理端入口已就绪") {
		t.Fatalf("expected placeholder-only admin ui copy to be removed")
	}
}

func TestAdminUIAssetsIncludeAuthLogic(t *testing.T) {
	engine := newAdminUITestRouter()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sar-admin/assets/app.js", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected app script to disable cache, got %s", recorder.Header().Get("Cache-Control"))
	}

	body := recorder.Body.String()
	required := []string{
		`const storageKey = "mssar.admin.session"`,
		`/api/v1/admin/auth/login`,
		`Authorization: ` + "`Bearer ${state.accessToken}`",
		`sessionStorage`,
		`generateAppSecret`,
		`window.crypto.getRandomValues`,
		`secret-reveal-panel`,
		`regenerate-app-secret`,
		`copy-secret`,
		`连接服务失败，请确认 sar-admin 服务已启动，并刷新页面后重试。`,
	}
	for _, value := range required {
		if !strings.Contains(body, value) {
			t.Fatalf("expected app script to contain %q", value)
		}
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/sar-admin/assets/app.css", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("expected text/css content type, got %s", recorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(recorder.Body.String(), "body.auth-modal-open .login-form") {
		t.Fatalf("expected css asset to include auth modal styling")
	}
}

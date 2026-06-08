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
		`id="sso-login-button"`,
		`id="close-login-modal" aria-label="关闭登录弹框" disabled`,
		`id="secret-reveal-panel"`,
		`role="dialog"`,
		"管理员登录",
		`id="login-error"`,
		`id="session-panel"`,
		`/sar-admin/assets/app.js?v=20260608-sso`,
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
		`/api/v1/admin/auth/sso`,
		`/api/v1/admin/auth/sso/login`,
		`Authorization: ` + "`Bearer ${state.accessToken}`",
		`sessionStorage`,
		`generateAppSecret`,
		`window.crypto.getRandomValues`,
		`secret-reveal-panel`,
		`regenerate-app-secret`,
		`copy-secret`,
		`originalSecret`,
		`secretChanged`,
		`secret: mode === "create" ? generateAppSecret() : item?.secret || ""`,
		`留空保持当前密钥`,
		`const showLogin = !loggedIn;`,
		`closeLoginButton.disabled = !loggedIn`,
		`id="rec-debug-type"`,
		`data-rec-field="period"`,
		`<option value="quarter"`,
		`<option value="all"`,
		`<option value="raw"`,
		`GET /user/xxx`,
		`/api/v1/admin/debug/es/raw`,
		`/api/v1/admin/debug/rec`,
		`连接服务失败，请确认 sar-admin 服务已启动，并刷新页面后重试。`,
		`readSSOToken`,
		`单点登录`,
	}
	for _, value := range required {
		if !strings.Contains(body, value) {
			t.Fatalf("expected app script to contain %q", value)
		}
	}
	if strings.Contains(body, `const showLogin = !loggedIn && state.loginOpen;`) {
		t.Fatalf("expected login modal to be mandatory when there is no session")
	}
	for _, value := range []string{`<option value="key">`, `Redis Key`, `key: String(values.key`} {
		if strings.Contains(body, value) {
			t.Fatalf("expected app script to remove redis key debug option %q", value)
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
	if !strings.Contains(recorder.Body.String(), ".login-actions") {
		t.Fatalf("expected css asset to include sso login action layout")
	}
}

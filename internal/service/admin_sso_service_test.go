package service

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

func TestAdminSSOServiceStatusBuildsLoginURL(t *testing.T) {
	service := NewAdminSSOService(config.SSOConfig{
		Enabled:        true,
		AdminUIURL:     "https://uc.example.com/uc-admin",
		APIBaseURL:     "https://uc.example.com",
		AppID:          "100001",
		AppSecret:      "secret-1",
		RedirectURL:    "https://sar.example.com/sar-admin",
		RequestTimeout: time.Second,
	}, auth.NewService(config.AuthConfig{JWTSecret: "test-secret", Issuer: "ms-sar-dashboard"}), audit.NewService(nil), log.New(io.Discard, "", 0))

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Enabled {
		t.Fatal("expected sso enabled")
	}

	parsed, err := url.Parse(status.LoginURL)
	if err != nil {
		t.Fatalf("parse login url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "uc.example.com" || parsed.Path != "/uc-admin" {
		t.Fatalf("unexpected login url: %s", status.LoginURL)
	}
	query := parsed.Query()
	if query.Get("appid") != "100001" {
		t.Fatalf("unexpected appid: %s", query.Get("appid"))
	}
	if query.Get("redirect_url") != "https://sar.example.com/sar-admin" {
		t.Fatalf("unexpected redirect_url: %s", query.Get("redirect_url"))
	}
	if query.Get("appsecret") != service.casAppSignature(time.Now()) {
		t.Fatalf("unexpected appsecret: %s", query.Get("appsecret"))
	}
}

func TestAdminSSOServiceLoginIssuesLocalAccessToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/cas/admin" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Token != "cas-token-1" {
			t.Fatalf("unexpected token: %s", req.Token)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":200,"message":"success","data":{"admin_id":8,"account":"root","nickname":"管理员","permissions":["APP"]},"request_id":"0000000000000001"}`))
	}))
	defer upstream.Close()

	tokenService := auth.NewService(config.AuthConfig{JWTSecret: "test-secret", Issuer: "ms-sar-dashboard"})
	sso := NewAdminSSOService(config.SSOConfig{
		Enabled:        true,
		AdminUIURL:     upstream.URL + "/uc-admin",
		APIBaseURL:     upstream.URL,
		AppID:          "100001",
		AppSecret:      "secret-1",
		RedirectURL:    "http://127.0.0.1:8081/sar-admin",
		RequestTimeout: time.Second,
	}, tokenService, audit.NewService(nil), log.New(io.Discard, "", 0))
	authService := NewAdminAuthService(nil, tokenService, audit.NewService(nil), log.New(io.Discard, "", 0))

	result, err := sso.Login(context.Background(), AdminSSOLoginInput{Token: "cas-token-1"})
	if err != nil {
		t.Fatalf("sso login: %v", err)
	}
	if strings.TrimSpace(result.AccessToken) == "" || result.TokenType != "Bearer" || result.ExpiresIn <= 0 {
		t.Fatalf("unexpected login result: %#v", result)
	}

	admin, err := authService.AuthenticateAccessToken(context.Background(), result.AccessToken)
	if err != nil {
		t.Fatalf("authenticate sso access token: %v", err)
	}
	if admin.ID != 8 || admin.Name != "root" || admin.Nickname != "管理员" {
		t.Fatalf("unexpected admin: %#v", admin)
	}
}

func TestAdminSSOServiceLoginRejectsInvalidCASToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"message":"invalid cas token","data":null,"request_id":"0000000000000001"}`))
	}))
	defer upstream.Close()

	sso := NewAdminSSOService(config.SSOConfig{
		Enabled:        true,
		AdminUIURL:     upstream.URL + "/uc-admin",
		APIBaseURL:     upstream.URL,
		AppID:          "100001",
		AppSecret:      "secret-1",
		RedirectURL:    "http://127.0.0.1:8081/sar-admin",
		RequestTimeout: time.Second,
	}, auth.NewService(config.AuthConfig{JWTSecret: "test-secret", Issuer: "ms-sar-dashboard"}), audit.NewService(nil), log.New(io.Discard, "", 0))

	if _, err := sso.Login(context.Background(), AdminSSOLoginInput{Token: "bad-token"}); err == nil || !strings.Contains(err.Error(), "invalid sso token") {
		t.Fatalf("expected invalid sso token, got %v", err)
	}
}

package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/auth"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/requestid"
)

type AdminSSOService struct {
	cfg        config.SSOConfig
	httpClient *http.Client
	tokens     *auth.Service
	audit      *audit.Service
	logger     *log.Logger
}

func NewAdminSSOService(cfg config.SSOConfig, tokens *auth.Service, auditSvc *audit.Service, logger *log.Logger) *AdminSSOService {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &AdminSSOService{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
		tokens:     tokens,
		audit:      auditSvc,
		logger:     logger,
	}
}

func (s *AdminSSOService) Status(ctx context.Context) (AdminSSOStatus, error) {
	if s == nil || !s.cfg.Enabled {
		return AdminSSOStatus{Enabled: false}, nil
	}
	loginURL, err := s.loginURL()
	if err != nil {
		return AdminSSOStatus{}, apperror.Internal("sso is not configured", err)
	}
	return AdminSSOStatus{
		Enabled:  true,
		LoginURL: loginURL,
	}, nil
}

func (s *AdminSSOService) Login(ctx context.Context, input AdminSSOLoginInput) (LoginOutput, error) {
	startedAt := time.Now()
	if s == nil || !s.cfg.Enabled {
		return LoginOutput{}, apperror.Forbidden("sso disabled", nil)
	}
	if _, err := s.loginURL(); err != nil {
		return LoginOutput{}, apperror.Internal("sso is not configured", err)
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
			"source": "sso",
			"reason": "token_required",
		})
		return LoginOutput{}, apperror.Unauthorized("invalid sso token", nil)
	}

	admin, err := s.fetchCASAdmin(ctx, token)
	if err != nil {
		_ = s.audit.Record(ctx, 0, "AUTH", "LOGIN_FAILED", map[string]any{
			"source": "sso",
			"reason": "invalid_token",
		})
		logx.Warn(s.logger, ctx, startedAt, "admin.sso.login.failed", "admin sso login failed",
			logx.Err(err),
		)
		return LoginOutput{}, err
	}

	accessToken, err := s.tokens.IssueSSOAccessToken(admin.AdminID, admin.Account, admin.Nickname)
	if err != nil {
		_ = s.audit.Record(ctx, admin.AdminID, "AUTH", "LOGIN_FAILED", map[string]any{
			"source": "sso",
			"name":   admin.Account,
			"reason": "token_issue_failed",
		})
		return LoginOutput{}, apperror.Internal("internal server error", err)
	}

	_ = s.audit.Record(ctx, admin.AdminID, "AUTH", "LOGIN_SUCCESS", map[string]any{
		"source": "sso",
		"name":   admin.Account,
	})
	logx.Info(s.logger, ctx, startedAt, "admin.sso.login.success", "admin sso login success",
		logx.Int64("admin_id", admin.AdminID),
		logx.String("name", admin.Account),
	)
	return LoginOutput{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.AccessTokenTTL().Seconds()),
	}, nil
}

func (s *AdminSSOService) fetchCASAdmin(ctx context.Context, token string) (AdminSSOAdmin, error) {
	endpoint, err := s.casAdminEndpoint()
	if err != nil {
		return AdminSSOAdmin{}, apperror.Internal("sso is not configured", err)
	}

	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return AdminSSOAdmin{}, apperror.Internal("internal server error", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return AdminSSOAdmin{}, apperror.Internal("internal server error", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID := requestid.FromContext(ctx); requestID != "" {
		req.Header.Set(requestid.HeaderName, requestID)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return AdminSSOAdmin{}, apperror.Internal("sso request failed", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AdminSSOAdmin{}, apperror.Internal("sso request failed", err)
	}

	var envelope struct {
		Status  int             `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return AdminSSOAdmin{}, apperror.Internal("invalid sso response", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || envelope.Status == http.StatusUnauthorized {
		return AdminSSOAdmin{}, apperror.Unauthorized("invalid sso token", nil)
	}
	if resp.StatusCode != http.StatusOK || envelope.Status != http.StatusOK {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = "sso request failed"
		}
		return AdminSSOAdmin{}, apperror.Internal(message, errors.New(message))
	}

	var admin AdminSSOAdmin
	if err := json.Unmarshal(envelope.Data, &admin); err != nil {
		return AdminSSOAdmin{}, apperror.Internal("invalid sso response", err)
	}
	admin.Account = strings.TrimSpace(admin.Account)
	admin.Nickname = strings.TrimSpace(admin.Nickname)
	if admin.AdminID <= 0 || admin.Account == "" {
		return AdminSSOAdmin{}, apperror.Internal("invalid sso response", errors.New("missing admin identity"))
	}
	return admin, nil
}

func (s *AdminSSOService) loginURL() (string, error) {
	if err := s.validateConfig(); err != nil {
		return "", err
	}
	target, err := url.Parse(strings.TrimSpace(s.cfg.AdminUIURL))
	if err != nil {
		return "", err
	}
	query := target.Query()
	query.Set("redirect_url", strings.TrimSpace(s.cfg.RedirectURL))
	query.Set("appid", strings.TrimSpace(s.cfg.AppID))
	query.Set("appsecret", s.casAppSignature(time.Now()))
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (s *AdminSSOService) casAdminEndpoint() (string, error) {
	if err := s.validateConfig(); err != nil {
		return "", err
	}
	base, err := url.Parse(strings.TrimSpace(s.cfg.APIBaseURL))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(base.Path, "/") + "/api/v1/admin/cas/admin"
	base.Path = path
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func (s *AdminSSOService) casAppSignature(now time.Time) string {
	raw := fmt.Sprintf("%s%s%s", strings.TrimSpace(s.cfg.AppID), strings.TrimSpace(s.cfg.AppSecret), now.Format("20060102"))
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *AdminSSOService) validateConfig() error {
	if s == nil {
		return errors.New("sso service is not configured")
	}
	if !s.cfg.Enabled {
		return errors.New("sso is disabled")
	}
	if !validHTTPURL(s.cfg.AdminUIURL) {
		return errors.New("invalid sso admin_ui_url")
	}
	if !validHTTPURL(s.cfg.APIBaseURL) {
		return errors.New("invalid sso api_base_url")
	}
	if strings.TrimSpace(s.cfg.AppID) == "" {
		return errors.New("invalid sso app_id")
	}
	if strings.TrimSpace(s.cfg.AppSecret) == "" {
		return errors.New("invalid sso app_secret")
	}
	if !validHTTPURL(s.cfg.RedirectURL) {
		return errors.New("invalid sso redirect_url")
	}
	return nil
}

func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

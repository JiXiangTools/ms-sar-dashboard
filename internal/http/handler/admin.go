package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/middleware"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type AdminHandler struct {
	auth   *service.AdminAuthService
	sso    *service.AdminSSOService
	apps   *service.AppService
	logs   *service.LogService
	debug  *service.DebugService
	metric *appmetrics.Registry
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type ssoLoginRequest struct {
	Token string `json:"token"`
}

type createAppRequest struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
	Remark string `json:"remark"`
}

type updateAppRequest struct {
	Name   *string `json:"name"`
	Secret *string `json:"secret"`
	Remark *string `json:"remark"`
}

type appResponse struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Secret           string    `json:"secret,omitempty"`
	SecretConfigured bool      `json:"secret_configured"`
	Remark           string    `json:"remark"`
	Disabled         bool      `json:"disabled"`
	CreateTime       time.Time `json:"create_time"`
	LastUpdateTime   time.Time `json:"last_update_time"`
}

type appPageResponse struct {
	Items    []appResponse `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Total    int64         `json:"total"`
}

type appAuthorizeRequest struct {
	AppID  int64  `json:"appid"`
	Secret string `json:"secret"`
}

type listAppsQuery struct {
	AppID    *int64 `form:"app_id"`
	Name     string `form:"name"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type listLogsQuery struct {
	Cate     string `form:"cate"`
	Type     string `form:"type"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type debugRecRequest struct {
	Type    string   `json:"type"`
	AppID   string   `json:"appid"`
	ItemID  string   `json:"item_id"`
	UserID  string   `json:"user_id"`
	Period  string   `json:"period"`
	Size    int      `json:"size"`
	Exclude []string `json:"exclude"`
}

type esRawDebugRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
	Input  string `json:"input"`
}

func NewAdminHandler(auth *service.AdminAuthService, sso *service.AdminSSOService, apps *service.AppService, logs *service.LogService, debug *service.DebugService, metricRegistry *appmetrics.Registry) *AdminHandler {
	return &AdminHandler{auth: auth, sso: sso, apps: apps, logs: logs, debug: debug, metric: metricRegistry}
}

func (h *AdminHandler) Login(c *gin.Context) {
	req, ok := bindJSON[loginRequest](c)
	if !ok {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthLogin, appmetrics.OutcomeInvalidRequest)
		return
	}
	if h.auth == nil {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthLogin, appmetrics.OutcomeError)
		response.Error(c, http.StatusInternalServerError, "internal server error", nil)
		return
	}

	result, err := h.auth.Login(c.Request.Context(), service.LoginInput{
		Name:     req.Name,
		Password: req.Password,
	})
	if err != nil {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthLogin, metricOutcome(err))
		writeError(c, err)
		return
	}
	h.metric.ObserveAdminAuth(appmetrics.AdminAuthLogin, appmetrics.OutcomeSuccess)
	response.Success(c, result)
}

func (h *AdminHandler) Logout(c *gin.Context) {
	admin, ok := c.Get(middleware.ContextKeyAdmin)
	if ok {
		if typed, ok := admin.(domain.Admin); ok {
			token, _ := c.Get(middleware.ContextKeyAccessToken)
			if err := h.auth.Logout(c.Request.Context(), typed, strings.TrimSpace(fmt.Sprint(token))); err != nil {
				writeError(c, err)
				return
			}
		}
	}
	response.Success(c, gin.H{"success": true})
}

func (h *AdminHandler) SSOStatus(c *gin.Context) {
	if h.sso == nil {
		response.Success(c, service.AdminSSOStatus{Enabled: false})
		return
	}
	status, err := h.sso.Status(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, status)
}

func (h *AdminHandler) SSOLogin(c *gin.Context) {
	req, ok := bindJSON[ssoLoginRequest](c)
	if !ok {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthSSOLogin, appmetrics.OutcomeInvalidRequest)
		return
	}
	if h.sso == nil {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthSSOLogin, appmetrics.OutcomeDenied)
		response.Error(c, http.StatusForbidden, "sso disabled", nil)
		return
	}
	result, err := h.sso.Login(c.Request.Context(), service.AdminSSOLoginInput{
		Token: req.Token,
	})
	if err != nil {
		h.metric.ObserveAdminAuth(appmetrics.AdminAuthSSOLogin, metricOutcome(err))
		writeError(c, err)
		return
	}
	h.metric.ObserveAdminAuth(appmetrics.AdminAuthSSOLogin, appmetrics.OutcomeSuccess)
	response.Success(c, result)
}

func (h *AdminHandler) AppAuthorize(c *gin.Context) {
	req, ok := bindJSON[appAuthorizeRequest](c)
	if !ok {
		h.metric.ObserveClientAuth(appmetrics.OutcomeInvalidRequest)
		return
	}
	if req.AppID <= 0 || strings.TrimSpace(req.Secret) == "" {
		h.metric.ObserveClientAuth(appmetrics.OutcomeInvalidRequest)
		middleware.WriteAppAuthError(c, req.AppID, "appid_or_secret_required")
		return
	}
	if h.apps == nil {
		h.metric.ObserveClientAuth(appmetrics.OutcomeError)
		response.Error(c, http.StatusInternalServerError, "internal server error", nil)
		return
	}
	if _, err := h.apps.Authorize(c.Request.Context(), strconv.FormatInt(req.AppID, 10), req.Secret); err != nil {
		h.metric.ObserveClientAuth(metricOutcome(err))
		middleware.WriteAppAuthFailure(c, req.AppID, err)
		return
	}
	h.metric.ObserveClientAuth(appmetrics.OutcomeSuccess)
	response.Success(c, nil)
}

func (h *AdminHandler) ListAuthorizedApps(c *gin.Context) {
	if h.apps == nil {
		response.Error(c, http.StatusInternalServerError, "internal server error", nil)
		return
	}
	data, err := h.apps.ListAuthorizedApps(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ListApps(c *gin.Context) {
	query, ok := bindQuery[listAppsQuery](c)
	if !ok {
		return
	}
	page, err := h.apps.List(c.Request.Context(), service.AppListQuery{
		AppID:    query.AppID,
		Name:     query.Name,
		Page:     query.Page,
		PageSize: query.PageSize,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	items := make([]appResponse, 0, len(page.Items))
	for _, app := range page.Items {
		items = append(items, newAppResponse(app, false))
	}
	response.Success(c, appPageResponse{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

func (h *AdminHandler) CreateApp(c *gin.Context) {
	req, ok := bindJSON[createAppRequest](c)
	if !ok {
		return
	}
	app, err := h.apps.Create(c.Request.Context(), adminID(c), service.AppCreateInput{
		Name:   req.Name,
		Secret: req.Secret,
		Remark: req.Remark,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, newAppResponse(app, true))
}

func (h *AdminHandler) UpdateApp(c *gin.Context) {
	appID, ok := int64Param(c, "app_id")
	if !ok {
		return
	}
	req, ok := bindJSON[updateAppRequest](c)
	if !ok {
		return
	}
	app, err := h.apps.Update(c.Request.Context(), adminID(c), appID, service.AppUpdateInput{
		Name:   req.Name,
		Secret: req.Secret,
		Remark: req.Remark,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, newAppResponse(app, req.Secret != nil))
}

func (h *AdminHandler) DeleteApp(c *gin.Context) {
	appID, ok := int64Param(c, "app_id")
	if !ok {
		return
	}
	if err := h.apps.Delete(c.Request.Context(), adminID(c), appID); err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *AdminHandler) ReconcileAppAuthorization(c *gin.Context) {
	if h.apps == nil {
		h.metric.ObserveProjectionReconcile(appmetrics.ProjectionAdminReconcile, appmetrics.OutcomeError)
		response.Error(c, http.StatusInternalServerError, "internal server error", nil)
		return
	}
	if err := h.apps.ReconcileAuthorizationProjection(c.Request.Context(), adminID(c)); err != nil {
		h.metric.ObserveProjectionReconcile(appmetrics.ProjectionAdminReconcile, metricOutcome(err))
		writeError(c, err)
		return
	}
	h.metric.ObserveProjectionReconcile(appmetrics.ProjectionAdminReconcile, appmetrics.OutcomeSuccess)
	response.Success(c, gin.H{"success": true})
}

func (h *AdminHandler) ListLogs(c *gin.Context) {
	query, ok := bindQuery[listLogsQuery](c)
	if !ok {
		return
	}
	logsPage, err := h.logs.List(c.Request.Context(), service.AdminLogQuery{
		Cate:     query.Cate,
		Type:     query.Type,
		Page:     query.Page,
		PageSize: query.PageSize,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, logsPage)
}

func (h *AdminHandler) ESIndexInfo(c *gin.Context) {
	appID, ok := int64Param(c, "appid")
	if !ok {
		return
	}
	data, err := h.debug.IndexInfo(c.Request.Context(), adminID(c), appID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ESDocument(c *gin.Context) {
	appID, ok := int64Param(c, "appid")
	if !ok {
		return
	}
	data, err := h.debug.Document(c.Request.Context(), adminID(c), appID, c.Param("item_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ESSearch(c *gin.Context) {
	appID, ok := int64Param(c, "appid")
	if !ok {
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	data, err := h.debug.Search(c.Request.Context(), adminID(c), appID, body)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ESRaw(c *gin.Context) {
	req, ok := bindJSON[esRawDebugRequest](c)
	if !ok {
		return
	}
	debugReq, err := req.toServiceRequest()
	if err != nil {
		writeError(c, err)
		return
	}
	data, err := h.debug.RawES(c.Request.Context(), adminID(c), debugReq)
	if err != nil {
		writeError(c, err)
		return
	}
	response.SuccessRaw(c, data)
}

func (h *AdminHandler) RecDebug(c *gin.Context) {
	req, ok := bindJSON[debugRecRequest](c)
	if !ok {
		return
	}
	data, err := h.debug.Recommend(c.Request.Context(), adminID(c), req.toServiceRequest())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (r debugRecRequest) toServiceRequest() service.RecDebugRequest {
	return service.RecDebugRequest{
		Type:    r.Type,
		AppID:   r.AppID,
		ItemID:  r.ItemID,
		UserID:  r.UserID,
		Period:  r.Period,
		Size:    r.Size,
		Exclude: r.Exclude,
	}
}

func (r esRawDebugRequest) toServiceRequest() (service.ESRawRequest, error) {
	if strings.TrimSpace(r.Input) != "" {
		return service.ParseESRawConsoleInput(r.Input)
	}
	return service.ESRawRequest{
		Method: r.Method,
		Path:   r.Path,
		Body:   r.Body,
	}, nil
}

func bindJSON[T any](c *gin.Context) (T, bool) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return req, false
	}
	return req, true
}

func bindQuery[T any](c *gin.Context) (T, bool) {
	var query T
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid query", nil)
		return query, false
	}
	return query, true
}

func int64Param(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid "+name, nil)
		return 0, false
	}
	return value, true
}

func adminID(c *gin.Context) int64 {
	return c.GetInt64(middleware.ContextKeyAdminID)
}

func newAppResponse(app domain.App, includeSecret bool) appResponse {
	result := appResponse{
		ID:               app.ID,
		Name:             app.Name,
		SecretConfigured: strings.TrimSpace(app.Secret) != "",
		Remark:           app.Remark,
		Disabled:         app.Disabled,
		CreateTime:       app.CreateTime,
		LastUpdateTime:   app.LastUpdateTime,
	}
	if includeSecret {
		result.Secret = app.Secret
	}
	return result
}

func metricOutcome(err error) appmetrics.Outcome {
	if err == nil {
		return appmetrics.OutcomeSuccess
	}
	if appErr, ok := apperror.As(err); ok {
		return appmetrics.OutcomeFromStatus(appErr.HTTPStatus)
	}
	return appmetrics.OutcomeError
}

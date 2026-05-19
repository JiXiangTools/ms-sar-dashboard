package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/middleware"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type AdminHandler struct {
	auth  *service.AdminAuthService
	apps  *service.AppService
	logs  *service.LogService
	debug *service.DebugService
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
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
	Key     string   `json:"key"`
	Size    int      `json:"size"`
	Exclude []string `json:"exclude"`
}

func NewAdminHandler(auth *service.AdminAuthService, apps *service.AppService, logs *service.LogService, debug *service.DebugService) *AdminHandler {
	return &AdminHandler{auth: auth, apps: apps, logs: logs, debug: debug}
}

func (h *AdminHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	result, err := h.auth.Login(c.Request.Context(), service.LoginInput{
		Name:     req.Name,
		Password: req.Password,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AdminHandler) Logout(c *gin.Context) {
	admin, ok := c.Get(middleware.ContextKeyAdmin)
	if ok {
		if typed, ok := admin.(domain.Admin); ok {
			_ = h.auth.Logout(c.Request.Context(), typed)
		}
	}
	response.Success(c, gin.H{"success": true})
}

func (h *AdminHandler) ListApps(c *gin.Context) {
	var query listAppsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid query", nil)
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
	response.Success(c, page)
}

func (h *AdminHandler) CreateApp(c *gin.Context) {
	var req createAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	app, err := h.apps.Create(c.Request.Context(), adminID, service.AppCreateInput{
		Name:   req.Name,
		Secret: req.Secret,
		Remark: req.Remark,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, app)
}

func (h *AdminHandler) UpdateApp(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("app_id"), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid app_id", nil)
		return
	}
	var req updateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	app, err := h.apps.Update(c.Request.Context(), adminID, appID, service.AppUpdateInput{
		Name:   req.Name,
		Secret: req.Secret,
		Remark: req.Remark,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, app)
}

func (h *AdminHandler) DeleteApp(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("app_id"), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid app_id", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	if err := h.apps.Delete(c.Request.Context(), adminID, appID); err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *AdminHandler) ListLogs(c *gin.Context) {
	var query listLogsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid query", nil)
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
	appID, err := strconv.ParseInt(c.Param("appid"), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid appid", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	data, err := h.debug.IndexInfo(c.Request.Context(), adminID, appID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ESDocument(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("appid"), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid appid", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	data, err := h.debug.Document(c.Request.Context(), adminID, appID, c.Param("item_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) ESSearch(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("appid"), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid appid", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	body, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	data, err := h.debug.Search(c.Request.Context(), adminID, appID, body)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

func (h *AdminHandler) RecDebug(c *gin.Context) {
	var req debugRecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body", nil)
		return
	}
	adminID := c.GetInt64(middleware.ContextKeyAdminID)
	data, err := h.debug.Recommend(c.Request.Context(), adminID, service.RecDebugRequest{
		Type:    req.Type,
		AppID:   req.AppID,
		ItemID:  req.ItemID,
		UserID:  req.UserID,
		Period:  req.Period,
		Key:     req.Key,
		Size:    req.Size,
		Exclude: req.Exclude,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, data)
}

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/health"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

type HealthHandler struct {
	service *health.Service
}

func NewHealthHandler(service *health.Service) *HealthHandler {
	return &HealthHandler{service: service}
}

func (h *HealthHandler) Get(c *gin.Context) {
	if h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "health service unavailable", nil)
		return
	}
	report := h.service.Check(c.Request.Context())
	if report.Status == "up" {
		response.Success(c, report)
		return
	}
	response.Error(c, http.StatusServiceUnavailable, "degraded", report)
}

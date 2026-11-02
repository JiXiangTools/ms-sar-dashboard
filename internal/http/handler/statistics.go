package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
	"github.com/gin-gonic/gin"
)

type StatisticsHandler struct {
	service *service.StatisticsService
	token   string
}

func NewStatisticsHandler(s *service.StatisticsService, token string) *StatisticsHandler {
	return &StatisticsHandler{service: s, token: token}
}
func (h *StatisticsHandler) Put(c *gin.Context) {
	if h.token == "" || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "statistics ingestion is not configured", nil)
		return
	}
	expected := sha256.Sum256([]byte(h.token))
	supplied := sha256.Sum256([]byte(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")))
	if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
		response.Error(c, http.StatusUnauthorized, "invalid statistics token", nil)
		return
	}
	var payload domain.StatisticsDay
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid statistics payload", nil)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		response.Error(c, http.StatusBadRequest, "invalid statistics payload", nil)
		return
	}
	changed, err := h.service.Put(c.Request.Context(), c.Param("appid"), c.Param("date"), payload)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"changed": changed, "revision": payload.Revision})
}
func (h *StatisticsHandler) Query(c *gin.Context) {
	if h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "statistics unavailable", nil)
		return
	}
	result, err := h.service.Query(c.Request.Context(), c.Query("appid"), c.Query("from"), c.Query("to"), c.DefaultQuery("granularity", "day"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

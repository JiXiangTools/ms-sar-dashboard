package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
	"github.com/gin-gonic/gin"
)

type RecommendationHandler struct {
	service *service.RecommendationService
}

func NewRecommendationHandler(s *service.RecommendationService) *RecommendationHandler {
	return &RecommendationHandler{service: s}
}
func recommendationJSON[T any](c *gin.Context) (T, bool) {
	var v T
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	err := d.Decode(&v)
	var extra any
	if err != nil || d.Decode(&extra) != io.EOF {
		response.Error(c, http.StatusBadRequest, "invalid recommendation configuration", nil)
		return v, false
	}
	return v, true
}
func (h *RecommendationHandler) Sources(c *gin.Context) {
	v, e := h.service.Sources(c.Request.Context())
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, v)
}
func (h *RecommendationHandler) SaveSource(c *gin.Context) {
	v, ok := recommendationJSON[domain.RecallSource](c)
	if !ok {
		return
	}
	out, e := h.service.SaveSource(c.Request.Context(), adminID(c), v)
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *RecommendationHandler) Workspace(c *gin.Context) {
	v, e := h.service.Workspace(c.Request.Context(), c.Param("appid"))
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, v)
}
func (h *RecommendationHandler) SaveStrategy(c *gin.Context) {
	v, ok := recommendationJSON[domain.RecommendationStrategy](c)
	if !ok {
		return
	}
	v.AppID = c.Param("appid")
	out, e := h.service.SaveStrategy(c.Request.Context(), adminID(c), v)
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *RecommendationHandler) SaveExperiment(c *gin.Context) {
	v, ok := recommendationJSON[domain.RecommendationExperiment](c)
	if !ok {
		return
	}
	v.AppID = c.Param("appid")
	out, e := h.service.SaveExperiment(c.Request.Context(), adminID(c), v)
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, out)
}
func (h *RecommendationHandler) Publish(c *gin.Context) {
	v, ok := recommendationJSON[struct {
		ExperimentID     string `json:"experiment_id"`
		Version          int64  `json:"experiment_version"`
		ExpectedRevision int64  `json:"expected_revision"`
	}](c)
	if !ok {
		return
	}
	out, e := h.service.Publish(c.Request.Context(), adminID(c), c.Param("appid"), v.ExperimentID, v.Version, v.ExpectedRevision)
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, out)
}

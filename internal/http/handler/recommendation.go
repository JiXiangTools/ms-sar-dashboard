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

type recommendationPublicationRequest struct {
	ExperimentID *string `json:"experiment_id"`
}

func (h *RecommendationHandler) Publish(c *gin.Context) {
	v, ok := recommendationJSON[recommendationPublicationRequest](c)
	if !ok {
		return
	}
	if v.ExperimentID == nil {
		response.Error(c, http.StatusBadRequest, "experiment_id must be an explicit string; use an empty string to stop", nil)
		return
	}
	out, e := h.service.Publish(c.Request.Context(), adminID(c), c.Param("appid"), *v.ExperimentID)
	if e != nil {
		writeError(c, e)
		return
	}
	response.Success(c, out)
}

func (h *RecommendationHandler) DeleteRecall(c *gin.Context)     { h.deleteDefinition(c, "recall") }
func (h *RecommendationHandler) DeleteStrategy(c *gin.Context)   { h.deleteDefinition(c, "strategy") }
func (h *RecommendationHandler) DeleteExperiment(c *gin.Context) { h.deleteDefinition(c, "experiment") }
func (h *RecommendationHandler) deleteDefinition(c *gin.Context, kind string) {
	if err := h.service.Delete(c.Request.Context(), adminID(c), kind, c.Param("appid"), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

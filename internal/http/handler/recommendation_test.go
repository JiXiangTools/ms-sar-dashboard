package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/gin-gonic/gin"
)

func TestPublishRejectsMissingOrNullExperimentID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, body string }{
		{"missing", `{}`},
		{"null body", `null`},
		{"null field", `{"experiment_id":null}`},
		{"wrong type", `{"experiment_id":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Invalid input must be rejected before touching the publication service.
			r := gin.New()
			r.Use(gin.RecoveryWithWriter(io.Discard))
			r.POST("/:appid/publication", NewRecommendationHandler(nil).Publish)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/100001/publication", strings.NewReader(tc.body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPublicationRequestAcceptsExplicitExperimentIDAndStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, body, id string }{
		{"publish", `{"experiment_id":"trial"}`, "trial"},
		{"stop", `{"experiment_id":""}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			v, ok := recommendationJSON[recommendationPublicationRequest](c)
			if !ok || v.ExperimentID == nil || *v.ExperimentID != tc.id {
				t.Fatalf("explicit publication request rejected: %+v", v)
			}
		})
	}
}

func TestRecommendationJSONRejectsRemovedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"experiment_id":"trial","experiment_version":1}`,
		`{"experiment_id":"trial","controls":[]}`,
		`{"experiment_id":"trial","salt":"test"}`,
		`{"experiment_id":"trial","bucket_algorithm":"sha1"}`,
		`{"experiment_id":"trial","assignments":[{"strategy_id":"feed","strategy_version":1}]}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		if _, ok := recommendationJSON[domain.RecommendationExperiment](c); ok || w.Code != 400 {
			t.Fatalf("obsolete field accepted: %s", body)
		}
	}
	for _, body := range []string{`{"strategy_id":"feed","version":1}`, `{"recall":[{"source_id":"icf"}]}`, `{"fallback_source_id":"hot_day"}`, `{"newest":{}}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		if _, ok := recommendationJSON[domain.RecommendationStrategy](c); ok || w.Code != 400 {
			t.Fatalf("obsolete strategy accepted: %s", body)
		}
	}
}

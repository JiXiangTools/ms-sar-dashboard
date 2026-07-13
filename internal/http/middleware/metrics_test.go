package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
)

func TestRequestMetricsUsesRouteTemplateNotRawRequestData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := appmetrics.New()
	engine := gin.New()
	engine.Use(RequestMetrics(registry))
	engine.GET("/api/v1/admin/app/:app_id", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, target := range []string{"/api/v1/admin/app/secret-one?token=secret-query", "/api/v1/admin/app/secret-two", "/unknown/secret-three"} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	}

	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, expected := range []string{
		`route="/api/v1/admin/app/:app_id"`,
		`route="unmatched"`,
		`status="204"`,
		`status="404"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics must contain %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"secret-one", "secret-two", "secret-three", "secret-query", "token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics leaked %q:\n%s", forbidden, body)
		}
	}
}

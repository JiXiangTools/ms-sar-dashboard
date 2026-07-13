package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
)

// RequestMetrics records only Gin's fixed route template. Unmatched requests
// use the constant "unmatched" and never expose the raw URL path or query.
func RequestMetrics(registry *appmetrics.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		registry.ObserveHTTPRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(startedAt))
	}
}

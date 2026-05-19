package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
)

func AccessLog(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Set(ContextKeyRequestStartedAt, startedAt)
		c.Next()

		errorText := ""
		if len(c.Errors) > 0 {
			errorText = c.Errors.String()
		}
		level := logx.Info
		if c.Writer.Status() >= 500 || errorText != "" {
			level = logx.Error
		} else if c.Writer.Status() >= 400 {
			level = logx.Warn
		}
		level(logger, c.Request.Context(), startedAt, "http.access", "request completed",
			logx.String("method", c.Request.Method),
			logx.String("path", c.Request.URL.Path),
			logx.String("query", c.Request.URL.RawQuery),
			logx.Int("status", c.Writer.Status()),
			logx.String("client_ip", c.ClientIP()),
			logx.String("errors", errorText),
		)
	}
}

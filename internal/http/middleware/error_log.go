package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

func ErrorLog(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if response.IsErrorLogged(c) {
			return
		}
		errorResponse, ok := response.ErrorResponseFromGin(c)
		if !ok {
			return
		}

		startedAt, _ := c.Get(ContextKeyRequestStartedAt)
		requestStartedAt, _ := startedAt.(time.Time)
		level := logx.Warn
		if errorResponse.Status >= 500 {
			level = logx.Error
		}
		fields := []logx.Field{
			logx.String("method", c.Request.Method),
			logx.String("path", c.Request.URL.Path),
			logx.String("query", c.Request.URL.RawQuery),
			logx.Int("status", errorResponse.Status),
			logx.Int("business_status", errorResponse.BusinessStatus),
			logx.String("message", errorResponse.Message),
			logx.String("client_ip", c.ClientIP()),
		}
		fields = appendErrorResponseLogFields(c, fields)
		level(logger, c.Request.Context(), requestStartedAt, "http.response_error", "error response",
			fields...,
		)
		response.MarkErrorLogged(c)
	}
}

package middleware

import (
	"log"
	"net/url"
	"strings"
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
		fields := []logx.Field{
			logx.String("method", c.Request.Method),
			logx.String("path", c.Request.URL.Path),
			logx.String("query", redactQuery(c.Request.URL.RawQuery)),
			logx.Int("status", c.Writer.Status()),
			logx.String("client_ip_masked", maskIP(ClientIP(c))),
			logx.String("errors", errorText),
		}
		fields = appendErrorResponseLogFields(c, fields)
		level(logger, c.Request.Context(), startedAt, "http.access", "request completed", fields...)
	}
}

func redactQuery(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[redacted-invalid-query]"
	}
	for key := range values {
		if sensitiveQueryKey(key) {
			values[key] = []string{"[redacted]"}
		}
	}
	return values.Encode()
}

func sensitiveQueryKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "password") ||
		normalized == "authorization"
}

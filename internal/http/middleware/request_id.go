package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/kely-jian/ms-sar-dashboard/internal/platform/requestid"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		value := requestid.Resolve(c.GetHeader(requestid.HeaderName))
		c.Set(requestid.ContextKey, value)
		c.Header(requestid.HeaderName, value)
		c.Request = c.Request.WithContext(requestid.WithContext(c.Request.Context(), value))
		c.Next()
	}
}

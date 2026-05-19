package middleware

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kely-jian/ms-sar-dashboard/internal/platform/logx"
	"github.com/kely-jian/ms-sar-dashboard/internal/response"
)

func Recovery(logger *log.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		startedAt, _ := c.Get(ContextKeyRequestStartedAt)
		requestStartedAt, _ := startedAt.(time.Time)
		logx.Error(logger, c.Request.Context(), requestStartedAt, "http.panic", "panic recovered",
			logx.String("method", c.Request.Method),
			logx.String("path", c.Request.URL.Path),
			logx.String("panic", recoveredMessage(recovered)),
			logx.String("stack", string(debug.Stack())),
		)
		response.MarkErrorLogged(c)
		response.Error(c, http.StatusInternalServerError, "internal server error", nil)
	})
}

func recoveredMessage(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

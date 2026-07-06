package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

const (
	HeaderAppID  = "x-dwzauth-appid"
	HeaderSecret = "x-dwzauth-secret"
)

func ClientAuth(appService *service.AppService) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := appService.Authorize(c.Request.Context(), c.GetHeader(HeaderAppID), c.GetHeader(HeaderSecret))
		if err != nil {
			WriteAppAuthError(c, c.GetHeader(HeaderAppID), service.AppAuthFailureReason(err))
			c.Abort()
			return
		}
		c.Set(ContextKeyAppID, app.ID)
		c.Next()
	}
}

func WriteAppAuthError(c *gin.Context, appID any, reason string) {
	response.Error(c, http.StatusUnauthorized, "invalid app authorization", nil)
	response.SetErrorLogDetails(c, map[string]any{
		"auth_reason": reason,
		"auth_appid":  appID,
	})
}

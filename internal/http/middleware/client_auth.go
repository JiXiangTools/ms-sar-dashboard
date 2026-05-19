package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kely-jian/ms-sar-dashboard/internal/response"
	"github.com/kely-jian/ms-sar-dashboard/internal/service"
)

const (
	HeaderAppID  = "x-dwzauth-appid"
	HeaderSecret = "x-dwzauth-secret"
)

func ClientAuth(appService *service.AppService) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := appService.Authorize(c.Request.Context(), c.GetHeader(HeaderAppID), c.GetHeader(HeaderSecret))
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "invalid app authorization", nil)
			c.Abort()
			return
		}
		c.Set(ContextKeyAppID, app.ID)
		c.Next()
	}
}

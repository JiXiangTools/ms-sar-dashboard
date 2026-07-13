package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

const (
	HeaderAppID  = "x-dwzauth-appid"
	HeaderSecret = "x-dwzauth-secret"
)

func ClientAuth(appService *service.AppService, observers ...*appmetrics.Registry) gin.HandlerFunc {
	var metricRegistry *appmetrics.Registry
	if len(observers) > 0 {
		metricRegistry = observers[0]
	}
	return func(c *gin.Context) {
		if appService == nil {
			metricRegistry.ObserveClientAuth(appmetrics.OutcomeError)
			response.Error(c, http.StatusInternalServerError, "internal server error", nil)
			c.Abort()
			return
		}
		app, err := appService.Authorize(c.Request.Context(), c.GetHeader(HeaderAppID), c.GetHeader(HeaderSecret))
		if err != nil {
			metricRegistry.ObserveClientAuth(authOutcome(err))
			WriteAppAuthFailure(c, c.GetHeader(HeaderAppID), err)
			c.Abort()
			return
		}
		metricRegistry.ObserveClientAuth(appmetrics.OutcomeSuccess)
		c.Set(ContextKeyAppID, app.ID)
		c.Next()
	}
}

func WriteAppAuthFailure(c *gin.Context, appID any, err error) {
	status := http.StatusUnauthorized
	message := "invalid app authorization"
	if appErr, ok := apperror.As(err); ok && appErr.HTTPStatus == http.StatusServiceUnavailable {
		status = http.StatusServiceUnavailable
		message = "app authorization service unavailable"
	}
	response.Error(c, status, message, nil)
	response.SetErrorLogDetails(c, map[string]any{
		"auth_reason": service.AppAuthFailureReason(err),
		"auth_appid":  appID,
	})
}

func WriteAppAuthError(c *gin.Context, appID any, reason string) {
	response.Error(c, http.StatusUnauthorized, "invalid app authorization", nil)
	response.SetErrorLogDetails(c, map[string]any{
		"auth_reason": reason,
		"auth_appid":  appID,
	})
}

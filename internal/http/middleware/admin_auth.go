package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

func AdminAuth(authService *service.AdminAuthService, metricRegistry *appmetrics.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := extractBearerToken(c.GetHeader("Authorization"))
		if !ok {
			metricRegistry.ObserveAdminAuth(appmetrics.AdminAuthAccessToken, appmetrics.OutcomeDenied)
			response.Error(c, http.StatusUnauthorized, "invalid access token", nil)
			c.Abort()
			return
		}
		if authService == nil {
			metricRegistry.ObserveAdminAuth(appmetrics.AdminAuthAccessToken, appmetrics.OutcomeUnavailable)
			response.Error(c, http.StatusServiceUnavailable, "admin authentication service unavailable", nil)
			c.Abort()
			return
		}

		admin, err := authService.AuthenticateAccessToken(c.Request.Context(), token)
		if err != nil {
			metricRegistry.ObserveAdminAuth(appmetrics.AdminAuthAccessToken, authOutcome(err))
			if appErr, ok := apperror.As(err); ok && appErr.HTTPStatus == http.StatusServiceUnavailable {
				response.Error(c, http.StatusServiceUnavailable, "admin authentication service unavailable", nil)
			} else {
				response.Error(c, http.StatusUnauthorized, "invalid access token", nil)
			}
			c.Abort()
			return
		}
		metricRegistry.ObserveAdminAuth(appmetrics.AdminAuthAccessToken, appmetrics.OutcomeSuccess)

		c.Set(ContextKeyAdminID, admin.ID)
		c.Set(ContextKeyAdmin, admin)
		c.Set(ContextKeyAccessToken, token)
		c.Next()
	}
}

func authOutcome(err error) appmetrics.Outcome {
	if err == nil {
		return appmetrics.OutcomeSuccess
	}
	if appErr, ok := apperror.As(err); ok {
		return appmetrics.OutcomeFromStatus(appErr.HTTPStatus)
	}
	return appmetrics.OutcomeError
}

func extractBearerToken(header string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

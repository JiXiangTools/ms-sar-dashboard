package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

func AdminAuth(authService *service.AdminAuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := extractBearerToken(c.GetHeader("Authorization"))
		if !ok {
			response.Error(c, http.StatusUnauthorized, "invalid access token", nil)
			c.Abort()
			return
		}

		admin, err := authService.AuthenticateAccessToken(c.Request.Context(), token)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "invalid access token", nil)
			c.Abort()
			return
		}

		c.Set(ContextKeyAdminID, admin.ID)
		c.Set(ContextKeyAdmin, admin)
		c.Next()
	}
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

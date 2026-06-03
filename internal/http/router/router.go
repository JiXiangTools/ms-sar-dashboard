package router

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/health"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/handler"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/middleware"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/ui"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type Dependencies struct {
	HealthService *health.Service
	Services      *service.Container
}

func New(cfg config.Config, logger *log.Logger, deps Dependencies) *gin.Engine {
	gin.SetMode(resolveGinMode(cfg.App.Env))

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(handler.ContextKeyLogger, logger)
		c.Next()
	})
	engine.Use(middleware.RequestID())
	engine.Use(middleware.AccessLog(logger))
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.ErrorLog(logger))

	healthHandler := handler.NewHealthHandler(deps.HealthService)
	engine.GET("/health", healthHandler.Get)
	ui.Register(engine)

	engine.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, "route not found", nil)
	})
	engine.NoMethod(func(c *gin.Context) {
		response.Error(c, http.StatusMethodNotAllowed, "method not allowed", nil)
	})

	api := engine.Group("/api/v1/admin")
	if deps.Services != nil {
		adminHandler := handler.NewAdminHandler(deps.Services.Auth, deps.Services.Apps, deps.Services.Logs, deps.Services.Debug)
		engine.GET("/api/v1/auth/app", adminHandler.ListAuthorizedApps)
		engine.POST("/api/v1/auth/app", adminHandler.AppAuthorize)
		api.POST("/auth/login", adminHandler.Login)

		authorized := api.Group("")
		authorized.Use(middleware.AdminAuth(deps.Services.Auth))
		{
			authorized.POST("/auth/logout", adminHandler.Logout)
			authorized.GET("/app", adminHandler.ListApps)
			authorized.POST("/app", adminHandler.CreateApp)
			authorized.PUT("/app/:app_id", adminHandler.UpdateApp)
			authorized.DELETE("/app/:app_id", adminHandler.DeleteApp)
			authorized.GET("/log", adminHandler.ListLogs)
			authorized.GET("/debug/es/index/:appid", adminHandler.ESIndexInfo)
			authorized.GET("/debug/es/doc/:appid/:item_id", adminHandler.ESDocument)
			authorized.POST("/debug/es/search/:appid", adminHandler.ESSearch)
			authorized.POST("/debug/es/raw", adminHandler.ESRaw)
			authorized.POST("/debug/rec", adminHandler.RecDebug)
		}
	}

	return engine
}

func resolveGinMode(environment string) string {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return gin.ReleaseMode
	case "test":
		return gin.TestMode
	default:
		return gin.DebugMode
	}
}

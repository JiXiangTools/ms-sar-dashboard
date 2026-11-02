package router

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/health"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/handler"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/middleware"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/ui"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

type Dependencies struct {
	HealthService  *health.Service
	Services       *service.Container
	Metrics        *appmetrics.Registry
	RateLimitStore middleware.RateLimitStore
}

func New(cfg config.Config, logger *log.Logger, deps Dependencies) *gin.Engine {
	gin.SetMode(resolveGinMode(cfg.App.Env))
	metricRegistry := deps.Metrics
	if metricRegistry == nil {
		metricRegistry = appmetrics.New()
	}

	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)
	clientIPResolver, err := middleware.NewClientIPResolver(cfg.Security.TrustedProxyCIDRs)
	if err != nil {
		panic(err)
	}
	maxRequestBodyBytes := cfg.Security.MaxRequestBodyBytes
	if maxRequestBodyBytes <= 0 {
		maxRequestBodyBytes = config.DefaultMaxRequestBodyBytes
	}
	engine.Use(func(c *gin.Context) {
		c.Set(handler.ContextKeyLogger, logger)
		c.Next()
	})
	engine.Use(middleware.RequestID())
	engine.Use(middleware.ResolveClientIP(clientIPResolver))
	engine.Use(middleware.RequestMetrics(metricRegistry))
	engine.Use(middleware.AccessLog(logger))
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.ErrorLog(logger))
	engine.Use(middleware.RequestBodyLimit(maxRequestBodyBytes))

	healthHandler := handler.NewHealthHandler(deps.HealthService)
	engine.GET("/health", healthHandler.Get)
	engine.GET("/metrics", gin.WrapH(metricRegistry.Handler()))
	ui.Register(engine)

	engine.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, "route not found", nil)
	})
	engine.NoMethod(func(c *gin.Context) {
		response.Error(c, http.StatusMethodNotAllowed, "method not allowed", nil)
	})

	api := engine.Group("/api/v1/admin")
	if deps.Services != nil {
		statisticsHandler := handler.NewStatisticsHandler(deps.Services.Statistics, cfg.Statistics.IngestToken)
		engine.PUT("/api/v1/internal/statistics/apps/:appid/days/:date", middleware.SecurityRateLimitWithStore(deps.RateLimitStore, 600, time.Minute, "statistics_ingest", middleware.ClientIPRateLimitKey), statisticsHandler.Put)
		adminHandler := handler.NewAdminHandler(deps.Services.Auth, deps.Services.SSO, deps.Services.Apps, deps.Services.Logs, deps.Services.Debug, metricRegistry)
		engine.GET("/api/v1/auth/app", adminHandler.ListAuthorizedApps)
		engine.POST("/api/v1/auth/app", middleware.SecurityRateLimitWithStore(deps.RateLimitStore, 120, time.Minute, "app_authorize", middleware.ClientIPRateLimitKey), adminHandler.AppAuthorize)
		api.POST("/auth/login",
			middleware.SecurityRateLimitWithStore(deps.RateLimitStore, 100, time.Minute, "admin_login_source", middleware.ClientIPRateLimitKey),
			middleware.SecurityRateLimitWithStore(deps.RateLimitStore, 10, time.Minute, "admin_login_account", middleware.LoginAccountRateLimitKey),
			adminHandler.Login,
		)
		api.GET("/auth/sso", adminHandler.SSOStatus)
		api.POST("/auth/sso/login", middleware.SecurityRateLimitWithStore(deps.RateLimitStore, 10, time.Minute, "admin_sso_login", middleware.ClientIPRateLimitKey), adminHandler.SSOLogin)

		authorized := api.Group("")
		authorized.Use(middleware.AdminAuth(deps.Services.Auth, metricRegistry))
		{
			authorized.POST("/auth/logout", adminHandler.Logout)
			authorized.GET("/app", adminHandler.ListApps)
			authorized.POST("/app", adminHandler.CreateApp)
			authorized.POST("/app/reconcile", adminHandler.ReconcileAppAuthorization)
			authorized.PUT("/app/:app_id", adminHandler.UpdateApp)
			authorized.DELETE("/app/:app_id", adminHandler.DeleteApp)
			authorized.GET("/log", adminHandler.ListLogs)
			authorized.GET("/statistics", statisticsHandler.Query)
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

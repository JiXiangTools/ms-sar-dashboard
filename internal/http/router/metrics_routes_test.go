package router

import (
	"io"
	"log"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	appmetrics "github.com/JiXiangTools/ms-sar-dashboard/internal/metrics"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/service"
)

func TestEveryRegisteredRouteHasBoundedMetricsTemplate(t *testing.T) {
	engine := New(config.Config{App: config.AppConfig{Env: "test"}}, log.New(io.Discard, "", 0), Dependencies{
		Services: &service.Container{},
		Metrics:  appmetrics.New(),
	})
	for _, route := range engine.Routes() {
		if !appmetrics.IsKnownRouteTemplate(route.Path) {
			t.Errorf("route %s %s is missing from the metrics template allowlist", route.Method, route.Path)
		}
	}
}

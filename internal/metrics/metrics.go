package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "ms_sar_dashboard"

var knownRouteTemplates = map[string]struct{}{
	"/health":                                    {},
	"/metrics":                                   {},
	"/sar-admin":                                 {},
	"/sar-admin/":                                {},
	"/sar-admin/assets/*filepath":                {},
	"/api/v1/auth/app":                           {},
	"/api/v1/admin/auth/login":                   {},
	"/api/v1/admin/auth/sso":                     {},
	"/api/v1/admin/auth/sso/login":               {},
	"/api/v1/admin/auth/logout":                  {},
	"/api/v1/admin/app":                          {},
	"/api/v1/admin/app/reconcile":                {},
	"/api/v1/admin/app/:app_id":                  {},
	"/api/v1/admin/log":                          {},
	"/api/v1/admin/debug/es/index/:appid":        {},
	"/api/v1/admin/debug/es/doc/:appid/:item_id": {},
	"/api/v1/admin/debug/es/search/:appid":       {},
	"/api/v1/admin/debug/es/raw":                 {},
	"/api/v1/admin/debug/rec":                    {},
}

type Outcome string

const (
	OutcomeSuccess        Outcome = "success"
	OutcomeDenied         Outcome = "denied"
	OutcomeInvalidRequest Outcome = "invalid_request"
	OutcomeUnavailable    Outcome = "unavailable"
	OutcomeError          Outcome = "error"
)

type AdminAuthOperation string

const (
	AdminAuthLogin       AdminAuthOperation = "login"
	AdminAuthSSOLogin    AdminAuthOperation = "sso_login"
	AdminAuthAccessToken AdminAuthOperation = "access_token"
)

type ProjectionOperation string

const (
	ProjectionStartup        ProjectionOperation = "startup"
	ProjectionAdminReconcile ProjectionOperation = "admin_reconcile"
)

type StartupOperation string

const StartupMigrationVerify StartupOperation = "migration_verify"

// Registry owns a private Prometheus registry so tests and multiple app
// instances in one process cannot collide through the global default registry.
type Registry struct {
	prometheus *prometheus.Registry

	httpRequests       *prometheus.CounterVec
	httpDuration       *prometheus.HistogramVec
	clientAuth         *prometheus.CounterVec
	adminAuth          *prometheus.CounterVec
	projection         *prometheus.CounterVec
	startupCheckResult *prometheus.CounterVec
}

func New() *Registry {
	registry := prometheus.NewRegistry()
	metrics := &Registry{
		prometheus: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total completed HTTP requests by fixed route template, method, and response status.",
		}, []string{"route", "method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds by fixed route template, method, and response status.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"route", "method", "status"}),
		clientAuth: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "client_auth_total",
			Help:      "Total client application authorization outcomes.",
		}, []string{"outcome"}),
		adminAuth: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "admin_auth_total",
			Help:      "Total administrator authentication outcomes by fixed operation type.",
		}, []string{"operation", "outcome"}),
		projection: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "authorization_projection_reconcile_total",
			Help:      "Total authorization projection reconciliation outcomes by fixed operation type.",
		}, []string{"operation", "outcome"}),
		startupCheckResult: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "startup_checks_total",
			Help:      "Total startup check outcomes by fixed operation type.",
		}, []string{"operation", "outcome"}),
	}
	registry.MustRegister(
		metrics.httpRequests,
		metrics.httpDuration,
		metrics.clientAuth,
		metrics.adminAuth,
		metrics.projection,
		metrics.startupCheckResult,
	)
	return metrics
}

func (r *Registry) Handler() http.Handler {
	if r == nil || r.prometheus == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(r.prometheus, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (r *Registry) ObserveHTTPRequest(method string, route string, status int, elapsed time.Duration) {
	if r == nil {
		return
	}
	labels := []string{normalizeRoute(route), normalizeMethod(method), normalizeStatus(status)}
	r.httpRequests.WithLabelValues(labels...).Inc()
	r.httpDuration.WithLabelValues(labels...).Observe(elapsed.Seconds())
}

func (r *Registry) ObserveClientAuth(outcome Outcome) {
	if r == nil {
		return
	}
	r.clientAuth.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

func (r *Registry) ObserveAdminAuth(operation AdminAuthOperation, outcome Outcome) {
	if r == nil {
		return
	}
	r.adminAuth.WithLabelValues(normalizeAdminAuthOperation(operation), normalizeOutcome(outcome)).Inc()
}

func (r *Registry) ObserveProjectionReconcile(operation ProjectionOperation, outcome Outcome) {
	if r == nil {
		return
	}
	r.projection.WithLabelValues(normalizeProjectionOperation(operation), normalizeOutcome(outcome)).Inc()
}

func (r *Registry) ObserveStartupCheck(operation StartupOperation, outcome Outcome) {
	if r == nil {
		return
	}
	r.startupCheckResult.WithLabelValues(normalizeStartupOperation(operation), normalizeOutcome(outcome)).Inc()
}

func OutcomeFromStatus(status int) Outcome {
	switch {
	case status >= 200 && status < 400:
		return OutcomeSuccess
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return OutcomeInvalidRequest
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return OutcomeDenied
	case status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		return OutcomeUnavailable
	default:
		return OutcomeError
	}
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if IsKnownRouteTemplate(route) {
		return route
	}
	return "unmatched"
}

func IsKnownRouteTemplate(route string) bool {
	_, ok := knownRouteTemplates[strings.TrimSpace(route)]
	return ok
}

func normalizeMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet:
		return http.MethodGet
	case http.MethodHead:
		return http.MethodHead
	case http.MethodPost:
		return http.MethodPost
	case http.MethodPut:
		return http.MethodPut
	case http.MethodPatch:
		return http.MethodPatch
	case http.MethodDelete:
		return http.MethodDelete
	case http.MethodOptions:
		return http.MethodOptions
	default:
		return "OTHER"
	}
}

func normalizeStatus(status int) string {
	if status < 100 || status > 599 {
		return "0"
	}
	return strconv.Itoa(status)
}

func normalizeOutcome(outcome Outcome) string {
	switch outcome {
	case OutcomeSuccess, OutcomeDenied, OutcomeInvalidRequest, OutcomeUnavailable, OutcomeError:
		return string(outcome)
	default:
		return string(OutcomeError)
	}
}

func normalizeAdminAuthOperation(operation AdminAuthOperation) string {
	switch operation {
	case AdminAuthLogin, AdminAuthSSOLogin, AdminAuthAccessToken:
		return string(operation)
	default:
		return "unknown"
	}
}

func normalizeProjectionOperation(operation ProjectionOperation) string {
	switch operation {
	case ProjectionStartup, ProjectionAdminReconcile:
		return string(operation)
	default:
		return "unknown"
	}
}

func normalizeStartupOperation(operation StartupOperation) string {
	if operation == StartupMigrationVerify {
		return string(operation)
	}
	return "unknown"
}

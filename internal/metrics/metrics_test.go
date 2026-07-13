package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryUsesBoundedLabels(t *testing.T) {
	registry := New()
	registry.ObserveHTTPRequest("BREW", "/caller-controlled/private-id", 999, 10*time.Millisecond)
	registry.ObserveClientAuth("caller-controlled")
	registry.ObserveAdminAuth("caller-controlled", "caller-controlled")
	registry.ObserveProjectionReconcile("caller-controlled", "caller-controlled")
	registry.ObserveStartupCheck("caller-controlled", "caller-controlled")
	registry.ObserveProjectionReconcile(ProjectionStartup, OutcomeSuccess)
	registry.ObserveProjectionReconcile(ProjectionAdminReconcile, OutcomeUnavailable)
	registry.ObserveStartupCheck(StartupMigrationVerify, OutcomeSuccess)

	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if recorder.Code != 200 {
		t.Fatalf("metrics status = %d", recorder.Code)
	}
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	text := string(body)
	for _, expected := range []string{
		`method="OTHER"`,
		`route="unmatched"`,
		`status="0"`,
		`operation="unknown"`,
		`outcome="error"`,
		`ms_sar_dashboard_authorization_projection_reconcile_total{operation="startup",outcome="success"} 1`,
		`ms_sar_dashboard_authorization_projection_reconcile_total{operation="admin_reconcile",outcome="unavailable"} 1`,
		`ms_sar_dashboard_startup_checks_total{operation="migration_verify",outcome="success"} 1`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics must contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "caller-controlled") {
		t.Fatalf("metrics leaked caller-controlled label:\n%s", text)
	}
}

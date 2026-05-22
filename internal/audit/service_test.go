package audit

import (
	"fmt"
	"testing"
)

func TestSanitizeRedactsSensitiveFieldsAndKeepsScalarValues(t *testing.T) {
	got := sanitize(map[string]any{
		"app_id": int64(100001),
		"name":   "verify-app",
		"secret": "plain-secret",
	})

	content, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", got)
	}
	if content["secret"] != "[redacted]" {
		t.Fatalf("expected secret to be redacted, got %#v", content["secret"])
	}
	if fmt.Sprint(content["app_id"]) != "100001" {
		t.Fatalf("expected scalar app_id to be preserved, got %#v", content["app_id"])
	}
}

package test

import (
	"strings"
	"testing"
)

func TestAdminAPIDocumentContainsCoreContract(t *testing.T) {
	doc := readRepoFile(t, "docs", "api-admin.md")

	required := []string{
		"/api/v1/admin/auth/login",
		"/api/v1/admin/auth/sso",
		"/api/v1/admin/auth/sso/login",
		"/api/v1/admin/app",
		"/api/v1/auth/app",
		"/api/v1/admin/debug/es/index/{appid}",
		"/api/v1/admin/debug/es/raw",
		"/api/v1/admin/debug/rec",
		"x-dwzauth-appid",
		"x-dwzauth-secret",
		"dashboard 授权",
	}

	for _, value := range required {
		if !strings.Contains(doc, value) {
			t.Fatalf("api-admin.md must contain %q", value)
		}
	}
}

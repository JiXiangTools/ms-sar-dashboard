package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
)

func TestAppResponseOnlyIncludesSecretForCreateOrRotation(t *testing.T) {
	app := domain.App{ID: 1, Name: "alpha", Secret: "top-secret"}

	listed, err := json.Marshal(newAppResponse(app, false))
	if err != nil {
		t.Fatalf("marshal list response: %v", err)
	}
	if strings.Contains(string(listed), "top-secret") || strings.Contains(string(listed), `"secret":`) {
		t.Fatalf("list response leaked secret: %s", listed)
	}
	if !strings.Contains(string(listed), `"secret_configured":true`) {
		t.Fatalf("list response must expose secret state: %s", listed)
	}

	revealed, err := json.Marshal(newAppResponse(app, true))
	if err != nil {
		t.Fatalf("marshal reveal response: %v", err)
	}
	if !strings.Contains(string(revealed), `"secret":"top-secret"`) {
		t.Fatalf("create/rotation response must reveal secret once: %s", revealed)
	}
}

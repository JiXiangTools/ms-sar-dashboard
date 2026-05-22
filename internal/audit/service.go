package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Record(ctx context.Context, adminID int64, cate string, typ string, content any) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.CreateAdminLog(ctx, domain.AdminLog{
		AdminID:    adminID,
		Cate:       strings.TrimSpace(cate),
		Type:       strings.TrimSpace(typ),
		Content:    sanitize(content),
		CreateTime: time.Now().UTC(),
	})
}

func sanitize(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitize(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = sanitize(nested)
		}
		return out
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return string(typed)
		}
		return sanitize(decoded)
	case string:
		return typed
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return typed
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return typed
		}
		switch decoded.(type) {
		case map[string]any, []any:
			return sanitize(decoded)
		default:
			return decoded
		}
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "password", "secret", "token", "access_token", "refresh_token", "jwt_secret", "authorization", "redis_password", "es_password":
		return true
	}
	return strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token")
}

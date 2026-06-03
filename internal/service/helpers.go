package service

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

func generateSecret() string {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", len(buf))
	}
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

func appAuthKey(appID int64) string {
	return fmt.Sprintf("app_auth_%d", appID)
}

func appAuthAllAppIDsKey() string {
	return "app_auth_allappids"
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func clampSize(size int, max int) int {
	if size <= 0 {
		return 20
	}
	if size > max {
		return max
	}
	return size
}

func isTruthy(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "true" || normalized == "1" || normalized == "yes"
}

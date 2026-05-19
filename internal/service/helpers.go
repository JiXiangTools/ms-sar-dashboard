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

func deriveDebugKey(debugType string, appID string, itemID string, userID string, period string, explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed
	}
	switch strings.ToLower(strings.TrimSpace(debugType)) {
	case "hot":
		p := strings.TrimSpace(period)
		if p == "" {
			p = "day"
		}
		return fmt.Sprintf("hot_%s_%s", appID, p)
	case "related":
		if itemID != "" {
			return fmt.Sprintf("icf_%s_%s", appID, itemID)
		}
	case "personalized":
		if userID != "" {
			return fmt.Sprintf("ck_%s_%s", appID, userID)
		}
	case "key":
	}
	return ""
}

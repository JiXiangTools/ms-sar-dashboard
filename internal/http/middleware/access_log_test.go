package middleware

import (
	"strings"
	"testing"
	"time"
)

func TestRedactQueryRemovesSensitiveValues(t *testing.T) {
	redacted := redactQuery("page=1&token=cas-token&appsecret=sso-secret&password=pwd&name=alpha")
	for _, secret := range []string{"cas-token", "sso-secret", "pwd"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted query leaked %q: %s", secret, redacted)
		}
	}
	for _, expected := range []string{"page=1", "name=alpha", "token=%5Bredacted%5D", "appsecret=%5Bredacted%5D"} {
		if !strings.Contains(redacted, expected) {
			t.Fatalf("expected %q in redacted query: %s", expected, redacted)
		}
	}
}

func TestSecurityRateLimiterRejectsRequestsOverLimit(t *testing.T) {
	limiter := &fixedWindowLimiter{entries: make(map[string]rateLimitEntry), limit: 2, window: time.Minute}
	if allowed, _ := limiter.allow("login:127.0.0.1"); !allowed {
		t.Fatal("expected first request to be allowed")
	}
	if allowed, _ := limiter.allow("login:127.0.0.1"); !allowed {
		t.Fatal("expected second request to be allowed")
	}
	if allowed, retryAfter := limiter.allow("login:127.0.0.1"); allowed || retryAfter <= 0 {
		t.Fatalf("expected third request to be rate limited, retry_after=%s", retryAfter)
	}
}

func TestMaskIP(t *testing.T) {
	if got := maskIP("203.0.113.42"); got != "203.0.113.0/24" {
		t.Fatalf("masked ipv4 = %q", got)
	}
	if got := maskIP("2001:db8:abcd:12::99"); got != "2001:db8:abcd:12::/64" {
		t.Fatalf("masked ipv6 = %q", got)
	}
	if got := maskIP("not-an-ip"); got != "" {
		t.Fatalf("invalid ip mask = %q", got)
	}
}

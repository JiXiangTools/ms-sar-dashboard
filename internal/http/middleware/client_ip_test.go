package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPResolverIgnoresForwardingHeadersFromUntrustedPeer(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.42:12345"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := resolver.Resolve(request); got != "203.0.113.42" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestClientIPResolverWalksTrustedProxyChain(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:12345"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.4")

	if got := resolver.Resolve(request); got != "198.51.100.7" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestClientIPResolverHandlesMultipleForwardedForHeaderLines(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:12345"
	request.Header.Add("X-Forwarded-For", "198.51.100.7")
	request.Header.Add("X-Forwarded-For", "10.0.0.4")

	if got := resolver.Resolve(request); got != "198.51.100.7" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestClientIPResolverCanonicalizesMappedIPv4(t *testing.T) {
	resolver, err := NewClientIPResolver(nil)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "[::ffff:192.0.2.10]:12345"

	if got := resolver.Resolve(request); got != "192.0.2.10" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestClientIPResolverFallsBackToPeerForMalformedForwardingChain(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:12345"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, not-an-ip")

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("client ip = %q", got)
	}
}

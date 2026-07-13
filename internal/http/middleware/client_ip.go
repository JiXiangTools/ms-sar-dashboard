package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxForwardedForEntries = 32

type ClientIPResolver struct {
	trustedProxies []netip.Prefix
}

func NewClientIPResolver(trustedProxyCIDRs []string) (*ClientIPResolver, error) {
	resolver := &ClientIPResolver{trustedProxies: make([]netip.Prefix, 0, len(trustedProxyCIDRs))}
	for _, rawPrefix := range trustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rawPrefix))
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", rawPrefix, err)
		}
		resolver.trustedProxies = append(resolver.trustedProxies, prefix.Masked())
	}
	return resolver, nil
}

func (r *ClientIPResolver) Resolve(request *http.Request) string {
	peer, ok := parseRemoteIP(request.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if r == nil || !r.isTrusted(peer) {
		return peer.String()
	}

	forwardedValues := request.Header.Values("X-Forwarded-For")
	if len(forwardedValues) == 0 {
		return peer.String()
	}
	forwardedValue := strings.Join(forwardedValues, ",")
	if strings.Count(forwardedValue, ",") >= maxForwardedForEntries {
		return peer.String()
	}
	forwarded := strings.Split(forwardedValue, ",")
	if len(forwarded) > maxForwardedForEntries {
		return peer.String()
	}

	chain := make([]netip.Addr, 0, len(forwarded))
	for _, rawAddress := range forwarded {
		address, err := netip.ParseAddr(strings.TrimSpace(rawAddress))
		if err != nil {
			return peer.String()
		}
		chain = append(chain, canonicalIP(address))
	}

	current := peer
	for index := len(chain) - 1; index >= 0 && r.isTrusted(current); index-- {
		current = chain[index]
	}
	return current.String()
}

func (r *ClientIPResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range r.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func ResolveClientIP(resolver *ClientIPResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextKeyClientIP, resolver.Resolve(c.Request))
		c.Next()
	}
}

func ClientIP(c *gin.Context) string {
	value, exists := c.Get(ContextKeyClientIP)
	if exists {
		if clientIP, ok := value.(string); ok && clientIP != "" {
			return clientIP
		}
	}
	address, ok := parseRemoteIP(c.Request.RemoteAddr)
	if !ok {
		return "unknown"
	}
	return address.String()
}

func parseRemoteIP(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddress))
	if err != nil {
		host = strings.TrimSpace(remoteAddress)
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return canonicalIP(address), true
}

func canonicalIP(address netip.Addr) netip.Addr {
	return address.WithZone("").Unmap()
}

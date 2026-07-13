package middleware

import (
	"fmt"
	"net"
	"strings"
)

func maskIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", ipv4[0], ipv4[1], ipv4[2])
	}
	ipv6 := ip.To16()
	if ipv6 == nil {
		return ""
	}
	for index := 8; index < len(ipv6); index++ {
		ipv6[index] = 0
	}
	return ipv6.String() + "/64"
}

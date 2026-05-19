package requestid

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	HeaderName = "x-request-id"
	ContextKey = "request_id"
	length     = 16
)

type contextKey struct{}

var maxValue = big.NewInt(10_000_000_000_000_000)

func Resolve(value string) string {
	if strings.TrimSpace(value) == "" {
		return Generate()
	}
	return Normalize(value)
}

func Normalize(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= length {
		return trimmed[len(trimmed)-length:]
	}
	return fmt.Sprintf("%0*s", length, trimmed)
}

func Generate() string {
	number, err := rand.Int(rand.Reader, maxValue)
	if err != nil {
		return Normalize(strconv.FormatInt(time.Now().UnixNano(), 10))
	}
	return fmt.Sprintf("%016d", number.Int64())
}

func WithContext(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, Normalize(requestID))
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(contextKey{}).(string); ok {
		return value
	}
	return ""
}

func DetachedContext(ctx context.Context) context.Context {
	if requestID := FromContext(ctx); requestID != "" {
		return WithContext(context.Background(), requestID)
	}
	return context.Background()
}

func FromGin(c *gin.Context) string {
	if value, exists := c.Get(ContextKey); exists {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}

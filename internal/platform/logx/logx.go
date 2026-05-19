package logx

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/requestid"
)

const backgroundRequestID = "background"

type Field struct {
	Key   string
	Value any
}

func String(key string, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func Err(err error) Field {
	if err == nil {
		return Field{}
	}
	return String("error", err.Error())
}

func Info(logger *log.Logger, ctx context.Context, startedAt time.Time, event string, msg string, fields ...Field) {
	write(logger, ctx, startedAt, "info", event, msg, fields...)
}

func Warn(logger *log.Logger, ctx context.Context, startedAt time.Time, event string, msg string, fields ...Field) {
	write(logger, ctx, startedAt, "warn", event, msg, fields...)
}

func Error(logger *log.Logger, ctx context.Context, startedAt time.Time, event string, msg string, fields ...Field) {
	write(logger, ctx, startedAt, "error", event, msg, fields...)
}

func write(logger *log.Logger, ctx context.Context, startedAt time.Time, level string, event string, msg string, fields ...Field) {
	if logger == nil {
		return
	}

	parts := []string{
		formatPair("level", level),
		formatPair("event", event),
		formatPair("request_id", requestID(ctx)),
		formatPair("cost_ms", costMS(startedAt)),
		formatPair("msg", msg),
	}
	for _, field := range fields {
		if field.Key == "" {
			continue
		}
		parts = append(parts, formatPair(field.Key, field.Value))
	}
	logger.Print(strings.Join(parts, " "))
}

func requestID(ctx context.Context) string {
	if value := requestid.FromContext(ctx); value != "" {
		return value
	}
	return backgroundRequestID
}

func costMS(startedAt time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(startedAt).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func formatPair(key string, value any) string {
	return safeKey(key) + "=" + formatValue(value)
}

func safeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "field"
	}
	var builder strings.Builder
	for _, r := range key {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return `""`
	case string:
		return quoteIfNeeded(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case time.Duration:
		return strconv.FormatInt(typed.Milliseconds(), 10)
	case time.Time:
		return strconv.Quote(typed.UTC().Format(time.RFC3339Nano))
	default:
		return strconv.Quote(fmt.Sprint(typed))
	}
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	for _, r := range value {
		if unicode.IsSpace(r) || r == '"' || r == '=' {
			return strconv.Quote(value)
		}
	}
	return value
}

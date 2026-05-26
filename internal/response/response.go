package response

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/requestid"
)

type Envelope struct {
	Status    int    `json:"status"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

const (
	timeDisplayLayout       = "2006-01-02 15:04:05"
	ContextKeyErrorResponse = "response_error"
	ContextKeyErrorLogged   = "response_error_logged"
)

type ErrorResponseLog struct {
	Status         int
	BusinessStatus int
	Message        string
}

func Success(c *gin.Context, data any) {
	JSON(c, http.StatusOK, "success", data)
}

func SuccessRaw(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Status:    http.StatusOK,
		Message:   "success",
		Data:      data,
		RequestID: requestid.FromGin(c),
	})
}

func Error(c *gin.Context, httpStatus int, message string, data any) {
	ErrorWithStatus(c, httpStatus, httpStatus, message, data)
}

func ErrorWithStatus(c *gin.Context, httpStatus int, businessStatus int, message string, data any) {
	c.Set(ContextKeyErrorResponse, ErrorResponseLog{
		Status:         httpStatus,
		BusinessStatus: businessStatus,
		Message:        message,
	})
	JSONWithStatus(c, httpStatus, businessStatus, message, data)
}

func MarkErrorLogged(c *gin.Context) {
	c.Set(ContextKeyErrorLogged, true)
}

func IsErrorLogged(c *gin.Context) bool {
	value, exists := c.Get(ContextKeyErrorLogged)
	if !exists {
		return false
	}
	logged, ok := value.(bool)
	return ok && logged
}

func ErrorResponseFromGin(c *gin.Context) (ErrorResponseLog, bool) {
	value, exists := c.Get(ContextKeyErrorResponse)
	if !exists {
		return ErrorResponseLog{}, false
	}
	errorResponse, ok := value.(ErrorResponseLog)
	return errorResponse, ok
}

func JSON(c *gin.Context, httpStatus int, message string, data any) {
	JSONWithStatus(c, httpStatus, httpStatus, message, data)
}

func JSONWithStatus(c *gin.Context, httpStatus int, businessStatus int, message string, data any) {
	c.JSON(httpStatus, Envelope{
		Status:    businessStatus,
		Message:   message,
		Data:      formatResponseTimes(data),
		RequestID: requestid.FromGin(c),
	})
}

func formatResponseTimes(data any) any {
	if data == nil {
		return nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return data
	}

	return formatTimeStrings(value)
}

func formatTimeStrings(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			typed[key] = formatTimeStrings(nested)
		}
		return typed
	case []any:
		for index, nested := range typed {
			typed[index] = formatTimeStrings(nested)
		}
		return typed
	case string:
		return formatTimeString(typed)
	default:
		return value
	}
}

func formatTimeString(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}

	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return value
	}
	return parsed.Local().Format(timeDisplayLayout)
}

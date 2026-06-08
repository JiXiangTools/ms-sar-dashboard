package middleware

import (
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

func appendErrorResponseLogFields(c *gin.Context, fields []logx.Field) []logx.Field {
	errorResponse, ok := response.ErrorResponseFromGin(c)
	if !ok || len(errorResponse.Details) == 0 {
		return fields
	}

	keys := make([]string, 0, len(errorResponse.Details))
	for key := range errorResponse.Details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fields = append(fields, logx.Any(key, errorResponse.Details[key]))
	}
	return fields
}

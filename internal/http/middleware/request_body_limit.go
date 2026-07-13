package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

const contextKeyRequestBody = "request_body_bytes"

// RequestBodyLimit reads and restores request bodies before route handlers run.
// This makes the limit deterministic for both Content-Length and streamed bodies.
func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			_ = c.Request.Body.Close()
			writeRequestBodyTooLarge(c)
			return
		}

		limitedBody := http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		body, err := io.ReadAll(limitedBody)
		_ = limitedBody.Close()
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeRequestBodyTooLarge(c)
				return
			}
			response.Error(c, http.StatusBadRequest, "invalid request body", nil)
			c.Abort()
			return
		}

		c.Set(contextKeyRequestBody, body)
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Next()
	}
}

func bufferedRequestBody(c *gin.Context) []byte {
	value, exists := c.Get(contextKeyRequestBody)
	if !exists {
		return nil
	}
	body, _ := value.([]byte)
	return body
}

func writeRequestBodyTooLarge(c *gin.Context) {
	response.Error(c, http.StatusRequestEntityTooLarge, "request body too large", nil)
	c.Abort()
}

package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitRejectsKnownAndChunkedBodiesBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name    string
		chunked bool
	}{
		{name: "content length"},
		{name: "chunked", chunked: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handlerCalled := false
			engine := gin.New()
			engine.Use(RequestBodyLimit(8))
			engine.POST("/upload", func(c *gin.Context) {
				handlerCalled = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("123456789"))
			if testCase.chunked {
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			if handlerCalled {
				t.Fatal("oversized request reached route handler")
			}
			if !strings.Contains(recorder.Body.String(), `"message":"request body too large"`) {
				t.Fatalf("unexpected response body: %s", recorder.Body.String())
			}
		})
	}
}

func TestRequestBodyLimitRestoresAllowedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestBodyLimit(8))
	engine.POST("/upload", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		c.String(http.StatusOK, string(body))
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("12345678")))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "12345678" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

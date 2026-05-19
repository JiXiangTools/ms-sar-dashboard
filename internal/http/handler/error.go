package handler

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/http/middleware"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/logx"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/response"
)

const ContextKeyLogger = "logger"

func writeError(c *gin.Context, err error) {
	logHandlerError(c, err)
	response.MarkErrorLogged(c)

	if appErr, ok := apperror.As(err); ok {
		if appErr.HTTPStatus >= http.StatusInternalServerError {
			_ = c.Error(err)
		}
		response.ErrorWithStatus(c, appErr.HTTPStatus, appErr.Status, appErr.Message, nil)
		return
	}
	_ = c.Error(err)
	response.Error(c, http.StatusInternalServerError, "internal server error", nil)
}

func logHandlerError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	loggerValue, ok := c.Get(ContextKeyLogger)
	if !ok {
		return
	}
	logger, ok := loggerValue.(*log.Logger)
	if !ok || logger == nil {
		return
	}

	status := http.StatusInternalServerError
	businessStatus := http.StatusInternalServerError
	message := "internal server error"
	if appErr, ok := apperror.As(err); ok {
		status = appErr.HTTPStatus
		businessStatus = appErr.Status
		message = appErr.Message
	}

	startedAt, _ := c.Get(middleware.ContextKeyRequestStartedAt)
	requestStartedAt, _ := startedAt.(time.Time)
	logx.Error(logger, c.Request.Context(), requestStartedAt, "http.error", "handler error",
		logx.String("method", c.Request.Method),
		logx.String("path", c.Request.URL.Path),
		logx.Int("status", status),
		logx.Int("business_status", businessStatus),
		logx.String("message", message),
		logx.Err(err),
	)
}

package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	HTTPStatus int
	Status     int
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func As(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func New(httpStatus int, status int, message string, cause error) *Error {
	return &Error{HTTPStatus: httpStatus, Status: status, Message: message, Cause: cause}
}

func BadRequest(message string, cause error) *Error {
	return New(http.StatusBadRequest, http.StatusBadRequest, message, cause)
}

func Unauthorized(message string, cause error) *Error {
	return New(http.StatusUnauthorized, http.StatusUnauthorized, message, cause)
}

func Forbidden(message string, cause error) *Error {
	return New(http.StatusForbidden, http.StatusForbidden, message, cause)
}

func NotFound(message string, cause error) *Error {
	return New(http.StatusNotFound, http.StatusNotFound, message, cause)
}

func Conflict(message string, cause error) *Error {
	return New(http.StatusConflict, http.StatusConflict, message, cause)
}

func Internal(message string, cause error) *Error {
	return New(http.StatusInternalServerError, http.StatusInternalServerError, message, cause)
}

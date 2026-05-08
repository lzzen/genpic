package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Standard error types — kept consistent with OpenAI's documented types so
// OpenAI-compatible clients can map them to familiar behaviours.
const (
	TypeInvalidRequest = "invalid_request_error"
	TypeAuthentication = "authentication_error"
	TypePermission     = "permission_error"
	TypeNotFound       = "not_found_error"
	TypeRateLimit      = "rate_limit_error"
	TypeUpstream       = "upstream_error"
	TypeInternal       = "internal_server_error"
	TypeValidation     = "validation_error"
)

// APIErr is a structured error that carries both a machine-readable type/code
// and the HTTP status code to respond with. It is the single error type the
// HTTP layer knows how to serialise.
type APIErr struct {
	// HTTPStatus is the HTTP response code to use, e.g. 400, 401, 429.
	HTTPStatus int
	// Type is a machine-readable category (one of the Type* constants above).
	Type string
	// Code is an optional fine-grained code within the type, e.g. "prompt_too_long".
	Code string
	// Message is a human-readable description safe to send to callers.
	Message string
	// cause wraps the underlying error for internal logging; never sent to callers.
	cause error
}

func (e *APIErr) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s/%s: %s", e.Type, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *APIErr) Unwrap() error { return e.cause }

// New creates an APIErr without wrapping an underlying cause.
func New(status int, errType, code, message string) *APIErr {
	return &APIErr{HTTPStatus: status, Type: errType, Code: code, Message: message}
}

// Wrap creates an APIErr that wraps an underlying cause for internal logging.
// The cause is not included in the message sent to callers.
func Wrap(status int, errType, code, message string, cause error) *APIErr {
	return &APIErr{HTTPStatus: status, Type: errType, Code: code, Message: message, cause: cause}
}

// BadRequest constructs a 400 invalid-request error.
func BadRequest(code, message string) *APIErr {
	return New(http.StatusBadRequest, TypeInvalidRequest, code, message)
}

// Unauthorized constructs a 401 authentication error.
func Unauthorized(message string) *APIErr {
	return New(http.StatusUnauthorized, TypeAuthentication, "", message)
}

// Forbidden constructs a 403 permission error.
func Forbidden(message string) *APIErr {
	return New(http.StatusForbidden, TypePermission, "", message)
}

// NotFound constructs a 404 error.
func NotFound(resource string) *APIErr {
	return New(http.StatusNotFound, TypeNotFound, "", resource+" not found")
}

// RateLimit constructs a 429 error.
func RateLimit(message string) *APIErr {
	return New(http.StatusTooManyRequests, TypeRateLimit, "", message)
}

// UpstreamErr constructs a 502 error for upstream provider failures.
func UpstreamErr(code, message string, cause error) *APIErr {
	return Wrap(http.StatusBadGateway, TypeUpstream, code, message, cause)
}

// UpstreamTimeout constructs a 504 error.
func UpstreamTimeout() *APIErr {
	return New(http.StatusGatewayTimeout, TypeUpstream, "upstream_timeout", "upstream did not respond in time")
}

// Internal constructs a 500 error. The cause is logged but never surfaced to callers.
func Internal(cause error) *APIErr {
	return Wrap(http.StatusInternalServerError, TypeInternal, "", "an internal error occurred", cause)
}

// As reports whether any error in the chain matches *APIErr.
func As(err error) (*APIErr, bool) {
	var e *APIErr
	ok := errors.As(err, &e)
	return e, ok
}

// Is delegates to the standard library.
var Is = errors.Is

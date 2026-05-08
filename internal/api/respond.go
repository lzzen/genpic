// Package api contains HTTP handlers, middleware, and DTO types for the
// Genpic platform's v1 external surface.
//
// Handlers are thin: they validate input, call domain/pkg functions, and
// serialise the result. Business logic lives in pkg/ and internal subdirectories.
package api

import (
	"encoding/json"
	"net/http"

	pkgerrors "genpic/pkg/errors"
)

// JSON writes a JSON-encoded value with the given HTTP status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes an OpenAI-compatible error response. If err is an *APIErr the
// status code and fields are taken from it; otherwise a 500 is returned.
func Error(w http.ResponseWriter, err error) {
	if apiErr, ok := pkgerrors.As(err); ok {
		JSON(w, apiErr.HTTPStatus, map[string]any{
			"error": map[string]any{
				"type":    apiErr.Type,
				"code":    apiErr.Code,
				"message": apiErr.Message,
			},
		})
		return
	}
	JSON(w, http.StatusInternalServerError, map[string]any{
		"error": map[string]any{
			"type":    pkgerrors.TypeInternal,
			"message": "an internal error occurred",
		},
	})
}

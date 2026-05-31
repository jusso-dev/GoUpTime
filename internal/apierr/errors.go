// Package apierr defines sentinel errors used at service/storage boundaries
// and helpers that map them to HTTP status codes. Keeping these in one place
// avoids leaking storage-specific error types into
// HTTP handlers and makes error responses consistent across the API surface.
package apierr

import (
	"context"
	"errors"
	"net/http"
)

var (
	// ErrNotFound is returned by repository methods when an entity cannot
	// be located. Handlers should map it to 404.
	ErrNotFound = errors.New("resource not found")

	// ErrConflict is returned for unique constraint violations and other
	// concurrency conflicts. Handlers should map it to 409.
	ErrConflict = errors.New("resource conflict")

	// ErrInvalidInput indicates user-supplied data failed validation
	// beyond what the binding layer caught. Handlers should map it to 422.
	ErrInvalidInput = errors.New("invalid input")

	// ErrUnauthorized indicates missing or invalid credentials.
	ErrUnauthorized = errors.New("unauthorized")
)

// StatusFor maps an error to an appropriate HTTP status code. Callers should
// pass the most specific error they have; unknown errors map to 500.
func StatusFor(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidInput):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		// 499 is non-standard but widely used (nginx) for client-aborted
		// requests; we surface it so logs/metrics can distinguish a client
		// disconnect from a server-side failure.
		return 499
	default:
		return http.StatusInternalServerError
	}
}

// PublicMessage returns an error message safe to expose to API clients.
// Sentinel errors expose their message; everything else collapses to a
// generic string to avoid leaking driver internals or stack traces.
func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return "resource not found"
	case errors.Is(err, ErrConflict):
		return "resource conflict"
	case errors.Is(err, ErrInvalidInput):
		return err.Error()
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	case errors.Is(err, context.Canceled):
		return "request cancelled"
	default:
		return "internal server error"
	}
}

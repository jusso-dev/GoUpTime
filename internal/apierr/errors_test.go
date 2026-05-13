package apierr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{ErrNotFound, http.StatusNotFound},
		{fmt.Errorf("wrapped: %w", ErrNotFound), http.StatusNotFound},
		{ErrConflict, http.StatusConflict},
		{ErrInvalidInput, http.StatusUnprocessableEntity},
		{ErrUnauthorized, http.StatusUnauthorized},
		{context.DeadlineExceeded, http.StatusGatewayTimeout},
		{context.Canceled, 499},
		{errors.New("anything else"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		if got := StatusFor(tt.err); got != tt.want {
			t.Errorf("StatusFor(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestPublicMessageHidesInternalDetails(t *testing.T) {
	msg := PublicMessage(errors.New("driver: connection refused at 10.0.0.5:5432"))
	if msg != "internal server error" {
		t.Fatalf("internal error message leaked: %q", msg)
	}
}

func TestPublicMessageReturnsInputValidationDetails(t *testing.T) {
	err := fmt.Errorf("%w: id is required", ErrInvalidInput)
	if got := PublicMessage(err); got != err.Error() {
		t.Fatalf("invalid input message dropped: got %q want %q", got, err.Error())
	}
}

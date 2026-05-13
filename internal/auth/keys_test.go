package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIKeyHashAndValidation(t *testing.T) {
	raw, err := NewRawKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "upt_") {
		t.Fatalf("expected upt_ prefix, got %q", raw)
	}
	hash := Hash(raw)
	if hash == raw {
		t.Fatal("hash should not equal raw key")
	}
	if !Matches(raw, hash) {
		t.Fatal("expected raw key to match hash")
	}
	if Matches(raw+"x", hash) {
		t.Fatal("expected wrong key to fail")
	}
	if Matches("", hash) || Matches(raw, "") {
		t.Fatal("empty inputs must not match")
	}
}

func TestPreview(t *testing.T) {
	raw, err := NewRawKey()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Preview(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(preview, "upt_") {
		t.Fatalf("preview missing prefix: %q", preview)
	}
	if len(preview) >= len(raw) {
		t.Fatalf("preview must be shorter than raw key: %q", preview)
	}
}

func TestPreviewRejectsBadInput(t *testing.T) {
	if _, err := Preview("nope"); !errors.Is(err, ErrInvalidKeyFormat) {
		t.Fatalf("expected ErrInvalidKeyFormat, got %v", err)
	}
	if _, err := Preview("xyz_aaaabbbb"); !errors.Is(err, ErrInvalidKeyFormat) {
		t.Fatalf("expected ErrInvalidKeyFormat, got %v", err)
	}
}

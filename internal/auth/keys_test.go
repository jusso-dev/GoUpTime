package auth

import "testing"

func TestAPIKeyHashAndValidation(t *testing.T) {
	raw, err := NewRawKey()
	if err != nil {
		t.Fatal(err)
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
}

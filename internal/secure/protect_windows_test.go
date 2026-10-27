//go:build windows

package secure

import (
	"errors"
	"testing"
)

func TestUnprotectOfDataFromElsewhereIsErrOtherUser(t *testing.T) {
	if _, err := Unprotect([]byte("not a DPAPI blob made for this user")); !errors.Is(err, ErrOtherUser) {
		t.Fatalf("Unprotect error = %v, want ErrOtherUser", err)
	}
}

func TestProtectRoundTrip(t *testing.T) {
	sealed, err := Protect([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Unprotect(sealed)
	if err != nil || string(plain) != "secret" {
		t.Fatalf("round trip = %q, %v", plain, err)
	}
}

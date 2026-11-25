package session

import (
	"errors"
	"testing"

	"SnakeMedia/internal/providers/flow/chrome"
)

func TestSignOutRequiresConsecutiveSignInObservations(t *testing.T) {
	var observations signInObservations
	for _, err := range []error{chrome.ErrLoginRequired, chrome.ErrPageNotReady, chrome.ErrLoginRequired, errors.New("network error"), chrome.ErrLoginRequired, nil, chrome.ErrLoginRequired} {
		if observations.signedOut(err, 2) {
			t.Fatalf("recorded sign-out after an inconclusive page read: %v", err)
		}
	}
	if !observations.signedOut(chrome.ErrLoginRequired, 2) {
		t.Fatal("two consecutive sign-in observations did not confirm sign-out")
	}
}

package main

import (
	"testing"

	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// A deployment without a seal key must hand the login handler a NIL INTERFACE,
// not a nil *mfa.Framework inside one (P3-15). The second passed the handler's
// `MFA == nil` guard, and every password sign-in answered 500.
func TestNoFrameworkIsANilChallenger(t *testing.T) {
	if got := challenger(nil); got != nil {
		t.Fatalf("challenger(nil) = %#v — a typed nil the login handler's nil check cannot see", got)
	}
	if got := challenger(&mfa.Framework{}); got == nil {
		t.Fatal("challenger dropped a configured framework")
	}
}

// The same trap for the mandate check, which the handlers also test with != nil.
func TestNoEnrollerMeansNoMandateCheck(t *testing.T) {
	if got := mandateCheck(nil, nil, nil); got != nil {
		t.Fatalf("mandateCheck(nil, …) = %#v, want nil", got)
	}
}

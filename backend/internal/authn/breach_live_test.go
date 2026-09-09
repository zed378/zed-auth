//go:build manual

package authn

import (
	"context"
	"testing"
	"time"
)

// Verification against the real corpus service, run by hand.
//
// Behind a build tag deliberately. A test that depends on a third party is a
// test that fails on their bad day, and a suite whose failures nobody believes
// is worse than a slower one (P1-03's lesson). The stub tests assert the wire
// format; this asserts that the format is still what the service sends.
//
//	go test -tags=manual ./internal/authn/ -run TestLiveCorpus -v
//
// Re-run it when the client changes or when a breach check starts reporting
// skips it should not be reporting.
func TestLiveCorpusAnswersAsExpected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := NewBreachClient()

	// A password that is certainly in any breach corpus. If this comes back
	// clean, the client is talking to something that is not the corpus.
	breached, err := client.Breached(ctx, "password")
	if err != nil {
		t.Fatalf("live lookup failed: %v", err)
	}
	if !breached {
		t.Error(`"password" was reported clean; the client is not reading the corpus correctly`)
	}

	// A password that is certainly not. Proves the client can return false for
	// a reason other than failing to find anything.
	clean, err := client.Breached(ctx, "kAt7-vQ3z!Lm9x_Wp2Rn6Yb4Tc8Hs1Jd5Fg")
	if err != nil {
		t.Fatalf("live lookup failed: %v", err)
	}
	if clean {
		t.Error("a random 35-character password was reported breached")
	}
}

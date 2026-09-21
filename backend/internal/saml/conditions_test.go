package saml

import (
	"errors"
	"testing"
	"time"

	"github.com/beevik/etree"
)

// Audience confusion and the validity window (P4-07 A-5).

const ourEntityID = "https://sp.example.test"

// withConditions returns a Verified wrapping an assertion carrying `conditions`.
//
// It constructs the Verified directly rather than signing and verifying,
// because these tests are about what happens AFTER verification. Signing here
// would make every case slower and would test goxmldsig again.
func withConditions(t *testing.T, conditions *etree.Element) Verified {
	t.Helper()
	el := assertion("_a", "budi@example.test")
	if conditions != nil {
		el.AddChild(conditions)
	}
	return Verified{el: el}
}

func TestAnAssertionForThisServiceProviderPasses(t *testing.T) {
	now := time.Now()
	v := withConditions(t, Conditions(ourEntityID, now.Add(-time.Minute), 5*time.Minute))

	if err := CheckConditions(v, ourEntityID, now); err != nil {
		t.Fatalf("a valid assertion was refused: %v", err)
	}
}

// A-5: the assertion is genuine, signed, unexpired — and addressed to somebody
// else.
func TestAnAssertionForAnotherServiceProviderIsRefused(t *testing.T) {
	now := time.Now()
	v := withConditions(t, Conditions("https://other.example.test", now.Add(-time.Minute), 5*time.Minute))

	if err := CheckConditions(v, ourEntityID, now); !errors.Is(err, ErrAudience) {
		t.Errorf("an assertion for another service provider gave %v, want ErrAudience", err)
	}
}

// The naive check — "are we mentioned anywhere" — accepts this. Two
// restrictions, one naming us and one naming somebody else: the specification
// requires every restriction to be satisfied.
func TestEveryAudienceRestrictionMustBeSatisfied(t *testing.T) {
	now := time.Now()
	conditions := Conditions(ourEntityID, now.Add(-time.Minute), 5*time.Minute)
	second := conditions.CreateElement("AudienceRestriction")
	second.CreateElement("Audience").SetText("https://other.example.test")

	v := withConditions(t, conditions)
	if err := CheckConditions(v, ourEntityID, now); !errors.Is(err, ErrAudience) {
		t.Errorf("an assertion naming us in one restriction and another party in a second gave %v, want ErrAudience", err)
	}
}

// And the legitimate multi-audience shape still works: one restriction listing
// several audiences is satisfied by any of them.
func TestOneRestrictionListingSeveralAudiencesIsSatisfiedByAny(t *testing.T) {
	now := time.Now()
	conditions := Conditions("https://other.example.test", now.Add(-time.Minute), 5*time.Minute)
	conditions.FindElement("./AudienceRestriction").CreateElement("Audience").SetText(ourEntityID)

	v := withConditions(t, conditions)
	if err := CheckConditions(v, ourEntityID, now); err != nil {
		t.Errorf("an assertion listing us among a restriction's audiences was refused: %v", err)
	}
}

func TestAnExpiredAssertionIsRefused(t *testing.T) {
	now := time.Now()
	// Issued ten minutes ago with a five-minute life: expired, and well past
	// any clock-skew allowance.
	v := withConditions(t, Conditions(ourEntityID, now.Add(-10*time.Minute), 5*time.Minute))

	if err := CheckConditions(v, ourEntityID, now); !errors.Is(err, ErrExpired) {
		t.Errorf("an expired assertion gave %v, want ErrExpired", err)
	}
}

func TestAnAssertionFromTheFutureIsRefused(t *testing.T) {
	now := time.Now()
	v := withConditions(t, Conditions(ourEntityID, now.Add(10*time.Minute), 5*time.Minute))

	if err := CheckConditions(v, ourEntityID, now); !errors.Is(err, ErrExpired) {
		t.Errorf("an assertion not yet valid gave %v, want ErrExpired", err)
	}
}

// Clock skew is allowed, and bounded. Both directions matter: too little and
// ordinary drift between two organizations' servers breaks logins, too much and
// a short-lived assertion outlives the request it was issued for.
func TestClockSkewIsAllowedWithinTheBound(t *testing.T) {
	now := time.Now()

	justStarted := withConditions(t, Conditions(ourEntityID, now.Add(MaxClockSkew/2), time.Minute))
	if err := CheckConditions(justStarted, ourEntityID, now); err != nil {
		t.Errorf("an assertion starting within the skew allowance was refused: %v", err)
	}

	justEnded := withConditions(t, Conditions(ourEntityID, now.Add(-time.Minute-MaxClockSkew/2), time.Minute))
	if err := CheckConditions(justEnded, ourEntityID, now); err != nil {
		t.Errorf("an assertion ending within the skew allowance was refused: %v", err)
	}

	// And beyond the bound it is refused, so the allowance is an allowance and
	// not an unbounded grace.
	wellPast := withConditions(t, Conditions(ourEntityID, now.Add(-time.Minute-2*MaxClockSkew), time.Minute))
	if err := CheckConditions(wellPast, ourEntityID, now); !errors.Is(err, ErrExpired) {
		t.Errorf("an assertion past the skew allowance gave %v, want ErrExpired", err)
	}
}

// An assertion with no Conditions is unusable, not unrestricted.
func TestAnAssertionWithNoConditionsIsRefused(t *testing.T) {
	if err := CheckConditions(withConditions(t, nil), ourEntityID, time.Now()); !errors.Is(err, ErrNoConditions) {
		t.Errorf("an assertion with no Conditions gave %v, want ErrNoConditions", err)
	}
}

func TestAnAssertionWithNoExpiryIsRefused(t *testing.T) {
	conditions := etree.NewElement("Conditions")
	restriction := conditions.CreateElement("AudienceRestriction")
	restriction.CreateElement("Audience").SetText(ourEntityID)

	if err := CheckConditions(withConditions(t, conditions), ourEntityID, time.Now()); !errors.Is(err, ErrNoConditions) {
		t.Errorf("an assertion with no NotOnOrAfter gave %v, want a refusal — no expiry is not forever", err)
	}
}

func TestAnAssertionWithNoAudienceRestrictionIsRefused(t *testing.T) {
	now := time.Now()
	conditions := etree.NewElement("Conditions")
	conditions.CreateAttr("NotBefore", now.Add(-time.Minute).UTC().Format(time.RFC3339))
	conditions.CreateAttr("NotOnOrAfter", now.Add(time.Minute).UTC().Format(time.RFC3339))

	if err := CheckConditions(withConditions(t, conditions), ourEntityID, now); !errors.Is(err, ErrAudience) {
		t.Errorf("an assertion with no AudienceRestriction gave %v, want ErrAudience", err)
	}
}

// Entity IDs are opaque identifiers, not addresses. A comparison that
// normalised them would make two textually different parties the same one.
func TestAudienceComparisonIsExact(t *testing.T) {
	now := time.Now()
	for _, near := range []string{
		ourEntityID + "/",
		"HTTPS://SP.EXAMPLE.TEST",
		ourEntityID + ":443",
		" " + ourEntityID,
	} {
		v := withConditions(t, Conditions(near, now.Add(-time.Minute), 5*time.Minute))
		if err := CheckConditions(v, ourEntityID, now); !errors.Is(err, ErrAudience) {
			t.Errorf("audience %q was accepted as %q", near, ourEntityID)
		}
	}
}

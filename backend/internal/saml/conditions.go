package saml

import (
	"errors"
	"fmt"
	"time"

	"github.com/beevik/etree"
)

// The conditions an assertion carries, and why each one is checked here rather
// than trusted from the document (P4-07 A-5).
//
// A signature says an assertion was issued by this service. It says nothing
// about WHO it was issued for or WHEN it stops being true — those are claims
// inside the signed content, and a consumer that verifies the signature and
// then ignores them accepts a genuine assertion in the wrong place, which is
// audience confusion, or long after it expired, which is replay with extra
// steps.

var (
	// ErrAudience is an assertion addressed to a different service provider.
	ErrAudience = errors.New("saml: the assertion is not addressed to this service provider")

	// ErrExpired is an assertion outside its validity window.
	ErrExpired = errors.New("saml: the assertion is outside its validity window")

	// ErrNoConditions is an assertion with no Conditions element.
	//
	// Refused rather than treated as "no restrictions". An assertion that does
	// not say who it is for and when it expires is not a permissive assertion,
	// it is an unusable one — and reading it as permissive is how a token with
	// no expiry becomes a token that never expires.
	ErrNoConditions = errors.New("saml: the assertion carries no Conditions")
)

// Clock skew allowed on each side of the validity window.
//
// SAML deployments involve two organizations' servers, and neither is obliged
// to run NTP. Thirty seconds absorbs ordinary drift without meaningfully
// extending the window; a minute is common and is already enough to make a
// short-lived assertion outlive the request it was issued for.
const MaxClockSkew = 30 * time.Second

// CheckConditions verifies the audience and the validity window of a verified
// assertion.
//
// It takes a Verified rather than an element, so it cannot be called on
// something nobody authenticated. The order of the arguments is the order of the
// questions: is this for me, and is it still true.
func CheckConditions(v Verified, audience string, now time.Time) error {
	el := v.Element()
	if el == nil {
		return fmt.Errorf("saml: no assertion to check")
	}

	conditions := el.FindElement("./Conditions")
	if conditions == nil {
		return ErrNoConditions
	}

	// --- the window ---------------------------------------------------------
	//
	// NotBefore is inclusive and NotOnOrAfter is exclusive, per the
	// specification's own naming. Getting that backwards makes an assertion
	// valid for one instant longer than it should be, which nobody notices and
	// which is wrong.
	if raw := conditions.SelectAttrValue("NotBefore", ""); raw != "" {
		notBefore, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return fmt.Errorf("%w: unparseable NotBefore %q", ErrExpired, raw)
		}
		if now.Before(notBefore.Add(-MaxClockSkew)) {
			return fmt.Errorf("%w: not valid until %s", ErrExpired, notBefore.Format(time.RFC3339))
		}
	}
	if raw := conditions.SelectAttrValue("NotOnOrAfter", ""); raw != "" {
		notOnOrAfter, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return fmt.Errorf("%w: unparseable NotOnOrAfter %q", ErrExpired, raw)
		}
		if !now.Before(notOnOrAfter.Add(MaxClockSkew)) {
			return fmt.Errorf("%w: expired at %s", ErrExpired, notOnOrAfter.Format(time.RFC3339))
		}
	} else {
		// No expiry is not "forever".
		return fmt.Errorf("%w: no NotOnOrAfter", ErrNoConditions)
	}

	// --- the audience -------------------------------------------------------
	//
	// Every AudienceRestriction must be satisfied, and each one is satisfied by
	// any of its Audience children. That is the specification's rule, and the
	// naive reading — "is our entity ID mentioned anywhere in the document" —
	// accepts an assertion that names us in one restriction and somebody else
	// in another, which is exactly audience confusion.
	restrictions := conditions.FindElements("./AudienceRestriction")
	if len(restrictions) == 0 {
		return fmt.Errorf("%w: no AudienceRestriction", ErrAudience)
	}
	for _, restriction := range restrictions {
		matched := false
		for _, aud := range restriction.FindElements("./Audience") {
			// Exact string comparison. A URI comparison that normalised case,
			// trailing slashes or default ports would let two entity IDs that
			// are textually different be treated as the same party, and entity
			// IDs are opaque identifiers rather than addresses to resolve.
			if aud.Text() == audience {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: %q is not among the audiences of one restriction", ErrAudience, audience)
		}
	}

	return nil
}

// Conditions builds the element an issued assertion carries.
//
// Exported so the issuing side and the checking side cannot drift: the same
// package writes the window it later enforces.
func Conditions(audience string, notBefore time.Time, lifetime time.Duration) *etree.Element {
	el := etree.NewElement("Conditions")
	el.CreateAttr("NotBefore", notBefore.UTC().Format(time.RFC3339))
	el.CreateAttr("NotOnOrAfter", notBefore.Add(lifetime).UTC().Format(time.RFC3339))

	restriction := el.CreateElement("AudienceRestriction")
	restriction.CreateElement("Audience").SetText(audience)
	return el
}

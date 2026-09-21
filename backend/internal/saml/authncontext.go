package saml

import (
	"errors"
	"sort"
	"strings"
)

// What a service provider is told about HOW the user authenticated (P4-08 C-6).
//
// The threat review is specific: `AuthnContextClassRef` comes from the session's
// recorded `auth_methods` — the same source as the OIDC `amr` claim — and is
// never asserted independently. A `RequestedAuthnContext` the session cannot
// satisfy triggers step-up or a `NoAuthnContext` status, never silence.
//
// The failure that rule prevents is worth naming. A service provider asks for
// multi-factor, the user has a password-only session, and the identity provider
// issues an assertion claiming multi-factor because that is what was asked for.
// Everything succeeds. The service provider believes a second factor was used,
// makes a decision on that basis, and is wrong — and nothing anywhere logged a
// refusal, because there was none.

// The SAML authentication context classes this service asserts.
//
// A small, closed set. Each maps to something the session actually recorded, so
// there is no class this service can claim without having observed it.
const (
	// ClassPassword — a password was used and nothing else.
	ClassPassword = "urn:oasis:names:tc:SAML:2.0:ac:classes:Password"

	// ClassPasswordProtectedTransport — a password over TLS. What this service
	// issues for a password-only session, because every session here is over
	// TLS and the distinction matters to service providers that check it.
	ClassPasswordProtectedTransport = "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"

	// ClassMultiFactor — a second factor was used.
	ClassMultiFactor = "urn:oasis:names:tc:SAML:2.0:ac:classes:MultiFactorAuthentication"

	// ClassUnspecified — authenticated, and this service will not characterise
	// how. Returned rather than guessing when the session's methods map to
	// nothing known.
	ClassUnspecified = "urn:oasis:names:tc:SAML:2.0:ac:classes:unspecified"
)

// ErrNoAuthnContext is a request for a context the session cannot satisfy.
//
// The caller answers it with a SAML `NoAuthnContext` status, or re-authenticates
// the user and asks again. What it must not do is issue an assertion anyway.
var ErrNoAuthnContext = errors.New("saml: the session does not satisfy the requested authentication context")

// ClassFor maps a session's recorded authentication methods to a SAML class.
//
// `methods` is `session.AuthMethods`, which is also what the OIDC `amr` claim is
// built from — one source, so a user who is told `mfa` over OIDC is told
// MultiFactor over SAML, and the two protocols cannot describe the same session
// differently.
func ClassFor(methods []string) string {
	if len(methods) == 0 {
		return ClassUnspecified
	}

	// The spellings `internal/mfa` actually writes: `pwd`, `mfa`, `otp`, `hwk`
	// (RFC 8176). Nothing is inferred from a value this service does not emit.
	var password, certified bool
	for _, method := range methods {
		switch strings.ToLower(strings.TrimSpace(method)) {
		case "pwd":
			password = true
		case "mfa":
			certified = true
		}
	}

	switch {
	case certified:
		return ClassMultiFactor
	case password:
		return ClassPasswordProtectedTransport
	default:
		// A single non-password factor — a passkey-only sign-in, say — lands
		// here, and that is deliberate.
		//
		// The first version of this function returned MultiFactor for `hwk`
		// alone, which is precisely the overclaim C-6 exists to prevent.
		// `internal/mfa` emits `mfa` "only when two distinct factor CATEGORIES
		// were used, never as a synonym for a second factor existing" — so
		// this service has already decided that a passkey alone is not
		// multi-factor, and SAML is not the place to decide otherwise.
		//
		// Unspecified is the honest answer: authenticated, and this service
		// will not characterise how. A service provider that needs more asks
		// for it, and gets NoAuthnContext rather than a comfortable lie.
		return ClassUnspecified
	}
}

// Satisfies reports whether a session's class meets what a service provider
// asked for.
//
// Empty `requested` means the service provider did not ask, and anything
// satisfies it. Otherwise the comparison is exact against the class this
// session actually earned, with one ordering rule: MultiFactor satisfies a
// request for a password class, because a stronger authentication is not a
// failure to meet a weaker requirement.
func Satisfies(requested, actual string) bool {
	if requested == "" {
		return true
	}
	if requested == actual {
		return true
	}
	if actual == ClassMultiFactor {
		switch requested {
		case ClassPassword, ClassPasswordProtectedTransport:
			return true
		}
	}
	return false
}

// CheckAuthnContext decides what to do with a RequestedAuthnContext.
//
// It returns the class to assert, or ErrNoAuthnContext. There is no third
// outcome, and deliberately no "assert what was requested" path: the whole
// point of C-6 is that the assertion describes what happened rather than what
// was asked for.
func CheckAuthnContext(requested string, methods []string) (string, error) {
	actual := ClassFor(methods)
	if !Satisfies(requested, actual) {
		return "", ErrNoAuthnContext
	}
	return actual, nil
}

// KnownClasses lists what this service can assert, for documentation and for
// the metadata a service provider reads.
func KnownClasses() []string {
	classes := []string{
		ClassPasswordProtectedTransport,
		ClassMultiFactor,
		ClassUnspecified,
	}
	sort.Strings(classes)
	return classes
}

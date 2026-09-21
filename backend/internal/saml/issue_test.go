package saml

import (
	"crypto/x509"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/signing"
)

// Issuing an assertion, and then consuming it through this package's own checks
// (P4-07 F-5, F-6, A-7).
//
// The round trip is the point. A test that only inspects the XML it just built
// proves the builder agrees with itself; putting the result through Verify,
// CheckConditions and the replay store proves the two halves agree — which is
// the property that breaks when somebody changes one of them.

const testIssuer = "https://auth.example.test"

func samlKey(t *testing.T) *SigningKey {
	t.Helper()
	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	certPEM, err := SelfSignedCertificate(pair, testIssuer, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("certifying the key: %v", err)
	}
	key, err := NewSigningKey(pair, certPEM)
	if err != nil {
		t.Fatalf("adapting the key: %v", err)
	}
	return key
}

func testSP() ServiceProvider {
	return ServiceProvider{
		EntityID: ourEntityID,
		ACSURL:   "https://sp.example.test/acs",
		Release:  []string{"email"},
	}
}

func testSubject() Subject {
	return Subject{
		NameID:               "budi@example.test",
		NameIDFormat:         "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
		Attributes:           map[string][]string{"email": {"budi@example.test"}, "groups": {"finance"}},
		SessionIndex:         "sess-1",
		AuthnInstant:         time.Now().Add(-2 * time.Minute),
		AuthnContextClassRef: "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport",
	}
}

func TestAnIssuedAssertionVerifiesAndSatisfiesItsOwnConditions(t *testing.T) {
	key := samlKey(t)
	now := time.Now()

	signed, err := Issue(key, testIssuer, testSP(), testSubject(), "_req1", now)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	cert, err := key.Certificate()
	if err != nil {
		t.Fatalf("reading the certificate: %v", err)
	}

	v, err := Verify(serialise(t, signed), "Assertion", []*x509.Certificate{cert})
	if err != nil {
		t.Fatalf("an assertion this service issued did not verify: %v", err)
	}
	if err := CheckConditions(v, ourEntityID, now); err != nil {
		t.Fatalf("an assertion this service issued failed its own conditions: %v", err)
	}
	if !strings.HasPrefix(v.ID(), "_") {
		t.Errorf("assertion ID %q does not begin with _ — xsd:ID may not start with a digit", v.ID())
	}
}

// F-6: the service provider receives what it was registered for, and nothing
// else. `groups` is known to this service and not released.
func TestOnlyRegisteredAttributesAreReleased(t *testing.T) {
	key := samlKey(t)
	signed, err := Issue(key, testIssuer, testSP(), testSubject(), "", time.Now())
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	doc := string(serialise(t, signed))
	if !strings.Contains(doc, "budi@example.test") {
		t.Error("the released attribute is missing")
	}
	if strings.Contains(doc, "finance") || strings.Contains(doc, "groups") {
		t.Error("an attribute the service provider was not registered for was released")
	}
}

// A service provider that asked for nothing receives nothing — not everything.
func TestAServiceProviderWithNoRegistrationReceivesNoAttributes(t *testing.T) {
	key := samlKey(t)
	sp := testSP()
	sp.Release = nil

	signed, err := Issue(key, testIssuer, sp, testSubject(), "", time.Now())
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	if doc := string(serialise(t, signed)); strings.Contains(doc, "AttributeStatement") {
		t.Error("an unregistered service provider received an AttributeStatement")
	}
}

func TestReleaseIsAnAllowList(t *testing.T) {
	available := map[string][]string{"email": {"a@b.test"}, "groups": {"x"}, "role": {"admin"}}

	got := Release(available, []string{"email", "absent"})
	if len(got) != 1 || got["email"][0] != "a@b.test" {
		t.Errorf("Release returned %v, want only email", got)
	}
	if Release(available, nil) != nil {
		t.Error("an empty allow list released something")
	}
	if Release(nil, []string{"email"}) != nil {
		t.Error("releasing from nothing produced something")
	}
}

// The recipient is the REGISTERED ACS URL. An assertion naming wherever a
// request pointed would be an open redirect with a signature on it.
func TestTheRecipientIsTheRegisteredACSURL(t *testing.T) {
	key := samlKey(t)
	signed, err := Issue(key, testIssuer, testSP(), testSubject(), "_req1", time.Now())
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	data := signed.FindElement("./Subject/SubjectConfirmation/SubjectConfirmationData")
	if data == nil {
		t.Fatal("the assertion carries no SubjectConfirmationData")
	}
	if got := data.SelectAttrValue("Recipient", ""); got != testSP().ACSURL {
		t.Errorf("Recipient is %q, want the registered %q", got, testSP().ACSURL)
	}
	if got := data.SelectAttrValue("InResponseTo", ""); got != "_req1" {
		t.Errorf("InResponseTo is %q, want the request id", got)
	}
}

// The authentication instant is when the user authenticated, not when the
// assertion was built. Reporting the second makes every assertion look freshly
// authenticated to a service provider deciding whether to re-prompt.
func TestTheAuthnInstantIsTheAuthenticationNotTheIssuance(t *testing.T) {
	key := samlKey(t)
	subject := testSubject()
	subject.AuthnInstant = time.Now().Add(-30 * time.Minute)

	signed, err := Issue(key, testIssuer, testSP(), subject, "", time.Now())
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	authn := signed.FindElement("./AuthnStatement")
	if authn == nil {
		t.Fatal("no AuthnStatement")
	}
	instant, err := time.Parse(time.RFC3339, authn.SelectAttrValue("AuthnInstant", ""))
	if err != nil {
		t.Fatalf("unparseable AuthnInstant: %v", err)
	}
	if time.Since(instant) < 25*time.Minute {
		t.Errorf("AuthnInstant is %s — it reports the issuance, not the authentication", instant)
	}
}

// A-5 from the issuing side: the audience is the service provider's entity ID,
// so an assertion for one cannot satisfy another.
func TestAnIssuedAssertionIsRefusedByAnotherServiceProvider(t *testing.T) {
	key := samlKey(t)
	now := time.Now()

	signed, err := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	cert, _ := key.Certificate()
	v, err := Verify(serialise(t, signed), "Assertion", []*x509.Certificate{cert})
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}

	if err := CheckConditions(v, "https://other.example.test", now); err == nil {
		t.Error("an assertion issued for one service provider satisfied another")
	}
}

// A-7: the certificate must describe the key that signs, or a service provider
// pins something that verifies nothing.
func TestACertificateForAnotherKeyIsRefused(t *testing.T) {
	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	other, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	certPEM, err := SelfSignedCertificate(other, testIssuer, time.Now())
	if err != nil {
		t.Fatalf("certifying: %v", err)
	}

	if _, err := NewSigningKey(pair, certPEM); err == nil {
		t.Error("a key was paired with a certificate for a different key")
	}
}

func TestAnECKeyIsRefusedForSAML(t *testing.T) {
	pair, err := signing.Generate(signing.ES256)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	if _, err := SelfSignedCertificate(pair, testIssuer, time.Now()); err == nil {
		t.Error("an EC key was certified for SAML, which signs with RSA")
	}
}

func TestIssuingRefusesAnIncompleteRegistration(t *testing.T) {
	key := samlKey(t)
	now := time.Now()

	for name, sp := range map[string]ServiceProvider{
		"no entity id": {ACSURL: "https://sp.example.test/acs"},
		"no ACS url":   {EntityID: ourEntityID},
	} {
		if _, err := Issue(key, testIssuer, sp, testSubject(), "", now); err == nil {
			t.Errorf("%s: an assertion was issued anyway", name)
		}
	}

	if _, err := Issue(key, testIssuer, testSP(), Subject{}, "", now); err == nil {
		t.Error("an assertion with no subject was issued")
	}
}

package saml

import (
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

// What the metadata promises (P4-08).

func testEndpoints() Endpoints {
	return Endpoints{
		SSORedirect: "https://auth.example.test/saml/sso",
		SSOPost:     "https://auth.example.test/saml/sso",
	}
}

func TestMetadataAdvertisesTheCertificateAssertionsAreSignedWith(t *testing.T) {
	key := samlKey(t)

	entity, err := Metadata(testIssuer, key, testEndpoints())
	if err != nil {
		t.Fatalf("building metadata: %v", err)
	}
	doc := string(serialise(t, entity))

	// The certificate in the document must be the one that signs — a service
	// provider pins this, and a mismatch is a failure only it can see.
	advertised := entity.FindElement("./IDPSSODescriptor/KeyDescriptor/ds:KeyInfo/ds:X509Data/ds:X509Certificate")
	if advertised == nil {
		t.Fatal("the metadata carries no certificate")
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(advertised.Text()))
	if err != nil {
		t.Fatalf("the advertised certificate is not base64: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("the advertised certificate does not parse: %v", err)
	}

	signing, err := key.Certificate()
	if err != nil {
		t.Fatalf("reading the signing certificate: %v", err)
	}
	if !parsed.Equal(signing) {
		t.Error("the metadata advertises a different certificate from the one assertions are signed with")
	}

	if !strings.Contains(doc, testIssuer) {
		t.Error("the metadata does not carry the entity id")
	}
}

// C-5: never email.
func TestMetadataAdvertisesAPersistentNameIDAndNeverEmail(t *testing.T) {
	key := samlKey(t)
	entity, err := Metadata(testIssuer, key, testEndpoints())
	if err != nil {
		t.Fatalf("building metadata: %v", err)
	}

	formats := entity.FindElements("./IDPSSODescriptor/NameIDFormat")
	if len(formats) == 0 {
		t.Fatal("no NameIDFormat advertised")
	}
	for _, format := range formats {
		if strings.Contains(strings.ToLower(format.Text()), "email") {
			t.Errorf("the metadata advertises %q — an SP keyed on email inherits a reissued address", format.Text())
		}
	}
	if formats[0].Text() != NameIDFormatPersistent {
		t.Errorf("NameIDFormat is %q, want persistent", formats[0].Text())
	}
}

func TestMetadataAdvertisesBothSSOBindings(t *testing.T) {
	key := samlKey(t)
	entity, err := Metadata(testIssuer, key, testEndpoints())
	if err != nil {
		t.Fatalf("building metadata: %v", err)
	}

	bindings := map[string]bool{}
	for _, sso := range entity.FindElements("./IDPSSODescriptor/SingleSignOnService") {
		bindings[sso.SelectAttrValue("Binding", "")] = true
		if sso.SelectAttrValue("Location", "") == "" {
			t.Error("a SingleSignOnService has no Location")
		}
	}
	for _, want := range []string{
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
	} {
		if !bindings[want] {
			t.Errorf("the metadata does not advertise %s", want)
		}
	}
}

// The absence that is a decision: no logout endpoint is advertised, because
// there is none. Advertising one that answers RequestDenied would be worse —
// a service provider would build a logout button on it.
func TestMetadataAdvertisesNoSingleLogoutService(t *testing.T) {
	key := samlKey(t)
	entity, err := Metadata(testIssuer, key, testEndpoints())
	if err != nil {
		t.Fatalf("building metadata: %v", err)
	}

	if slo := entity.FindElement("./IDPSSODescriptor/SingleLogoutService"); slo != nil {
		t.Errorf("the metadata advertises a SingleLogoutService at %q, and there is none",
			slo.SelectAttrValue("Location", ""))
	}
}

func TestMetadataRefusesToDescribeEndpointsThatDoNotExist(t *testing.T) {
	key := samlKey(t)

	if _, err := Metadata("", key, testEndpoints()); err == nil {
		t.Error("metadata was built with no entity id")
	}
	if _, err := Metadata(testIssuer, key, Endpoints{}); err == nil {
		t.Error("metadata was built with no SSO endpoints — it would advertise a 404")
	}
	if _, err := Metadata(testIssuer, key, Endpoints{SSORedirect: "https://x.test/sso"}); err == nil {
		t.Error("metadata was built advertising only one binding of two")
	}
}

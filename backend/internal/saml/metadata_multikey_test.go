package saml

import (
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

// The overlap a SAML rotation depends on (P4-09).
//
// A service provider pins what it reads here. Publishing only the key that
// signs now means the instant `keyctl -purpose saml rotate` promotes a new
// one, every service provider still pinning the old certificate rejects every
// assertion — the OIDC procedure's "publish, wait, then sign" has no
// counterpart unless there is somewhere to publish a key that is not signing.
//
// This is that somewhere, so it is asserted rather than assumed.
func TestMetadataAdvertisesTheKeyThatWillSignAsWellAsTheOneThatDoes(t *testing.T) {
	current := samlKey(t)
	next := samlKey(t)

	entity, err := Metadata(testIssuer, certificatesOf(t, current, next), testEndpoints())
	if err != nil {
		t.Fatalf("building metadata: %v", err)
	}

	advertised := entity.FindElements("./IDPSSODescriptor/KeyDescriptor")
	if len(advertised) != 2 {
		t.Fatalf("%d KeyDescriptors, want 2 — a service provider cannot trust a key "+
			"the document does not carry", len(advertised))
	}

	var published []string
	for _, descriptor := range advertised {
		if use := descriptor.SelectAttrValue("use", ""); use != "signing" {
			t.Errorf("a KeyDescriptor has use=%q, want signing", use)
		}
		el := descriptor.FindElement("./ds:KeyInfo/ds:X509Data/ds:X509Certificate")
		if el == nil {
			t.Fatal("a KeyDescriptor carries no certificate")
		}
		published = append(published, strings.TrimSpace(el.Text()))
	}

	currentCert, err := current.Certificate()
	if err != nil {
		t.Fatalf("current certificate: %v", err)
	}
	nextCert, err := next.Certificate()
	if err != nil {
		t.Fatalf("next certificate: %v", err)
	}

	// Order is the document's meaning: whichever signs now comes first.
	if published[0] != base64.StdEncoding.EncodeToString(currentCert.Raw) {
		t.Error("the first KeyDescriptor is not the key that signs now")
	}
	if published[1] != base64.StdEncoding.EncodeToString(nextCert.Raw) {
		t.Error("the second KeyDescriptor is not the key that will sign next")
	}
	if published[0] == published[1] {
		t.Error("both KeyDescriptors carry the same certificate, so there is no overlap")
	}
}

// Metadata with no certificate is a document a service provider cannot
// configure itself from, so it is refused rather than published empty.
func TestMetadataWithNoKeyIsRefused(t *testing.T) {
	if _, err := Metadata(testIssuer, nil, testEndpoints()); err == nil {
		t.Error("metadata with no signing key was produced")
	}
	if _, err := Metadata(testIssuer, []*x509.Certificate{}, testEndpoints()); err == nil {
		t.Error("metadata with an empty key list was produced")
	}
}

// certificatesOf is what CachedKeys.Published does for real: the certificate a
// key publishes, with no private half involved.
func certificatesOf(t *testing.T, keys ...*SigningKey) []*x509.Certificate {
	t.Helper()
	out := make([]*x509.Certificate, 0, len(keys))
	for _, key := range keys {
		cert, err := key.Certificate()
		if err != nil {
			t.Fatalf("certificate: %v", err)
		}
		out = append(out, cert)
	}
	return out
}

package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

// A certificate to publish in test metadata.
func testCertificateDER(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sp.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}
	return der
}

func metadataWith(inner string) string {
	return `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"` +
		` xmlns:ds="http://www.w3.org/2000/09/xmldsig#"` +
		` entityID="https://sp.example.test">` + inner + `</md:EntityDescriptor>`
}

func acs(location string, index int, isDefault bool) string {
	return fmt.Sprintf(
		`<md:AssertionConsumerService Binding="%s" Location="%s" index="%d" isDefault="%t"/>`,
		BindingHTTPPost, location, index, isDefault)
}

func keyDescriptor(use string, der []byte) string {
	attr := ""
	if use != "" {
		attr = ` use="` + use + `"`
	}
	return `<md:KeyDescriptor` + attr + `><ds:KeyInfo><ds:X509Data><ds:X509Certificate>` +
		base64.StdEncoding.EncodeToString(der) +
		`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>`
}

// The ordinary case, and what it is allowed to conclude.
func TestSPMetadataIsReadIntoARegistration(t *testing.T) {
	der := testCertificateDER(t)
	raw := metadataWith(
		`<md:SPSSODescriptor AuthnRequestsSigned="true" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` +
			keyDescriptor("signing", der) +
			acs("https://sp.example.test/acs", 0, true) +
			`</md:SPSSODescriptor>`)

	sp, err := ParseSPMetadata([]byte(raw))
	if err != nil {
		t.Fatalf("parsing ordinary metadata: %v", err)
	}

	if sp.EntityID != "https://sp.example.test" {
		t.Errorf("entity id %q", sp.EntityID)
	}
	if sp.ACSURL != "https://sp.example.test/acs" {
		t.Errorf("acs url %q", sp.ACSURL)
	}
	if !sp.WantSignedRequests {
		t.Error("AuthnRequestsSigned=true was not carried")
	}
	if !strings.HasPrefix(sp.CertificatePEM, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("certificate is not PEM: %q", truncateForTest(sp.CertificatePEM))
	}
	// And the certificate that comes back is the one that went in.
	parsed, err := ParseCertificate(sp.CertificatePEM)
	if err != nil {
		t.Fatalf("the parsed certificate does not round-trip: %v", err)
	}
	if parsed.Subject.CommonName != "sp.example.test" {
		t.Errorf("certificate subject %q", parsed.Subject.CommonName)
	}
}

// The choice among several Assertion Consumer Services is a stated rule, not
// whichever the parser happened to return first.
func TestTheAssertionConsumerServiceIsChosenByTheStatedRule(t *testing.T) {
	cases := map[string]struct {
		services []string
		want     string
		wantErr  error
	}{
		"isDefault wins over a lower index": {
			services: []string{
				acs("https://sp.example.test/first", 0, false),
				acs("https://sp.example.test/default", 7, true),
			},
			want: "https://sp.example.test/default",
		},
		"the lowest index wins when none is default": {
			services: []string{
				acs("https://sp.example.test/three", 3, false),
				acs("https://sp.example.test/one", 1, false),
				acs("https://sp.example.test/two", 2, false),
			},
			want: "https://sp.example.test/one",
		},
		"a binding this service never delivers on is not chosen": {
			services: []string{
				`<md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Artifact"` +
					` Location="https://sp.example.test/artifact" index="0" isDefault="true"/>`,
				acs("https://sp.example.test/post", 9, false),
			},
			want: "https://sp.example.test/post",
		},
		"http is not chosen, whatever it claims": {
			services: []string{
				fmt.Sprintf(`<md:AssertionConsumerService Binding="%s" Location="http://sp.example.test/plain" index="0" isDefault="true"/>`, BindingHTTPPost),
				acs("https://sp.example.test/secure", 5, false),
			},
			want: "https://sp.example.test/secure",
		},
		"no usable service at all is refused": {
			services: []string{
				fmt.Sprintf(`<md:AssertionConsumerService Binding="%s" Location="http://sp.example.test/plain" index="0"/>`, BindingHTTPPost),
			},
			wantErr: ErrNoUsableACS,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := metadataWith(`<md:SPSSODescriptor>` + strings.Join(tc.services, "") + `</md:SPSSODescriptor>`)
			sp, err := ParseSPMetadata([]byte(raw))

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if sp.ACSURL != tc.want {
				t.Errorf("chose %q, want %q", sp.ACSURL, tc.want)
			}
		})
	}
}

// An encryption key is not a signing key, and using one would fail at a login
// rather than here.
func TestAnEncryptionKeyIsNotTakenAsTheSigningCertificate(t *testing.T) {
	signing := testCertificateDER(t)
	encryption := testCertificateDER(t)

	raw := metadataWith(
		`<md:SPSSODescriptor>` +
			keyDescriptor("encryption", encryption) +
			keyDescriptor("signing", signing) +
			acs("https://sp.example.test/acs", 0, true) +
			`</md:SPSSODescriptor>`)

	sp, err := ParseSPMetadata([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	parsed, err := ParseCertificate(sp.CertificatePEM)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	want, _ := x509.ParseCertificate(signing)
	if !parsed.Equal(want) {
		t.Error("the encryption certificate was taken as the signing one")
	}
}

// A KeyDescriptor with no `use` serves both purposes, per the specification.
func TestAKeyWithNoUseIsAcceptedAsSigning(t *testing.T) {
	der := testCertificateDER(t)
	raw := metadataWith(
		`<md:SPSSODescriptor>` + keyDescriptor("", der) +
			acs("https://sp.example.test/acs", 0, true) + `</md:SPSSODescriptor>`)

	sp, err := ParseSPMetadata([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sp.CertificatePEM == "" {
		t.Error("a KeyDescriptor with no use was skipped")
	}
}

// Documents that must be refused, and the reason each one matters.
func TestMetadataThatMustBeRefused(t *testing.T) {
	der := testCertificateDER(t)
	usable := acs("https://sp.example.test/acs", 0, true)

	cases := map[string]string{
		"not XML at all": "this is not a document",

		"the root is not an EntityDescriptor": `<md:Nonsense xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"/>`,

		"no entityID": `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata">` +
			`<md:SPSSODescriptor>` + usable + `</md:SPSSODescriptor></md:EntityDescriptor>`,

		// An identity provider's metadata describes endpoints this service
		// would then treat as a partner's. Registering our own SSO URL as a
		// service provider's ACS is a loop at best.
		"an identity provider's metadata, not a service provider's": metadataWith(
			`<md:IDPSSODescriptor><md:SingleSignOnService Binding="` + BindingHTTPPost +
				`" Location="https://idp.example.test/sso"/></md:IDPSSODescriptor>`),

		"AuthnRequestsSigned with no certificate": metadataWith(
			`<md:SPSSODescriptor AuthnRequestsSigned="true">` + usable + `</md:SPSSODescriptor>`),

		"a certificate that is not base64": metadataWith(
			`<md:SPSSODescriptor><md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data>` +
				`<ds:X509Certificate>not base64 !!!</ds:X509Certificate>` +
				`</ds:X509Data></ds:KeyInfo></md:KeyDescriptor>` + usable + `</md:SPSSODescriptor>`),

		"base64 that is not a certificate": metadataWith(
			`<md:SPSSODescriptor><md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data>` +
				`<ds:X509Certificate>` + base64.StdEncoding.EncodeToString([]byte("hello")) +
				`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>` +
				usable + `</md:SPSSODescriptor>`),

		"an entity id past the bound": `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"` +
			` entityID="https://` + strings.Repeat("a", MaxEntityIDBytes) + `.test">` +
			`<md:SPSSODescriptor>` + usable + `</md:SPSSODescriptor></md:EntityDescriptor>`,
	}

	_ = der

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSPMetadata([]byte(raw)); err == nil {
				t.Error("accepted a document that must be refused")
			}
		})
	}
}

// The XML hardening applies here too, and this asserts it rather than assuming
// it — the parser is one call away from a different one that does not.
func TestSPMetadataGoesThroughTheSameXMLGate(t *testing.T) {
	usable := acs("https://sp.example.test/acs", 0, true)

	t.Run("a DTD is refused", func(t *testing.T) {
		raw := `<?xml version="1.0"?><!DOCTYPE EntityDescriptor [<!ENTITY x "y">]>` +
			metadataWith(`<md:SPSSODescriptor>`+usable+`</md:SPSSODescriptor>`)
		if _, err := ParseSPMetadata([]byte(raw)); !errors.Is(err, ErrDoctype) {
			t.Errorf("error %v, want ErrDoctype", err)
		}
	})

	t.Run("a document past the size bound is refused", func(t *testing.T) {
		padding := strings.Repeat("<md:Extensions/>", MaxDocumentBytes/16+1)
		raw := metadataWith(`<md:SPSSODescriptor>` + usable + `</md:SPSSODescriptor>` + padding)
		if _, err := ParseSPMetadata([]byte(raw)); !errors.Is(err, ErrTooLarge) {
			t.Errorf("error %v, want ErrTooLarge", err)
		}
	})
}

func truncateForTest(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

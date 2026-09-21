package saml

import (
	"errors"
	"strings"
	"testing"
)

// The AuthnRequest ID is an xsd:ID and is refused when it is not.
//
// It is not decoration. This value becomes the primary key of the pending row,
// a query parameter on the hosted login URL, and the `InResponseTo` a service
// provider reads back — and an unsigned AuthnRequest can be sent by anyone, so
// it is attacker-chosen on every one of those paths.
func TestAMalformedRequestIDIsRefused(t *testing.T) {
	refused := map[string]string{
		"empty":                 "",
		"a slash":               "_req/../etc",
		"a space":               "req 1",
		"an angle bracket":      "_<script>",
		"a quote":               `_"onload=`,
		"a colon":               "saml:_req1",
		"a leading digit":       "1req",
		"a leading hyphen":      "-req",
		"a leading dot":         ".req",
		"past the length bound": strings.Repeat("a", MaxRequestIDBytes+1),
		"a null byte":           "_req\x00",
		"a newline":             "_req\n",
	}

	for name, id := range refused {
		t.Run(name, func(t *testing.T) {
			if ValidRequestID(id) {
				t.Errorf("ValidRequestID(%q) accepted it", id)
			}

			// And the parser refuses the whole request, rather than accepting
			// it with an id nothing downstream can safely carry.
			raw := []byte(`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"` +
				` xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="` + id + `" Version="2.0">` +
				`<saml:Issuer>https://sp.example.test</saml:Issuer></samlp:AuthnRequest>`)
			if _, err := ParseAuthnRequest(raw); err == nil {
				t.Errorf("ParseAuthnRequest accepted an AuthnRequest with ID %q", id)
			}
		})
	}
}

func TestARealRequestIDIsAccepted(t *testing.T) {
	accepted := map[string]string{
		"a hex digest with an underscore": "_8e8dc5f69a98cc4c1ff3427e5ce34606fd672f91e6",
		"a prefixed id":                   "id-4f3c2b1a",
		"a dotted id":                     "sp.example.test.1",
		"a bare word":                     "request",
		"at the length bound":             strings.Repeat("a", MaxRequestIDBytes),
	}

	for name, id := range accepted {
		t.Run(name, func(t *testing.T) {
			if !ValidRequestID(id) {
				t.Fatalf("ValidRequestID(%q) refused a conforming id", id)
			}

			raw := []byte(`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"` +
				` xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="` + id + `" Version="2.0">` +
				`<saml:Issuer>https://sp.example.test</saml:Issuer></samlp:AuthnRequest>`)
			parsed, err := ParseAuthnRequest(raw)
			if err != nil {
				t.Fatalf("ParseAuthnRequest refused a conforming request: %v", err)
			}
			if parsed.ID != id {
				t.Errorf("parsed ID %q, want %q", parsed.ID, id)
			}
		})
	}
}

// The refusal is distinguishable, because "malformed" and "absent" are
// different things to whoever has to fix the service provider.
func TestAMalformedIDIsReportedAsSuchRatherThanAsAbsent(t *testing.T) {
	raw := []byte(`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"` +
		` xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_req/1" Version="2.0">` +
		`<saml:Issuer>https://sp.example.test</saml:Issuer></samlp:AuthnRequest>`)

	_, err := ParseAuthnRequest(raw)
	if !errors.Is(err, ErrMalformedID) {
		t.Errorf("error %v does not wrap ErrMalformedID", err)
	}
}

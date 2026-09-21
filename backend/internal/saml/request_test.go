package saml

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The bindings, and the bomb (P4-08 C-4, A-2).

const authnRequest = `<?xml version="1.0" encoding="UTF-8"?>
<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol"
              ID="_req1" Version="2.0" IssueInstant="2026-09-21T00:00:00Z"
              AssertionConsumerServiceURL="https://attacker.example.test/acs">
  <Issuer xmlns="urn:oasis:names:tc:SAML:2.0:assertion">https://sp.example.test</Issuer>
</AuthnRequest>`

// deflate encodes a document the way the HTTP-Redirect binding does.
func deflate(t *testing.T, raw string) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatalf("flate writer: %v", err)
	}
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestARedirectBindingRequestIsDecoded(t *testing.T) {
	raw, err := DecodeRedirect(deflate(t, authnRequest))
	if err != nil {
		t.Fatalf("a legitimate Redirect request was refused: %v", err)
	}
	if !strings.Contains(string(raw), "_req1") {
		t.Error("the decoded document is not the request that was encoded")
	}
}

func TestAPostBindingRequestIsDecoded(t *testing.T) {
	raw, err := DecodePost(base64.StdEncoding.EncodeToString([]byte(authnRequest)))
	if err != nil {
		t.Fatalf("a legitimate POST request was refused: %v", err)
	}
	if !strings.Contains(string(raw), "_req1") {
		t.Error("the decoded document is not the request that was encoded")
	}
}

// A-2: the attack the Redirect binding invites.
//
// The encoded parameter is small — a few kilobytes — and inflates to megabytes.
// A service that bounds the query string and not the inflation accepts it.
func TestADecompressionBombIsRefused(t *testing.T) {
	// 8 MB of zeroes: far past MaxDocumentBytes, and it compresses to a few
	// kilobytes, which is comfortably under MaxEncodedRequestBytes.
	bomb := deflate(t, strings.Repeat("\x00", 8*1024*1024))

	if len(bomb) > MaxEncodedRequestBytes {
		t.Fatalf("the bomb encodes to %d bytes, over the crude gate — the test is not exercising the inflated bound", len(bomb))
	}

	if _, err := DecodeRedirect(bomb); !errors.Is(err, ErrBomb) {
		t.Errorf("a decompression bomb gave %v, want ErrBomb", err)
	}
}

// And a document that is merely large, without compressing well, is refused by
// the same bound — so the defence is about the inflated size rather than about
// a compression ratio somebody could tune around.
func TestAnOversizedInflatedRequestIsRefused(t *testing.T) {
	big := deflate(t, "<a>"+strings.Repeat("xy", MaxDocumentBytes)+"</a>")
	if _, err := DecodeRedirect(big); err == nil {
		t.Error("a request inflating past the document bound was accepted")
	}
}

func TestAnOversizedEncodedParameterIsRefusedBeforeDecoding(t *testing.T) {
	huge := strings.Repeat("A", MaxEncodedRequestBytes+1)
	if _, err := DecodeRedirect(huge); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an oversized parameter gave %v, want ErrTooLarge", err)
	}
	if _, err := DecodePost(huge); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an oversized POST parameter gave %v, want ErrTooLarge", err)
	}
}

func TestBadEncodingIsRefused(t *testing.T) {
	if _, err := DecodeRedirect("not base64 at all !!!"); !errors.Is(err, ErrEncoding) {
		t.Error("a non-base64 Redirect parameter was accepted")
	}
	// Valid base64 that is not DEFLATE.
	if _, err := DecodeRedirect(base64.StdEncoding.EncodeToString([]byte("plain text"))); err == nil {
		t.Error("a Redirect parameter that is not DEFLATE was accepted")
	}
}

// The bindings change how a document arrives, never what it may be. An XXE
// document is refused on both.
func TestTheDocumentBoundsApplyOnBothBindings(t *testing.T) {
	const xxe = `<?xml version="1.0"?>
<!DOCTYPE foo [ <!ENTITY xxe SYSTEM "file:///etc/passwd"> ]>
<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="_x">&xxe;</AuthnRequest>`

	if _, err := DecodeRedirect(deflate(t, xxe)); err == nil {
		t.Error("the Redirect binding accepted an XXE document")
	}
	if _, err := DecodePost(base64.StdEncoding.EncodeToString([]byte(xxe))); err == nil {
		t.Error("the POST binding accepted an XXE document")
	}
}

// C-1: the struct this service acts on does not carry the ACS URL, so no code
// path can be steered by one.
func TestTheParsedRequestCarriesNoACSURL(t *testing.T) {
	raw, err := DecodePost(base64.StdEncoding.EncodeToString([]byte(authnRequest)))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	req, err := ParseAuthnRequest(raw)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if req.ID != "_req1" {
		t.Errorf("ID is %q, want _req1", req.ID)
	}
	if req.Issuer != "https://sp.example.test" {
		t.Errorf("Issuer is %q", req.Issuer)
	}

	// The document names an attacker's ACS URL. NO field of the parsed request
	// carries it — checked by walking the struct rather than by naming the
	// fields, so adding one that reads the request's ACS URL fails here
	// instead of quietly becoming a redirect the registration never approved.
	v := reflect.ValueOf(req)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		if strings.Contains(field.String(), "attacker") {
			t.Errorf("%s carries the request's own ACS URL %q — the registration decides where an assertion goes",
				v.Type().Field(i).Name, field.String())
		}
	}
}

// The inflated bound is about MEMORY, and a test that only checks the error
// cannot tell a bounded reader from an unbounded one: both refuse the bomb, one
// after allocating 512 KB and the other after allocating everything.
//
// So this measures. The threshold is generous — an order of magnitude below the
// inflated size and an order above the bound — because the property is "does
// not allocate proportionally to the attacker's chosen size", not an exact
// figure.
func TestABombIsRefusedWithoutInflatingIt(t *testing.T) {
	const inflated = 32 * 1024 * 1024
	bomb := deflate(t, strings.Repeat("A", inflated))
	if len(bomb) > MaxEncodedRequestBytes {
		t.Skipf("the bomb encodes to %d bytes, over the crude gate", len(bomb))
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	if _, err := DecodeRedirect(bomb); !errors.Is(err, ErrBomb) {
		t.Fatalf("the bomb gave %v, want ErrBomb", err)
	}

	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc

	if allocated > inflated/4 {
		t.Errorf("refusing a %d-byte bomb allocated %d bytes — the reader is inflating it before deciding",
			inflated, allocated)
	}
}

func TestARequestWithNoIDOrIssuerIsRefused(t *testing.T) {
	for name, doc := range map[string]string{
		"no ID": `<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol">` +
			`<Issuer>https://sp.example.test</Issuer></AuthnRequest>`,
		"no Issuer": `<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="_x"/>`,
		"not a request": `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="_x">` +
			`<Issuer>https://sp.example.test</Issuer></Response>`,
	} {
		if _, err := ParseAuthnRequest([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestARequestedAuthnContextIsRead(t *testing.T) {
	const doc = `<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="_x" ForceAuthn="true">
  <Issuer>https://sp.example.test</Issuer>
  <RequestedAuthnContext>
    <AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:MultiFactor</AuthnContextClassRef>
  </RequestedAuthnContext>
</AuthnRequest>`

	req, err := ParseAuthnRequest([]byte(doc))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	// Read, so C-6 can honour it. A field that is parsed and ignored would be
	// worse than one that is absent.
	if req.RequestedAuthnContext == "" {
		t.Error("a RequestedAuthnContext was not read — it cannot be honoured or refused")
	}
	if !req.ForceAuthn {
		t.Error("ForceAuthn was not read")
	}
}

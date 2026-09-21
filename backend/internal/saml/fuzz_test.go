package saml

import (
	"strings"
	"testing"
)

// Fuzzing the document reader (P4-07, docs/PLAN/11 § Security Testing).
//
// XML is the largest attack surface this service has, and a hand-written corpus
// only covers the attacks somebody thought of. `internal/signing` has the same
// arrangement for tokens and found real crashes in the JWT path.
//
// The contract under test is deliberately narrow, because a fuzzer cannot know
// what a SAML document means:
//
//	ReadDocument never panics, whatever it is given.
//	When it returns an error it returns NO bytes — a parser that hands back a
//	payload alongside an error is one whose caller will eventually use the
//	payload, which is how a refused document gets processed anyway.
//	When it succeeds it returns exactly what it was given, byte for byte,
//	because a caller has to hash and canonicalise what ARRIVED rather than
//	what a parser reconstructed.
func FuzzReadDocument(f *testing.F) {
	// The corpus starts from the shapes that matter: legitimate documents, the
	// attacks this package refuses, and the degenerate inputs that break naive
	// scanners.
	seeds := []string{
		wellFormedRequest,
		`<Assertion ID="_a"/>`,
		`<a>&amp;</a>`,
		`<?xml version="1.0"?><!DOCTYPE x><a/>`,
		`<!DOCTYPE lolz [<!ENTITY lol "lol">]><a>&lol;</a>`,
		`<a>&undefined;</a>`,
		`<a><!-- <!DOCTYPE x> --></a>`,
		`<a` + strings.Repeat("><b", 200) + `/>`,
		"",
		"\x00",
		"<",
		"<a",
		`<a x="`,
		`<a xmlns:p="urn:x"><p:b/></a>`,
		"\xef\xbb\xbf<a/>", // a byte-order mark
		`<a>` + strings.Repeat("x", 4096) + `</a>`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := ReadDocument(strings.NewReader(string(in)))

		if err != nil {
			if out != nil {
				t.Errorf("ReadDocument(%q) returned %d bytes with an error %v", in, len(out), err)
			}
			return
		}

		// Success must be byte-identical. Anything else means the caller would
		// verify a signature over a document that is not the one that arrived.
		if string(out) != string(in) {
			t.Errorf("ReadDocument(%q) returned %q — the bytes must be the ones that arrived", in, out)
		}
	})
}

// The same contract for the verifier, which parses a second time.
//
// No certificate is supplied, so every input is refused; the property under test
// is that it is refused rather than crashing, and that nothing comes back with
// the error.
func FuzzVerify(f *testing.F) {
	for _, s := range []string{
		wellFormedRequest,
		`<Assertion ID="_a"><Signature/></Assertion>`,
		`<Response ID="_r"><Assertion ID="_a"/></Response>`,
		`<Assertion/>`,
		"",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		v, err := Verify(in, "Assertion", nil)
		if err == nil {
			t.Errorf("Verify(%q) succeeded with no trust anchor", in)
		}
		if v.Element() != nil {
			t.Errorf("Verify(%q) returned an element with an error %v", in, err)
		}
	})
}

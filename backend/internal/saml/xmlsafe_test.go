package saml

import (
	"errors"
	"strings"
	"testing"
)

// The documents this package exists to refuse (P4-07 A-1, A-2).
//
// Each one is a real attack shape rather than an invented string, and each is
// paired with a legitimate document that must still be accepted — a gate that
// refuses everything passes every negative test and is useless.

const wellFormedRequest = `<?xml version="1.0" encoding="UTF-8"?>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"
                    xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"
                    ID="_abc123" Version="2.0" IssueInstant="2026-09-21T00:00:00Z"
                    AssertionConsumerServiceURL="https://sp.example.test/acs">
  <saml:Issuer>https://sp.example.test</saml:Issuer>
</samlp:AuthnRequest>`

func TestALegitimateDocumentIsAccepted(t *testing.T) {
	raw, err := ReadDocument(strings.NewReader(wellFormedRequest))
	if err != nil {
		t.Fatalf("a well-formed AuthnRequest was refused: %v", err)
	}
	if len(raw) != len(wellFormedRequest) {
		t.Errorf("returned %d bytes, want the %d that arrived — the caller must hash what was sent",
			len(raw), len(wellFormedRequest))
	}
}

// A-1: XXE. The classic file-read payload.
func TestAnExternalEntityIsRefused(t *testing.T) {
	const xxe = `<?xml version="1.0"?>
<!DOCTYPE foo [ <!ENTITY xxe SYSTEM "file:///etc/passwd"> ]>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">&xxe;</samlp:AuthnRequest>`

	raw, err := ReadDocument(strings.NewReader(xxe))
	if err == nil {
		t.Fatalf("an XXE document was accepted, returning %d bytes", len(raw))
	}
	if !errors.Is(err, ErrDoctype) && !errors.Is(err, ErrEntity) {
		t.Errorf("refused with %v — want the DTD or the entity named, so an operator can tell what arrived", err)
	}
	if raw != nil {
		t.Error("bytes came back with the error; a caller will eventually use them")
	}
}

// A-2: billion laughs. Small on the wire, enormous in memory.
func TestEntityExpansionIsRefused(t *testing.T) {
	const bomb = `<?xml version="1.0"?>
<!DOCTYPE lolz [
 <!ENTITY lol "lol">
 <!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
 <!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
 <!ENTITY lol4 "&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;">
]>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">&lol4;</samlp:AuthnRequest>`

	if _, err := ReadDocument(strings.NewReader(bomb)); err == nil {
		t.Fatal("a billion-laughs document was accepted")
	}
}

// A DTD with no entity reference at all.
//
// This test exists because of a mutation that survived: deleting the DOCTYPE
// refusal entirely left every other test green. The XXE and billion-laughs
// documents were being refused for their UNDEFINED ENTITIES, not for their
// DTD — so the DTD check was real code that nothing proved worked, which is
// this repository's recurring defect class in its purest form.
//
// A DOCTYPE with no entity reference can only be refused by the DOCTYPE check.
func TestADTDIsRefusedEvenWithNoEntityReference(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<!DOCTYPE samlp:AuthnRequest>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
  <Issuer>https://sp.example.test</Issuer>
</samlp:AuthnRequest>`

	_, err := ReadDocument(strings.NewReader(doc))
	if err == nil {
		t.Fatal("a document carrying a DTD was accepted — a SAML document has no legitimate reason to have one")
	}
	if !errors.Is(err, ErrDoctype) {
		t.Errorf("refused with %v, want ErrDoctype", err)
	}
}

// A custom entity without a DOCTYPE — the same attack with the declaration
// moved somewhere the document does not admit it.
func TestAnUndefinedEntityIsRefused(t *testing.T) {
	const doc = `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">&secret;</samlp:AuthnRequest>`

	_, err := ReadDocument(strings.NewReader(doc))
	if err == nil {
		t.Fatal("a document referencing an undefined entity was accepted")
	}
	if !errors.Is(err, ErrEntity) {
		t.Errorf("refused with %v, want ErrEntity", err)
	}
}

// The five predefined entities are legitimate XML and must still work: a SAML
// issuer or an attribute value routinely contains an ampersand.
func TestPredefinedEntitiesAreAccepted(t *testing.T) {
	const doc = `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">` +
		`<Issuer>Acme &amp; Partners &lt;eu&gt;</Issuer></samlp:AuthnRequest>`

	if _, err := ReadDocument(strings.NewReader(doc)); err != nil {
		t.Fatalf("a document using predefined entities was refused: %v", err)
	}
}

func TestAnOversizedDocumentIsRefusedBeforeParsing(t *testing.T) {
	// One byte over. The boundary is the interesting case: a bound that refuses
	// at the limit rejects legitimate documents, and one that reads past it has
	// already spent the memory.
	big := strings.Repeat("a", MaxDocumentBytes+1)
	if _, err := ReadDocument(strings.NewReader(big)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a document of %d bytes gave %v, want ErrTooLarge", len(big), err)
	}

	// Exactly at the limit, and well-formed, is accepted. Without this the test
	// above is satisfied by a function that refuses everything.
	padding := MaxDocumentBytes - len(wellFormedRequest)
	if padding > 0 {
		atLimit := strings.Replace(wellFormedRequest, "</samlp:AuthnRequest>",
			"<!--"+strings.Repeat("x", padding-7)+"--></samlp:AuthnRequest>", 1)
		if len(atLimit) != MaxDocumentBytes {
			t.Fatalf("test built a %d-byte document, wanted exactly %d", len(atLimit), MaxDocumentBytes)
		}
		if _, err := ReadDocument(strings.NewReader(atLimit)); err != nil {
			t.Errorf("a document exactly at the limit was refused: %v", err)
		}
	}
}

func TestExcessiveNestingIsRefused(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<root>`)
	for i := 0; i < MaxElementDepth+5; i++ {
		b.WriteString("<a>")
	}
	for i := 0; i < MaxElementDepth+5; i++ {
		b.WriteString("</a>")
	}
	b.WriteString(`</root>`)

	if _, err := ReadDocument(strings.NewReader(b.String())); !errors.Is(err, ErrTooDeep) {
		t.Errorf("a document %d levels deep gave %v, want ErrTooDeep", MaxElementDepth+6, err)
	}

	// And a normally nested document is not caught by the same rule.
	if _, err := ReadDocument(strings.NewReader(wellFormedRequest)); err != nil {
		t.Errorf("an ordinary document was refused by the depth bound: %v", err)
	}
}

// A DOCTYPE inside a comment is not a DTD. A byte-level scanner would refuse
// this, which is the false positive that gets a check disabled.
func TestADoctypeInsideACommentIsNotADTD(t *testing.T) {
	const doc = `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">` +
		`<!-- <!DOCTYPE foo> is mentioned here --></samlp:AuthnRequest>`

	if _, err := ReadDocument(strings.NewReader(doc)); err != nil {
		t.Errorf("a comment mentioning DOCTYPE was refused: %v — a check that cries wolf gets disabled", err)
	}
}

// The Go-specific attack the threat review (T4-6) named: a document that parses
// one way and re-serialises into something that parses another.
//
// It matters because a SAML document is parsed twice — once to verify a
// signature over a canonicalised form, once to read what it says. A document the
// two parses disagree about is signature wrapping achieved without touching the
// signature, and no amount of care about DTDs or entities prevents it.
func TestADocumentThatDoesNotRoundTripIsRefused(t *testing.T) {
	// A colon inside a local name. Go's parser silently rewrites `<x::Root/>`
	// into `<Root xmlns="x"/>` — the element is renamed and a namespace is
	// invented — so the document that was signed and the document that is read
	// are different documents.
	unstable := map[string]string{
		"colon in the root name":     `<x::Assertion ID="_a"/>`,
		"colon in a nested name":     `<Assertion ID="_a"><x::Subject></::Subject></Assertion>`,
		"colon in an attribute name": `<Assertion ::ID="_a"></Assertion>`,
	}

	// Refused — but NOT by the round-trip validator.
	//
	// The strict token walk above reaches these first: a colon in a local name
	// is a syntax error to Go's decoder, and every round-trip failure the
	// validator knows about is either that or a directive, which `checkSafe`
	// refuses outright. No document this package accepts can reach the
	// validator's own refusal, so no mutation can turn it red — the same honest
	// position as the empty-trust-store check in verify.go.
	//
	// It stays in the path because the threat review requires it and because
	// `checkSafe` is ours to change: if somebody ever relaxes the directive
	// rule or the strictness, this is what still stands between two disagreeing
	// parses and a signature over the wrong bytes.
	for name, doc := range unstable {
		if _, err := ReadDocument(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: accepted — a document Go's parser silently rewrites", name)
		}
	}

	// And an ordinary document still passes, so the validator has not simply
	// been turned into a refusal of everything.
	if _, err := ReadDocument(strings.NewReader(wellFormedRequest)); err != nil {
		t.Errorf("a well-formed document was refused by the round-trip check: %v", err)
	}
}

func TestMalformedMarkupIsRefused(t *testing.T) {
	for name, doc := range map[string]string{
		"unclosed element":   `<a><b></a>`,
		"stray close":        `</a>`,
		"unquoted attribute": `<a x=1/>`,
	} {
		if _, err := ReadDocument(strings.NewReader(doc)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

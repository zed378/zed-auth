// Package saml is the SAML 2.0 Identity Provider (P4-07).
//
// Specification: MEMORY/specs/P4-07-saml-idp-core.md.
//
// # Why this package exists at all, given a library
//
// `github.com/crewjam/saml` handles the XML and the digital signatures, and
// hand-rolling either is a documented path to compromise (the task card says so
// and it is right). What this package does NOT do is treat the library's
// behaviour as the security guarantee.
//
// SAML's two historic failure classes are XML parsing — XXE, entity expansion,
// DTD — and signature wrapping, where a document carries a valid signature over
// one element while a DIFFERENT element is the one consumed. Both fail
// silently: the request succeeds, the assertion is accepted, and nothing looks
// wrong. A library that stops them today is a library that might stop them
// tomorrow; a test that fails when OUR check is removed is a statement about
// this repository.
//
// So every property this service depends on is checked here, with a test that
// goes red when the check is deleted:
//
//	the document is bounded before it is parsed        (limits.go, this file)
//	no DTD, no external entities, no entity expansion  (this file)
//	the signed element IS the consumed element         (verify.go)
//	the audience names exactly this service provider   (verify.go)
//	the assertion has not been seen before             (replay.go)
package saml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	xmlvalidator "github.com/mattermost/xml-roundtrip-validator"
)

// MaxDocumentBytes bounds a SAML document before anything parses it.
//
// 512 KiB. A real AuthnRequest is a couple of kilobytes; a real assertion with
// attributes and a certificate chain is tens. The bound exists for the document
// that is not real: an entity-expansion attack is small on the wire and large in
// memory, and the cheapest place to refuse it is before the parser sees it.
//
// Applied with io.LimitReader plus an explicit overflow check, because
// LimitReader alone reports a truncated document as a well-formed short one —
// which is worse than a refusal, since a truncated AuthnRequest could parse.
const MaxDocumentBytes = 512 * 1024

// MaxElementDepth bounds nesting.
//
// Deep nesting is the other shape of the same attack: a document small enough to
// pass the byte bound can still be a hundred thousand elements deep, and a
// recursive walk over it is a stack exhaustion. 100 is far above any legitimate
// SAML document, which nests around a dozen levels at the extreme.
const MaxElementDepth = 100

var (
	// ErrTooLarge is a document over MaxDocumentBytes.
	ErrTooLarge = errors.New("saml: document exceeds the maximum size")

	// ErrDoctype is a document carrying a DTD.
	//
	// Refused outright rather than ignored. Go's encoding/xml does not resolve
	// external entities, so a DOCTYPE here is not exploitable the way it is in
	// a C parser — and a SAML document has no legitimate reason to carry one,
	// so its presence says the sender is either broken or probing. Refusing
	// says so; silently ignoring it means the next parser this document meets
	// gets to decide.
	ErrDoctype = errors.New("saml: document declares a DTD")

	// ErrEntity is a custom entity reference.
	ErrEntity = errors.New("saml: document references a custom entity")

	// ErrTooDeep is a document nested past MaxElementDepth.
	ErrTooDeep = errors.New("saml: document nesting is too deep")

	// ErrUnstable is a document Go's XML parser does not round-trip.
	//
	// The Go-specific SAML attack, and the one the Phase 4 threat review
	// (T4-6) singled out. `encoding/xml` can parse a document, re-serialise
	// it, and produce something that parses DIFFERENTLY — a namespace prefix
	// rebound, an attribute that moves. That matters here because a SAML
	// document is parsed TWICE: once to verify a signature over a
	// canonicalised form, and once to read what it says. If the two parses
	// disagree, the bytes that were signed are not the bytes that are read,
	// which is signature wrapping achieved without touching the signature.
	ErrUnstable = errors.New("saml: document does not survive a parse and re-serialise unchanged")
)

// ReadDocument reads a SAML document under every bound above.
//
// It returns the raw bytes, for a caller that must hash or canonicalise exactly
// what arrived. Parsing into a tree is the caller's next step; this is the gate
// in front of it.
//
// The order matters. Size is checked first because it is the cheapest, then the
// structural checks, and only then is the document handed on. A check that runs
// after the parser has already walked the document is not a defence.
func ReadDocument(r io.Reader) ([]byte, error) {
	// One byte over the limit, so a document exactly at the bound is accepted
	// and one past it is detectable. LimitReader alone cannot tell "ended" from
	// "was cut off".
	raw, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("saml: reading the document: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, ErrTooLarge
	}
	if err := checkSafe(raw); err != nil {
		return nil, err
	}

	// Round-trip stability, checked BEFORE any signature work.
	//
	// `mattermost/xml-roundtrip-validator` exists for exactly this failure and
	// nothing else. It is cheap, it is the one defence against a class Go's
	// own parser creates, and the threat review requires it — running it after
	// verification would be running it after the damage.
	if err := xmlvalidator.Validate(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnstable, err)
	}
	return raw, nil
}

// checkSafe walks the token stream and refuses what a SAML document must not
// contain.
//
// A token walk rather than a regular expression over the bytes: `<!DOCTYPE`
// inside a comment or a CDATA section is not a DTD, and a scanner that cannot
// tell the difference either produces false refusals or is trivially evaded by
// the encoding tricks this check exists to stop.
func checkSafe(raw []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(raw))

	// Strict is the default and is restated because it is load-bearing: a
	// permissive decoder accepts malformed markup that downstream parsers
	// interpret differently, which is how two parsers disagree about what a
	// document says.
	dec.Strict = true

	// No entity table. Go's decoder resolves only the five predefined XML
	// entities unless this map supplies more, and it never fetches an external
	// one — so leaving this nil is what makes XXE structurally impossible here
	// rather than merely disabled. An undefined entity is then an error, which
	// is how a billion-laughs document is refused: its expansions are custom
	// entities, and there is nothing to expand them with.
	dec.Entity = nil

	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// An undefined entity reaches here, and so does malformed markup.
			// Both are refusals; the message distinguishes them for an operator
			// without telling a caller which probe worked.
			if strings.Contains(err.Error(), "invalid character entity") ||
				strings.Contains(err.Error(), "unknown entity") {
				return ErrEntity
			}
			return fmt.Errorf("saml: parsing the document: %w", err)
		}

		switch t := tok.(type) {
		case xml.Directive:
			// Directives are <!...> — DOCTYPE among them. Anything else here is
			// equally unexpected in a SAML document.
			if isDoctype(t) {
				return ErrDoctype
			}
			return fmt.Errorf("%w: unexpected directive", ErrDoctype)
		case xml.StartElement:
			depth++
			if depth > MaxElementDepth {
				return ErrTooDeep
			}
		case xml.EndElement:
			depth--
		}
	}
}

// isDoctype reports whether a directive is a DOCTYPE declaration.
func isDoctype(d xml.Directive) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToUpper(string(d))), "DOCTYPE")
}

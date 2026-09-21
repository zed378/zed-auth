package saml

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Signature wrapping, and the one line that defeats it.
//
// The attack: take a document the identity provider legitimately signed, move
// that signed element somewhere a consumer does not look, and put an
// attacker-written element where the consumer DOES look. The document still
// contains a cryptographically valid signature. A verifier that asks "is there
// a valid signature in this document?" answers yes, and the consumer then reads
// the forged element.
//
// Every historic SAML break of this class has the same shape: the element that
// was VERIFIED and the element that was USED are two different elements, and
// nothing in the code ever compares them.
//
// So this package never returns a document. `Verify` returns the element the
// signature actually covers — the one goxmldsig validated — and the caller has
// nothing else to read. There is no API here that hands back the original tree,
// because an API that does will eventually be used.

var (
	// ErrNoSignature is a document with no signature where one is required.
	ErrNoSignature = errors.New("saml: the document is not signed")

	// ErrBadSignature is a signature that does not verify.
	ErrBadSignature = errors.New("saml: the signature does not verify")

	// ErrWrapped is a document whose signed element is not the element the
	// caller asked for — the signature-wrapping refusal.
	ErrWrapped = errors.New("saml: the signed element is not the element being consumed")
)

// Verified is an element whose signature this service checked.
//
// A distinct type rather than a bare *etree.Element, so a function that means
// "I have verified this" cannot be handed one that nobody verified. The
// compiler will not let an unverified element into a place that wants a
// Verified, which is a weaker guarantee than a proof and a stronger one than a
// comment.
type Verified struct {
	el *etree.Element
}

// Element is the verified element. Only reachable through Verify.
func (v Verified) Element() *etree.Element { return v.el }

// ID is the verified element's ID attribute, which is what a replay check
// records and what an audience check is scoped to.
func (v Verified) ID() string {
	if v.el == nil {
		return ""
	}
	return v.el.SelectAttrValue("ID", "")
}

// Verify reads a signed SAML document and returns the element the signature
// covers.
//
// `want` names the local element the caller intends to consume — "Assertion",
// "AuthnRequest", "Response". If the signature covers something else, this is a
// wrapping attempt and it is refused by name rather than by silence.
//
// The document is read under every bound in xmlsafe.go first. Parsing a
// document in order to decide whether to parse it is not a defence.
func Verify(raw []byte, want string, certs []*x509.Certificate) (Verified, error) {
	checked, err := ReadDocument(bytes.NewReader(raw))
	if err != nil {
		return Verified{}, err
	}

	doc := etree.NewDocument()
	// etree's own reader settings, belt to xmlsafe's braces. `Permissive` off
	// means it refuses what Go's decoder refuses; the entity map stays empty for
	// the same reason it does there.
	doc.ReadSettings = etree.ReadSettings{Permissive: false, Entity: map[string]string{}}
	if err := doc.ReadFromBytes(checked); err != nil {
		return Verified{}, fmt.Errorf("saml: parsing the document: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return Verified{}, fmt.Errorf("saml: the document has no root element")
	}

	if len(certs) == 0 {
		// Not "accept anything". A verification with no trust anchor verifies
		// nothing, and returning success here would be the most dangerous
		// possible default.
		return Verified{}, fmt.Errorf("%w: no certificate to verify against", ErrBadSignature)
	}

	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: certs})

	// Validate the element that is about to be CONSUMED, not the document.
	//
	// This is the whole wrapping defence, and it is a choice about what to pass
	// in rather than a check performed afterwards. goxmldsig refuses a
	// signature that does not reference the element it was handed — "Missing
	// signature referencing the top-level element" — so handing it the element
	// the caller will read makes a signature over anything else structurally
	// unusable. A verifier that passes the whole document instead is asking
	// "is there a valid signature somewhere in here", which is the question
	// every wrapping attack is built to answer yes.
	target := root
	if root.Tag != want {
		// The ordinary SAML shape: a signed Assertion inside a Response. The
		// FIRST matching element is taken deliberately — if an attacker has
		// prepended a forgery, this selects the forgery and the validation
		// below fails, which is the refusal we want. Selecting "the one that
		// happens to verify" would be the wrapping bug written in a loop.
		target = root.FindElement("//" + want)
		if target == nil {
			return Verified{}, fmt.Errorf("saml: the document contains no %q", want)
		}
	}

	signed, err := ctx.Validate(target)
	if err != nil {
		if hasSignature(root) {
			return Verified{}, fmt.Errorf("%w: %v", ErrBadSignature, err)
		}
		return Verified{}, ErrNoSignature
	}
	if signed == nil {
		return Verified{}, ErrBadSignature
	}

	// Belt and braces, and honestly labelled as such.
	//
	// goxmldsig already refuses a signature that does not reference `target`,
	// so no document this package can construct makes these two checks fire —
	// a mutation run could not turn either of them red, and the record says so
	// rather than claiming a caught mutation.
	//
	// They stay because they are the property this package promises, and the
	// promise should not be a comment about a dependency's current behaviour.
	// If goxmldsig ever relaxes that rule, these fail closed.
	if signed.Tag != want {
		return Verified{}, fmt.Errorf("%w: signature covers %q, consuming %q", ErrWrapped, signed.Tag, want)
	}
	signedID := signed.SelectAttrValue("ID", "")
	if signedID == "" {
		// Without an ID there is nothing for the replay check to record.
		return Verified{}, fmt.Errorf("%w: the signed %q carries no ID", ErrWrapped, want)
	}
	if targetID := target.SelectAttrValue("ID", ""); targetID != signedID {
		return Verified{}, fmt.Errorf("%w: consuming %q, the signature covers %q", ErrWrapped, targetID, signedID)
	}

	return Verified{el: signed}, nil
}

// hasSignature reports whether the document carries a Signature element at all.
//
// It separates "not signed" from "signed badly", which are different events for
// an operator: the first is usually a misconfigured service provider, the second
// is either a key rotation nobody coordinated or an attack.
func hasSignature(root *etree.Element) bool {
	if root.FindElement("./Signature") != nil || root.FindElement("./ds:Signature") != nil {
		return true
	}
	for _, child := range root.ChildElements() {
		if child.Tag == "Signature" {
			return true
		}
		if hasSignature(child) {
			return true
		}
	}
	return false
}

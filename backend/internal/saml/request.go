package saml

import (
	"compress/flate"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/beevik/etree"
)

// Reading an AuthnRequest off the wire (P4-08 C-4, A-2).
//
// SAML defines two bindings for getting a request to an identity provider, and
// they differ in exactly the way that matters here:
//
//	HTTP-POST     the document, base64-encoded, in a form field.
//	HTTP-Redirect the document, DEFLATE-compressed AND base64-encoded, in a
//	              query parameter.
//
// The Redirect binding is where the decompression bomb lives. A bound on the
// query string bounds the COMPRESSED bytes, and DEFLATE compresses a megabyte
// of zeroes into about a kilobyte — so a request that looks small enough to
// accept expands into one that is not. The Go SAML ecosystem has advisories for
// precisely this, and the Phase 4 threat review (T4-6) named it before any of
// this was written.
//
// So the inflated byte count is bounded explicitly, while inflating, and the
// reader stops rather than finishing and measuring afterwards.

// MaxEncodedRequestBytes bounds the encoded parameter before decoding.
//
// A first gate, cheap and crude: it stops an obviously hostile query string
// without spending any decompression on it. It is NOT the defence — that is the
// inflated bound below — and a comment saying so is worth more than the
// constant.
const MaxEncodedRequestBytes = 64 * 1024

var (
	// ErrEncoding is a parameter that is not valid base64, or not DEFLATE.
	ErrEncoding = errors.New("saml: the request is not encoded as the binding requires")

	// ErrBomb is a request that inflates past MaxDocumentBytes.
	ErrBomb = errors.New("saml: the compressed request expands beyond the maximum document size")
)

// DecodeRedirect reads an AuthnRequest from the HTTP-Redirect binding.
//
// base64 → DEFLATE → the same bounds every other document passes. The order is
// forced: nothing can be checked about the document until it is inflated, so
// the inflation itself has to be the thing that is bounded.
func DecodeRedirect(encoded string) ([]byte, error) {
	if len(encoded) > MaxEncodedRequestBytes {
		return nil, fmt.Errorf("%w: %d encoded bytes", ErrTooLarge, len(encoded))
	}

	// RawStdEncoding: the binding's value is URL-decoded by the time it reaches
	// here, and standard base64 with padding is what SAML emits. A permissive
	// decoder that accepted several alphabets would let one document have two
	// encodings, and two encodings of one document is how a signature is
	// computed over bytes nobody else reproduces.
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncoding, err)
	}

	// Raw DEFLATE, no zlib header — what the binding specifies.
	//
	// LimitReader at MaxDocumentBytes+1 is the whole defence: the reader stops
	// one byte past the bound, so a bomb is detected after inflating a bounded
	// amount rather than after inflating all of it. Reading first and measuring
	// afterwards is the version of this code that has already lost.
	reader := flate.NewReader(io.LimitReader(strings.NewReader(string(compressed)), int64(len(compressed))))
	defer func() { _ = reader.Close() }()

	raw, err := io.ReadAll(io.LimitReader(reader, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncoding, err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, ErrBomb
	}

	// And then every bound a POSTed document gets. The binding changes how a
	// document arrives, never what it is allowed to be.
	return ReadDocument(strings.NewReader(string(raw)))
}

// DecodePost reads an AuthnRequest from the HTTP-POST binding.
func DecodePost(encoded string) ([]byte, error) {
	if len(encoded) > MaxEncodedRequestBytes {
		return nil, fmt.Errorf("%w: %d encoded bytes", ErrTooLarge, len(encoded))
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncoding, err)
	}
	return ReadDocument(strings.NewReader(string(raw)))
}

// AuthnRequest is the part of a request this service acts on.
//
// Deliberately small. Every field a service provider could put in a request
// that this service does NOT read is a field that cannot be used to steer it —
// the ACS URL is the worked example, and it is absent from this struct on
// purpose (C-1).
type AuthnRequest struct {
	// ID is echoed as InResponseTo and recorded single-use.
	ID string

	// Issuer identifies the service provider. It is the lookup key for the
	// registration, and the registration is what decides everything else.
	Issuer string

	// RequestedAuthnContext, if the request asks for one. Honoured by stepping
	// up or answering NoAuthnContext — never ignored (C-6).
	RequestedAuthnContext string

	// ForceAuthn asks for a fresh authentication even with a live session.
	ForceAuthn bool
}

// ParseAuthnRequest reads the fields this service acts on.
//
// It deliberately does NOT return the ACS URL, the audience, or anything else a
// request might suggest about where its answer should go. Those come from the
// registration; a struct that carried them would eventually have one of them
// used.
func ParseAuthnRequest(raw []byte) (AuthnRequest, error) {
	doc := etree.NewDocument()
	doc.ReadSettings = etree.ReadSettings{Permissive: false, Entity: map[string]string{}}
	if err := doc.ReadFromBytes(raw); err != nil {
		return AuthnRequest{}, fmt.Errorf("saml: parsing the request: %w", err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "AuthnRequest" {
		return AuthnRequest{}, fmt.Errorf("saml: not an AuthnRequest")
	}

	out := AuthnRequest{
		ID:         root.SelectAttrValue("ID", ""),
		ForceAuthn: root.SelectAttrValue("ForceAuthn", "") == "true",
	}
	if out.ID == "" {
		// Without an ID there is nothing to record single-use and nothing to
		// echo, so a replayed request could not be told from a new one.
		return AuthnRequest{}, fmt.Errorf("saml: the AuthnRequest has no ID")
	}

	if issuer := root.FindElement("./Issuer"); issuer != nil {
		out.Issuer = strings.TrimSpace(issuer.Text())
	}
	if out.Issuer == "" {
		return AuthnRequest{}, fmt.Errorf("saml: the AuthnRequest names no Issuer")
	}

	if ctx := root.FindElement("./RequestedAuthnContext/AuthnContextClassRef"); ctx != nil {
		out.RequestedAuthnContext = strings.TrimSpace(ctx.Text())
	}

	return out, nil
}

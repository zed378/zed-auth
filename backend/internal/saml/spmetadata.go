package saml

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// Reading a service provider's own metadata (P4-09 step 2 and 3).
//
// An administrator registering a SAML integration is normally handed a
// metadata document by the other party. Typing its four fields out by hand is
// how an entity ID acquires a trailing space and a certificate loses a line,
// so this parses the document instead.
//
// # It is untrusted input, and more so than an AuthnRequest
//
// An AuthnRequest at least arrives through a registered service provider's
// flow. This arrives from an administrator's file picker, pasted out of an
// email, or fetched from a partner URL — and it is XML, which is the format
// every parser risk in P4-07 belongs to. So it goes through exactly the same
// gate: ReadDocument bounds the size and the depth, refuses DTDs and entities,
// and rejects a document that does not survive a parse and re-serialise
// unchanged.
//
// Nothing here is signed and nothing here is trusted to be correct. The parser
// reads what it needs, validates each field against what this service will
// actually accept, and refuses the document rather than storing a registration
// that would fail later, at a login, with an error naming none of this.

var (
	// ErrNotSPMetadata is a document that is not a service provider's metadata.
	ErrNotSPMetadata = errors.New("saml: not a service provider's metadata")

	// ErrNoUsableACS is metadata with no Assertion Consumer Service this
	// service can deliver to.
	ErrNoUsableACS = errors.New("saml: the metadata advertises no HTTPS HTTP-POST AssertionConsumerService")
)

// BindingHTTPPost is the only binding an assertion is delivered on.
const BindingHTTPPost = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"

// MaxEntityIDBytes bounds an entity ID.
//
// It becomes a unique index key, the Audience of every assertion issued to
// that service provider, and a name rendered in the console. 1024 is far above
// any real one — they are URLs — and far below a value that would make an
// index or a page awkward.
const MaxEntityIDBytes = 1024

// MaxACSURLBytes bounds an Assertion Consumer Service URL.
const MaxACSURLBytes = 2048

// SPDescriptor is what a registration needs from a service provider's metadata.
//
// Deliberately not the whole document. The fields absent here are as much a
// decision as the ones present: an AssertionConsumerService on a binding this
// service does not deliver on, a KeyDescriptor marked for encryption, an
// AttributeConsumingService requesting attributes — none can be honoured, and
// carrying them would invite a later change to start honouring one without
// anybody deciding to.
type SPDescriptor struct {
	// EntityID is the service provider's own name, compared exactly.
	EntityID string

	// ACSURL is where assertions may be POSTed. One, chosen by pickACS's
	// stated rules, because a registration holds one.
	ACSURL string

	// CertificatePEM is the signing certificate, if the metadata publishes
	// one. Empty is legitimate: a service provider that does not sign its
	// requests has no certificate to offer.
	CertificatePEM string

	// WantSignedRequests reflects AuthnRequestsSigned on the descriptor.
	//
	// Read from the metadata rather than asked of the administrator, because
	// the service provider is the party that knows whether it signs. It is
	// still only a default for the registration — the switch is settable — but
	// a default that comes from the other side is right far more often than
	// one that comes from a checkbox nobody understood.
	WantSignedRequests bool
}

// ParseSPMetadata reads a service provider's metadata document.
func ParseSPMetadata(raw []byte) (SPDescriptor, error) {
	safe, err := ReadDocument(bytes.NewReader(raw))
	if err != nil {
		return SPDescriptor{}, err
	}

	doc := etree.NewDocument()
	doc.ReadSettings = etree.ReadSettings{Permissive: false, Entity: map[string]string{}}
	if err := doc.ReadFromBytes(safe); err != nil {
		return SPDescriptor{}, fmt.Errorf("saml: parsing the metadata: %w", err)
	}

	root := doc.Root()
	if root == nil || root.Tag != "EntityDescriptor" {
		return SPDescriptor{}, fmt.Errorf("%w: the root element is not an EntityDescriptor", ErrNotSPMetadata)
	}

	out := SPDescriptor{EntityID: strings.TrimSpace(root.SelectAttrValue("entityID", ""))}
	if out.EntityID == "" {
		return SPDescriptor{}, fmt.Errorf("%w: it names no entityID", ErrNotSPMetadata)
	}
	if len(out.EntityID) > MaxEntityIDBytes {
		return SPDescriptor{}, fmt.Errorf("%w: the entityID is longer than %d bytes",
			ErrNotSPMetadata, MaxEntityIDBytes)
	}

	// The SP descriptor, not the IdP one. A document can carry both — some
	// products publish one file for a service that is both — and reading the
	// wrong one would register this service's own endpoints as a partner's.
	sp := root.FindElement("./SPSSODescriptor")
	if sp == nil {
		return SPDescriptor{}, fmt.Errorf(
			"%w: it has no SPSSODescriptor, so it does not describe a service provider", ErrNotSPMetadata)
	}

	out.WantSignedRequests = sp.SelectAttrValue("AuthnRequestsSigned", "") == "true"

	acs, err := pickACS(sp)
	if err != nil {
		return SPDescriptor{}, err
	}
	out.ACSURL = acs

	certificate, err := pickSigningCertificate(sp)
	if err != nil {
		return SPDescriptor{}, err
	}
	out.CertificatePEM = certificate

	// A service provider that says it signs its requests and publishes no
	// certificate has published a contradiction. Refused here rather than
	// stored: migration 040's CHECK refuses it too, and a constraint violation
	// surfacing from an INSERT names the column rather than the half of the
	// document that was wrong.
	if out.WantSignedRequests && out.CertificatePEM == "" {
		return SPDescriptor{}, fmt.Errorf(
			"%w: it sets AuthnRequestsSigned and publishes no signing certificate", ErrNotSPMetadata)
	}

	return out, nil
}

// pickACS chooses the one Assertion Consumer Service this service will use.
//
// A registration holds one URL, so a document offering several needs a rule —
// and the rule must be stated rather than being "whichever the parser returned
// first", which is a choice made by a library's iteration order.
//
//  1. Only HTTP-POST. It is the only binding assertions are delivered on, and
//     picking a URL this service will never POST to registers an integration
//     that cannot work.
//  2. Only https. Migration 040's CHECK enforces it, and an assertion is a
//     bearer credential: delivering one over cleartext hands it to the network.
//  3. isDefault="true" wins, then the lowest index. That is the
//     specification's own precedence, so a service provider that expressed a
//     preference gets the one it asked for.
func pickACS(sp *etree.Element) (string, error) {
	type candidate struct {
		location  string
		index     int
		isDefault bool
	}

	var usable []candidate
	for _, el := range sp.FindElements("./AssertionConsumerService") {
		if el.SelectAttrValue("Binding", "") != BindingHTTPPost {
			continue
		}
		location := strings.TrimSpace(el.SelectAttrValue("Location", ""))
		if !strings.HasPrefix(location, "https://") || len(location) > MaxACSURLBytes {
			continue
		}
		index, err := strconv.Atoi(el.SelectAttrValue("index", ""))
		if err != nil {
			// Absent, or not a number. Sorted last among equals rather than
			// discarded: the URL is usable and the attribute is a preference.
			index = 1 << 30
		}
		usable = append(usable, candidate{
			location:  location,
			index:     index,
			isDefault: el.SelectAttrValue("isDefault", "") == "true",
		})
	}

	if len(usable) == 0 {
		return "", ErrNoUsableACS
	}

	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].isDefault != usable[j].isDefault {
			return usable[i].isDefault
		}
		return usable[i].index < usable[j].index
	})
	return usable[0].location, nil
}

// pickSigningCertificate reads the certificate a service provider signs with.
//
// use="signing", or no use at all — which the specification says means the key
// serves both purposes. A use="encryption" key is skipped: this service does
// not encrypt assertions, and verifying a signature against an encryption key
// fails at a login rather than here.
//
// When several signing keys are published — which is what a service provider
// mid-rotation looks like — the first usable one wins and the rest are ignored.
// A registration holds one certificate, so that is a limit worth stating: an
// administrator whose partner is rotating can paste the other one in.
func pickSigningCertificate(sp *etree.Element) (string, error) {
	for _, key := range sp.FindElements("./KeyDescriptor") {
		switch key.SelectAttrValue("use", "") {
		case "", "signing":
		default:
			continue
		}

		el := key.FindElement(".//X509Certificate")
		if el == nil {
			continue
		}

		// Whitespace is expected: metadata wraps base64 across lines.
		encoded := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
				return -1
			}
			return r
		}, el.Text())
		if encoded == "" {
			continue
		}

		der, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("%w: a KeyDescriptor's X509Certificate is not base64", ErrNotSPMetadata)
		}

		// Parsed, not merely decoded. Storing bytes that are not a certificate
		// produces a registration that fails at the first signed request, with
		// an error naming the signature rather than the paste that caused it.
		if _, err := x509.ParseCertificate(der); err != nil {
			return "", fmt.Errorf("%w: a KeyDescriptor's X509Certificate does not parse: %v",
				ErrNotSPMetadata, err)
		}

		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
	}
	return "", nil
}

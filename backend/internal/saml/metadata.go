package saml

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/beevik/etree"
)

// IdP metadata (P4-08).
//
// A service provider configures itself from this document: it learns the entity
// ID to expect as an assertion's Issuer, the URL to send an AuthnRequest to, and
// the certificate to verify assertions against. Everything in it is a promise
// somebody else will act on for years, which shapes two decisions:
//
// **It is not published until the endpoints exist.** This moved out of `P4-07`
// for that reason: metadata advertising a SingleSignOnService that answers 404
// is a promise rather than a capability, and a service provider's only way to
// discover the problem is a failed login. The repository already refuses to let
// the public site describe unshipped capabilities; a protocol surface deserves
// the same reading.
//
// **It advertises no SingleLogoutService**, because there is none. `P4-08`
// decided to document the absence rather than ship a front-channel logout that
// tells a user they are signed out of applications they are not — see
// `docs/IDENTITY-PROTOCOL/04`. Advertising an endpoint that answers
// `RequestDenied` would be worse than advertising nothing: a service provider
// would build a logout button on it.

// NameIDFormatPersistent is the only format this service advertises.
//
// Never email (threat review T4-6, C-5). A service provider keyed on an email
// address hands a deactivated user's account to whoever is issued that address
// next, and the same address across two service providers lets them correlate a
// user who never agreed to be correlated.
const NameIDFormatPersistent = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"

// Endpoints are the URLs a service provider is told to use.
type Endpoints struct {
	// SSORedirect and SSOPost are the same logical endpoint on the two
	// bindings. Both are advertised because a service provider chooses.
	SSORedirect string
	SSOPost     string
}

// Metadata builds the IdP's `EntityDescriptor`.
//
// The certificates are the ones assertions are actually signed with, taken
// from the signing keys rather than passed in separately — a metadata document
// advertising a certificate the service does not sign with is a failure only
// the service provider can see.
//
// # Why more than one (P4-09)
//
// It used to publish exactly the key that signs now, and that made a SAML key
// rotation an outage waiting on other people. A service provider pins the
// certificate it read here; the moment `keyctl -purpose saml rotate` promoted
// a new key, every service provider still pinning the old one rejected every
// assertion. The overlap that makes an OIDC rotation safe — publish the key,
// let consumers fetch it, then sign with it — had no counterpart, because
// there was nowhere to publish a key that was not yet signing.
//
// There is now. `current` first, then `next`, so a service provider that
// refreshes its metadata on any schedule at all has already trusted the new
// certificate before it is used — which is what the SAML specification
// expects a multi-KeyDescriptor descriptor to be for.
//
// `previous` is deliberately NOT published. This service is the issuer: it
// signs with exactly one key, and a demoted key verifies nothing anyone is
// asking about. Publishing it would keep a key trusted after it stopped being
// used, which is the opposite of what retiring one is for.
// Certificates rather than keys, because publication needs no private half —
// and asking for one would make a key that is published-but-not-yet-signing
// impossible to publish, which is the whole point of the overlap. The caller
// is responsible for the first entry being the certificate of the key that
// signs now; samlapi.CachedKeys.Published takes it from the same accessor the
// issuing path uses, so the two cannot disagree.
func Metadata(entityID string, certs []*x509.Certificate, endpoints Endpoints) (*etree.Element, error) {
	if entityID == "" {
		return nil, fmt.Errorf("saml: metadata needs an entity id")
	}
	if endpoints.SSORedirect == "" || endpoints.SSOPost == "" {
		return nil, fmt.Errorf("saml: metadata needs both SSO endpoints")
	}
	if len(certs) == 0 {
		// Metadata with no certificate is a document a service provider cannot
		// configure itself from. Refused rather than published empty.
		return nil, fmt.Errorf("saml: metadata needs at least one certificate")
	}

	entity := etree.NewElement("EntityDescriptor")
	entity.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:metadata")
	entity.CreateAttr("xmlns:ds", "http://www.w3.org/2000/09/xmldsig#")
	entity.CreateAttr("entityID", entityID)

	idp := entity.CreateElement("IDPSSODescriptor")
	idp.CreateAttr("protocolSupportEnumeration", "urn:oasis:names:tc:SAML:2.0:protocol")

	// Assertions are always signed, so a service provider is told to require it.
	// Saying `false` here — or omitting it — invites an integration that accepts
	// unsigned assertions, which is somebody else's vulnerability caused by our
	// document.
	idp.CreateAttr("WantAuthnRequestsSigned", "false")

	// --- the signing certificates ------------------------------------------
	//
	// One KeyDescriptor each, in the order given: the key that signs now, then
	// the one that will. A service provider is expected to trust all of them.
	for _, cert := range certs {
		descriptor := idp.CreateElement("KeyDescriptor")
		descriptor.CreateAttr("use", "signing")
		keyInfo := descriptor.CreateElement("ds:KeyInfo")
		x509Data := keyInfo.CreateElement("ds:X509Data")
		x509Data.CreateElement("ds:X509Certificate").
			SetText(base64.StdEncoding.EncodeToString(cert.Raw))
	}

	// --- what this service will assert about a subject ----------------------
	idp.CreateElement("NameIDFormat").SetText(NameIDFormatPersistent)

	// --- where to send a request -------------------------------------------
	redirect := idp.CreateElement("SingleSignOnService")
	redirect.CreateAttr("Binding", "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect")
	redirect.CreateAttr("Location", endpoints.SSORedirect)

	post := idp.CreateElement("SingleSignOnService")
	post.CreateAttr("Binding", "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST")
	post.CreateAttr("Location", endpoints.SSOPost)

	// No SingleLogoutService. Deliberate, and the package comment says why.

	return entity, nil
}

// Serialise writes an element as a standalone XML document.
//
// Here rather than in each caller, so the declaration and the encoding are the
// same everywhere a SAML document leaves this service.
func Serialise(el *etree.Element) ([]byte, error) {
	if el == nil {
		return nil, fmt.Errorf("saml: nothing to serialise")
	}
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	doc.SetRoot(el.Copy())
	raw, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("saml: serialising: %w", err)
	}
	return raw, nil
}

package saml

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Issuing an assertion (P4-07 F-5, F-6).
//
// The assertion is what a service provider consumes instead of talking to this
// service, so everything it says has to be true at the moment it is read, and
// nothing it says can be more than the service provider was registered to
// receive.

// AssertionLifetime is how long an issued assertion is valid.
//
// Five minutes. It exists to be carried from this service to a service provider
// through a browser redirect, which takes seconds — the window is for clock
// skew and a slow network, not for storage. A longer life is a longer replay
// window for a credential that is, by construction, handed to a third party.
const AssertionLifetime = 5 * time.Minute

// SubjectConfirmationBearer is the only confirmation method this service issues.
//
// Holder-of-key would be stronger and needs the service provider to hold a key
// this service can name, which no registration here carries. Saying `bearer`
// plainly is better than implying a stronger binding than exists.
const SubjectConfirmationBearer = "urn:oasis:names:tc:SAML:2.0:cm:bearer"

// Subject describes who the assertion is about and what it may say.
type Subject struct {
	// NameID is the identifier the service provider knows this user by.
	NameID string

	// NameIDFormat is the format URI. Unspecified is the safe default: a
	// service provider that asked for an email format and receives a UUID has
	// been told something untrue about the value.
	NameIDFormat string

	// Attributes are every attribute this service COULD release. What is
	// actually released is the intersection with the service provider's
	// registration — see Release.
	Attributes map[string][]string

	// SessionIndex ties the assertion to the session behind it, so a Single
	// Logout (P4-08) can name what it is ending.
	SessionIndex string

	// AuthnInstant is when the user actually authenticated — not when this
	// assertion was built. A service provider deciding whether a sign-in is
	// recent enough is asking about the first, and reporting the second would
	// make every assertion look freshly authenticated.
	AuthnInstant time.Time

	// AuthnContextClassRef says HOW they authenticated.
	AuthnContextClassRef string
}

// ServiceProvider is the registration an assertion is issued against.
type ServiceProvider struct {
	EntityID string
	ACSURL   string

	// Release is the closed set of attribute names this service provider
	// receives. Empty means the NameID and nothing else, which is the correct
	// answer for a party that has not asked for anything — the same discipline
	// /oauth/userinfo applies to scopes.
	Release []string
}

// Issue builds and signs an assertion for one service provider.
//
// `inResponseTo` is the AuthnRequest's ID for an SP-initiated flow, and empty
// for IdP-initiated. It is echoed so a service provider can correlate the
// assertion with the request it made — the correlation IdP-initiated flows
// structurally lack, which is why `P4-08` makes that mode opt-in.
func Issue(
	key *SigningKey, issuer string, sp ServiceProvider, subject Subject, inResponseTo string, now time.Time,
) (*etree.Element, error) {
	if key == nil {
		return nil, fmt.Errorf("saml: no signing key")
	}
	if sp.EntityID == "" || sp.ACSURL == "" {
		return nil, fmt.Errorf("saml: the service provider has no entity id or ACS url")
	}
	if subject.NameID == "" {
		return nil, fmt.Errorf("saml: the assertion has no subject")
	}

	id, err := assertionID()
	if err != nil {
		return nil, err
	}

	el := etree.NewElement("Assertion")
	el.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:assertion")
	el.CreateAttr("ID", id)
	el.CreateAttr("Version", "2.0")
	el.CreateAttr("IssueInstant", now.UTC().Format(time.RFC3339))
	el.CreateElement("Issuer").SetText(issuer)

	// --- subject ------------------------------------------------------------
	subj := el.CreateElement("Subject")
	nameID := subj.CreateElement("NameID")
	if subject.NameIDFormat != "" {
		nameID.CreateAttr("Format", subject.NameIDFormat)
	}
	nameID.SetText(subject.NameID)

	confirmation := subj.CreateElement("SubjectConfirmation")
	confirmation.CreateAttr("Method", SubjectConfirmationBearer)
	data := confirmation.CreateElement("SubjectConfirmationData")
	data.CreateAttr("NotOnOrAfter", now.Add(AssertionLifetime).UTC().Format(time.RFC3339))
	// Recipient is the registered ACS URL, not one the request asked for. An
	// assertion that names wherever the request pointed is an open redirect
	// with a signature on it.
	data.CreateAttr("Recipient", sp.ACSURL)
	if inResponseTo != "" {
		data.CreateAttr("InResponseTo", inResponseTo)
	}

	// --- conditions ---------------------------------------------------------
	//
	// Built by the same package that later enforces them, so the window this
	// service issues and the window it accepts cannot drift apart.
	el.AddChild(Conditions(sp.EntityID, now, AssertionLifetime))

	// --- authentication statement -------------------------------------------
	authn := el.CreateElement("AuthnStatement")
	instant := subject.AuthnInstant
	if instant.IsZero() {
		instant = now
	}
	authn.CreateAttr("AuthnInstant", instant.UTC().Format(time.RFC3339))
	if subject.SessionIndex != "" {
		authn.CreateAttr("SessionIndex", subject.SessionIndex)
	}
	if subject.AuthnContextClassRef != "" {
		authn.CreateElement("AuthnContext").
			CreateElement("AuthnContextClassRef").
			SetText(subject.AuthnContextClassRef)
	}

	// --- attributes ---------------------------------------------------------
	if released := Release(subject.Attributes, sp.Release); len(released) > 0 {
		statement := el.CreateElement("AttributeStatement")
		// Sorted, so two assertions for the same user are byte-comparable and
		// a diff between them means something changed rather than that a map
		// iterated differently.
		names := make([]string, 0, len(released))
		for name := range released {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			attr := statement.CreateElement("Attribute")
			attr.CreateAttr("Name", name)
			for _, value := range released[name] {
				attr.CreateElement("AttributeValue").SetText(value)
			}
		}
	}

	signed, err := dsig.NewDefaultSigningContext(key).SignEnveloped(el)
	if err != nil {
		return nil, fmt.Errorf("saml: signing the assertion: %w", err)
	}
	return signed, nil
}

// Release narrows what this service knows to what a service provider was
// registered to receive (F-6).
//
// An allow list, never a deny list. A new attribute added to a user reaches no
// service provider until somebody registers it, which is the direction that
// fails safe: the alternative leaks every future attribute to every existing
// integration on the day it is introduced.
func Release(available map[string][]string, allowed []string) map[string][]string {
	if len(available) == 0 || len(allowed) == 0 {
		return nil
	}
	out := make(map[string][]string, len(allowed))
	for _, name := range allowed {
		if values, ok := available[name]; ok && len(values) > 0 {
			out[name] = append([]string(nil), values...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// assertionID returns an identifier for a new assertion.
//
// An `_` prefix because the xsd:ID type an assertion's ID has may not begin
// with a digit, and a hex string frequently does — a constraint that produces
// documents some parsers accept and others reject, which is the worst kind of
// intermittent.
func assertionID() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("saml: generating an assertion id: %w", err)
	}
	return "_" + hex.EncodeToString(b), nil
}

package application

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/saml"
)

// The SAML half of an application (P4-09).
//
// A `saml` application and its service provider registration are created and
// changed together, in one call and one transaction. The alternative — a
// second call to a sub-resource — leaves a window in which the application
// exists and no `AuthnRequest` can be matched to it, and an administrator who
// stops halfway has produced something that looks registered and is not.
//
// # Where the fields come from
//
// Normally from the other party's metadata document, which is parsed here
// rather than in the console: it is untrusted XML, and a client that parsed it
// would be a second parser to keep as careful as the first. The manual fields
// exist for a service provider that publishes no metadata, which is common
// enough among older products to be worth supporting.

// samlInputFrom reads a registration out of a request body.
//
// Metadata or fields, never both. A body carrying both is a caller who does
// not know which one this service will honour, and answering by precedence
// would make that a guess the caller never sees.
func samlInputFrom(in *api.SamlRegistrationInput) (saml.Managed, error) {
	if in == nil {
		return saml.Managed{}, nil
	}

	metadata := strings.TrimSpace(deref(in.MetadataXml))
	manual := in.EntityId != nil || in.AcsUrl != nil || in.Certificate != nil

	if metadata != "" && manual {
		return saml.Managed{}, management.Fault{
			Class:   management.Invalid,
			Message: "Supply metadata or the individual fields, not both.",
			Details: []api.ErrorDetail{{Field: "saml.metadata_xml", Issue: "cannot be combined with entity_id, acs_url or certificate"}},
			Reason:  "saml registration supplied twice",
		}
	}

	var out saml.Managed

	if metadata != "" {
		parsed, err := saml.ParseSPMetadata([]byte(metadata))
		if err != nil {
			return saml.Managed{}, metadataFault(err)
		}
		out = saml.Managed{
			EntityID:           parsed.EntityID,
			ACSURL:             parsed.ACSURL,
			CertificatePEM:     parsed.CertificatePEM,
			WantSignedRequests: parsed.WantSignedRequests,
		}
	} else {
		out = saml.Managed{
			EntityID:       strings.TrimSpace(deref(in.EntityId)),
			ACSURL:         strings.TrimSpace(deref(in.AcsUrl)),
			CertificatePEM: deref(in.Certificate),
		}
		if in.WantSignedRequests != nil {
			out.WantSignedRequests = *in.WantSignedRequests
		}
	}

	// These two are the caller's either way. The metadata says what the
	// service provider signs; it does not say what this organization chooses
	// to release to it, and it cannot say whether somebody here decided to
	// accept unsolicited logins.
	if in.AttributeRelease != nil {
		out.AttributeRelease = *in.AttributeRelease
	}
	if out.AttributeRelease == nil {
		out.AttributeRelease = []string{}
	}
	if in.AllowIdpInitiated != nil {
		out.AllowIdPInitiated = *in.AllowIdpInitiated
	}
	// An explicit want_signed_requests overrides what the metadata said, so an
	// administrator can turn it off for a service provider whose document
	// claims it signs and whose requests do not.
	if metadata != "" && in.WantSignedRequests != nil {
		out.WantSignedRequests = *in.WantSignedRequests
	}

	return out, nil
}

// requireSamlConsistency refuses a registration on the wrong type, or a `saml`
// application with none.
//
// Both directions, because both are a caller believing something that is not
// true. A registration sent with `type: web` would be silently discarded; a
// `saml` application created without one would exist and match no request.
func requireSamlConsistency(kind client.Type, in *api.SamlRegistrationInput) error {
	if kind == client.TypeSAML && in == nil {
		return management.Fault{
			Class:   management.Invalid,
			Message: "A SAML application needs its service provider registration.",
			Details: []api.ErrorDetail{{Field: "saml", Issue: "is required when type is saml"}},
			Reason:  "saml application without a registration",
		}
	}
	if kind != client.TypeSAML && in != nil {
		return management.Fault{
			Class:   management.Invalid,
			Message: fmt.Sprintf("An application of type %s has no SAML registration.", kind),
			Details: []api.ErrorDetail{{Field: "saml", Issue: "is only valid when type is saml"}},
			Reason:  "saml registration on a non-saml application",
		}
	}
	return nil
}

// renderSaml turns a stored registration into what the API returns.
//
// The certificate's expiry is derived here rather than stored, so it cannot go
// stale against the certificate it describes — a column would have to be
// rewritten every time the certificate changed, and the one time it was not
// would be a warning that never fired.
func renderSaml(m saml.Managed, now time.Time) (*api.SamlRegistration, error) {
	out := &api.SamlRegistration{
		EntityId:           m.EntityID,
		AcsUrl:             m.ACSURL,
		AttributeRelease:   m.AttributeRelease,
		WantSignedRequests: m.WantSignedRequests,
		AllowIdpInitiated:  m.AllowIdPInitiated,
	}
	if out.AttributeRelease == nil {
		out.AttributeRelease = []string{}
	}
	if m.CertificatePEM != "" {
		certificate := m.CertificatePEM
		out.Certificate = &certificate

		expiry, err := m.CertificateExpiry()
		if err != nil {
			// Stored and unparseable. Reported rather than hidden: it means
			// every signed request from this service provider is already being
			// refused, and a rendering that omitted the field would leave the
			// console showing a healthy registration.
			return nil, management.Fault{
				Class:   management.Internal,
				Message: "The stored certificate for this service provider cannot be read.",
				Reason:  "unparseable stored saml certificate",
			}
		}
		if !expiry.IsZero() {
			expiresAt := expiry
			soon := !expiry.After(now.Add(saml.CertificateWarningWindow))
			out.CertificateExpiresAt = &expiresAt
			out.CertificateExpiresSoon = &soon
		}
	}
	return out, nil
}

// metadataFault turns a parser refusal into an answer a caller can act on.
//
// The parser's errors already name what was wrong with the document, and
// passing them through is the point: "the metadata advertises no HTTPS
// HTTP-POST AssertionConsumerService" tells an administrator what to go and
// ask for, where "invalid metadata" tells them to try again.
func metadataFault(err error) error {
	switch {
	case errors.Is(err, saml.ErrTooLarge),
		errors.Is(err, saml.ErrDoctype),
		errors.Is(err, saml.ErrEntity),
		errors.Is(err, saml.ErrTooDeep),
		errors.Is(err, saml.ErrUnstable):
		// The hardening refused it. The reason is deliberately not echoed in
		// full: these fire on documents that are malformed in ways an attacker
		// chooses, and a detailed report is a probe result.
		return management.Fault{
			Class:   management.Invalid,
			Message: "That metadata document was refused before it was read.",
			Details: []api.ErrorDetail{{Field: "saml.metadata_xml", Issue: "is not a document this service will parse"}},
			Reason:  "saml metadata refused by the xml gate",
		}
	default:
		return management.Fault{
			Class:   management.Invalid,
			Message: "That metadata document could not be used.",
			Details: []api.ErrorDetail{{Field: "saml.metadata_xml", Issue: cleanSaml(err)}},
			Reason:  "saml metadata unusable",
		}
	}
}

// samlFault turns a registration store error into an answer.
func samlFault(err error) error {
	var field saml.FieldError
	switch {
	case errors.As(err, &field):
		return management.Fault{
			Class:   management.Invalid,
			Message: "That SAML registration could not be saved.",
			Details: []api.ErrorDetail{{Field: "saml." + field.Field, Issue: field.Issue}},
			Reason:  "invalid saml registration",
		}
	case errors.Is(err, saml.ErrEntityIDTaken):
		return management.Fault{
			Class: management.Conflict,
			Message: "Another application is already registered with that entity ID. " +
				"An entity ID is a global name in SAML, so it identifies one service provider across the whole instance.",
			Details: []api.ErrorDetail{{Field: "saml.entity_id", Issue: "is already registered"}},
			Reason:  "duplicate saml entity id",
		}
	case errors.Is(err, saml.ErrNoRegistration):
		return management.Fault{
			Class:   management.NotFound,
			Message: "That application has no SAML registration.",
			Reason:  "saml registration not found",
		}
	default:
		return err
	}
}

// cleanSaml strips the package sentinel, leaving the part that says what was
// wrong with the document.
func cleanSaml(err error) string {
	message := err.Error()
	if _, rest, ok := strings.Cut(message, ": "); ok && strings.HasPrefix(message, "saml: ") {
		return rest
	}
	return message
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// requireSamlUpdate refuses a registration sent to an application that has
// none.
//
// The other direction — a `saml` application updated without one — is
// deliberately allowed: an omitted field means "leave it alone" everywhere
// else in this body, and making `saml` the exception would mean a rename could
// not be sent without restating the whole registration.
func requireSamlUpdate(isSAML bool, in *api.SamlRegistrationInput) error {
	if in != nil && !isSAML {
		return management.Fault{
			Class:   management.Invalid,
			Message: "This application is not a SAML application.",
			Details: []api.ErrorDetail{{Field: "saml", Issue: "is only valid on an application of type saml"}},
			Reason:  "saml registration on a non-saml application",
		}
	}
	return nil
}

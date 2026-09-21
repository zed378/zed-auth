package samlapi

import (
	"context"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// A SAML login in the audit log (P4-08 F-8).
//
// The card asks that SAML logins appear "identically to OIDC logins", and the
// interesting decision is which event to write.
//
// A new `saml.assertion.issued` type would be easy and wrong. Every existing
// query — the console's audit screen, an operator asking what credentials were
// issued to whom, a detection rule watching for a burst of them — filters on
// the types that exist today, and a new one is invisible to all of them until
// somebody remembers to add it. "Identically" is doing real work in that
// sentence.
//
// So a SAML assertion is `token.issued`, the same event an OAuth token is,
// with the protocol named in the payload. A query for "what was issued to this
// user" returns both without being taught about SAML, and a query that cares
// which protocol can ask.
//
// The sign-in itself is already recorded: a user who went through the hosted
// login produced `user.login.success` there, because SAML reuses that page
// rather than having one of its own. What this adds is the issuance — the part
// with no OIDC-free equivalent.

// Recorder writes audit events. The subset of `management.Recorder` this
// package needs, declared here so it does not depend on the Management API.
type Recorder interface {
	Write(ctx context.Context, tx *postgres.Tx, event audit.Event) error
}

// AuditWriter records SAML assertion issuance.
type AuditWriter struct {
	Recorder Recorder
}

// NewAuditWriter returns an AuditWriter.
func NewAuditWriter(recorder Recorder) *AuditWriter { return &AuditWriter{Recorder: recorder} }

// SAMLAuthenticated records that an assertion was issued.
//
// It runs inside the caller's transaction — the same one that read the claims —
// so an assertion that was issued and an event that says so cannot come apart.
func (a *AuditWriter) SAMLAuthenticated(
	ctx context.Context, tx *postgres.Tx, orgID, userID, entityID string,
) error {
	if a == nil || a.Recorder == nil {
		return nil
	}
	return a.Recorder.Write(ctx, tx, audit.Event{
		OrgID:       orgID,
		ActorUserID: userID,
		Type:        audit.EventTokenIssued,
		Payload: map[string]any{
			// `protocol` is what distinguishes this from an OAuth issuance,
			// and it is a payload field rather than a type so that a query
			// which does not know about SAML still finds it.
			"protocol": "saml",

			// The service provider, by the name it is registered under. Not a
			// client_id: SAML has no such thing, and inventing one would make
			// the two protocols' payloads look interchangeable when they are
			// not.
			"entity_id": entityID,
		},
	})
}

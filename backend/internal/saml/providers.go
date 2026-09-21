package saml

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

// Resolving a service provider before the tenant is known (P4-08 C-1).
//
// An AuthnRequest names an `Issuer`; that entity ID determines the
// organization, and nothing before this lookup does. So it runs outside any
// tenant scope, through a `SECURITY DEFINER` function bounded to one exact
// entity ID and to the columns the flow needs — the same shape ADR-023 settled
// for OIDC's `application_by_client_id`.
//
// Everything the flow does afterwards comes from what this returns. The request
// supplies an id to echo and an issuer to look up; it does not supply a
// destination, an audience, or a list of attributes.

// ErrNoSuchProvider is an entity ID that names no live registration.
var ErrNoSuchProvider = errors.New("saml: no service provider is registered with that entity id")

// Registration is a service provider as the flow needs it.
type Registration struct {
	ServiceProvider

	// ID is the registration row, for recording a pending request against it.
	ID string

	// OrgID is the tenant this login belongs to. Determined here and nowhere
	// else.
	OrgID string

	// ApplicationID ties the registration to the application it belongs to, so
	// a SAML login audits against the same application an OIDC one would.
	ApplicationID string

	// WantSignedRequests and Certificate govern whether this service provider's
	// own AuthnRequests must be verified.
	WantSignedRequests bool
	Certificate        string
}

// Querier is the minimal database handle this lookup needs.
//
// An interface rather than *sql.DB so the caller decides — and so this cannot
// accidentally be handed a tenant-scoped transaction, which would apply RLS to
// a lookup that runs before a tenant exists.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ProviderStore resolves registrations.
type ProviderStore struct{}

// NewProviderStore returns a ProviderStore.
func NewProviderStore() *ProviderStore { return &ProviderStore{} }

// ByEntityID resolves one live registration.
func (s *ProviderStore) ByEntityID(ctx context.Context, db Querier, entityID string) (Registration, error) {
	if entityID == "" {
		return Registration{}, ErrNoSuchProvider
	}

	var (
		reg  Registration
		cert sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT sp_id, org_id, application_id, entity_id, acs_url,
		       attribute_release, want_signed_requests, certificate
		  FROM service_provider_by_entity_id($1)`, entityID,
	).Scan(&reg.ID, &reg.OrgID, &reg.ApplicationID, &reg.EntityID, &reg.ACSURL,
		pq.Array(&reg.Release), &reg.WantSignedRequests, &cert)

	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, ErrNoSuchProvider
	}
	if err != nil {
		return Registration{}, fmt.Errorf("saml: resolving the service provider: %w", err)
	}
	reg.Certificate = cert.String
	return reg, nil
}

// ByID resolves a registration by its own row id.
//
// Used when a pending request is answered: the request recorded WHICH
// registration it was made against, not merely which entity id. A service
// provider that was revoked and re-registered under the same entity id is a
// different row, and a login started against the old one must not be completed
// against the new — the entity id is a name, and the row is the thing.
func (s *ProviderStore) ByID(ctx context.Context, db Querier, id string) (Registration, error) {
	if id == "" {
		return Registration{}, ErrNoSuchProvider
	}

	var (
		reg  Registration
		cert sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT sp_id, org_id, application_id, entity_id, acs_url,
		       attribute_release, want_signed_requests, certificate
		  FROM service_provider_by_id($1::uuid)`, id,
	).Scan(&reg.ID, &reg.OrgID, &reg.ApplicationID, &reg.EntityID, &reg.ACSURL,
		pq.Array(&reg.Release), &reg.WantSignedRequests, &cert)

	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, ErrNoSuchProvider
	}
	if err != nil {
		return Registration{}, fmt.Errorf("saml: resolving the service provider: %w", err)
	}
	reg.Certificate = cert.String
	return reg, nil
}

package saml

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Managing a registration, as opposed to resolving one (P4-09).
//
// `providers.go` reads a registration at login time, before a tenant exists,
// through a bounded SECURITY DEFINER function. This is the other side: an
// administrator of a known organization creating, reading and changing one.
//
// It runs inside the caller's tenant transaction and touches the table
// directly, which is correct here for the reason it was wrong there — the
// tenant IS known, row-level security applies, and a function that bypassed it
// would be a function that could be called with somebody else's application id.

var (
	// ErrNoRegistration is an application with no SAML registration.
	ErrNoRegistration = errors.New("saml: the application has no service provider registration")

	// ErrEntityIDTaken is an entity ID another live registration already holds.
	//
	// Instance-wide, not per organization. An entity ID is a global name in
	// SAML: two organizations claiming the same one would make "which service
	// provider is this assertion for" ambiguous at the moment it decides where
	// an assertion goes.
	ErrEntityIDTaken = errors.New("saml: that entity id is already registered")
)

// Managed is a registration as an administrator sees it.
type Managed struct {
	ID            string
	ApplicationID string
	OrgID         string

	EntityID         string
	ACSURL           string
	AttributeRelease []string

	WantSignedRequests bool
	CertificatePEM     string
	AllowIdPInitiated  bool

	CreatedAt time.Time
}

// CertificateExpiry reports when the service provider's certificate expires.
//
// Zero when there is no certificate, which is not a problem: a service
// provider that does not sign its requests has nothing to expire. The caller
// distinguishes "no certificate" from "expiring soon" rather than this
// returning a misleading date.
func (m Managed) CertificateExpiry() (time.Time, error) {
	if strings.TrimSpace(m.CertificatePEM) == "" {
		return time.Time{}, nil
	}
	cert, err := ParseCertificate(m.CertificatePEM)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// CertificateWarningWindow is how long before expiry a certificate is reported
// as expiring.
//
// Thirty days, because the remedy is not this service's to perform: somebody
// has to ask the other party for a new certificate and wait for them to send
// it. A window measured in days would be an alert that arrives after the only
// useful time to act on it.
const CertificateWarningWindow = 30 * 24 * time.Hour

// Registrations manages SAML service provider registrations.
type Registrations struct{}

// NewRegistrations returns a Registrations.
func NewRegistrations() *Registrations { return &Registrations{} }

// Create registers a service provider against an application.
func (r *Registrations) Create(
	ctx context.Context, tx *postgres.Tx, orgID string, m Managed,
) (Managed, error) {
	if err := validateManaged(m); err != nil {
		return Managed{}, err
	}

	var id string
	var createdAt time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO saml_service_providers
		       (application_id, org_id, entity_id, acs_url, attribute_release,
		        want_signed_requests, certificate, allow_idp_initiated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		m.ApplicationID, orgID, m.EntityID, m.ACSURL, pq.Array(m.AttributeRelease),
		m.WantSignedRequests, nullableText(m.CertificatePEM), m.AllowIdPInitiated).
		Scan(&id, &createdAt)
	if err != nil {
		return Managed{}, translateWriteError(err)
	}

	m.ID = id
	m.OrgID = orgID
	m.CreatedAt = createdAt
	return m, nil
}

// ByApplication reads the live registration an application owns.
func (r *Registrations) ByApplication(
	ctx context.Context, tx *postgres.Tx, applicationID string,
) (Managed, error) {
	var (
		m           Managed
		certificate sql.NullString
	)
	err := tx.QueryRow(ctx, `
		SELECT id::text, application_id::text, org_id::text, entity_id, acs_url,
		       attribute_release, want_signed_requests, certificate,
		       allow_idp_initiated, created_at
		  FROM saml_service_providers
		 WHERE application_id = $1
		   AND revoked_at IS NULL`, applicationID).
		Scan(&m.ID, &m.ApplicationID, &m.OrgID, &m.EntityID, &m.ACSURL,
			pq.Array(&m.AttributeRelease), &m.WantSignedRequests, &certificate,
			&m.AllowIdPInitiated, &m.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return Managed{}, ErrNoRegistration
	}
	if err != nil {
		return Managed{}, fmt.Errorf("saml: reading the registration: %w", err)
	}
	m.CertificatePEM = certificate.String
	return m, nil
}

// Update replaces the registration an application owns.
//
// Every field at once rather than a patch, because several of them constrain
// each other: `want_signed_requests` needs a certificate, and a caller sending
// only the flag would be relying on a certificate it has not seen. The
// database CHECK refuses the inconsistent combination either way; sending the
// whole registration means the caller has looked at all of it.
func (r *Registrations) Update(
	ctx context.Context, tx *postgres.Tx, applicationID string, m Managed,
) (Managed, error) {
	m.ApplicationID = applicationID
	if err := validateManaged(m); err != nil {
		return Managed{}, err
	}

	var (
		id          string
		orgID       string
		createdAt   time.Time
		certificate sql.NullString
	)
	err := tx.QueryRow(ctx, `
		UPDATE saml_service_providers
		   SET entity_id = $2, acs_url = $3, attribute_release = $4,
		       want_signed_requests = $5, certificate = $6, allow_idp_initiated = $7
		 WHERE application_id = $1
		   AND revoked_at IS NULL
		RETURNING id::text, org_id::text, certificate, created_at`,
		applicationID, m.EntityID, m.ACSURL, pq.Array(m.AttributeRelease),
		m.WantSignedRequests, nullableText(m.CertificatePEM), m.AllowIdPInitiated).
		Scan(&id, &orgID, &certificate, &createdAt)

	if errors.Is(err, sql.ErrNoRows) {
		return Managed{}, ErrNoRegistration
	}
	if err != nil {
		return Managed{}, translateWriteError(err)
	}

	m.ID = id
	m.OrgID = orgID
	m.CreatedAt = createdAt
	m.CertificatePEM = certificate.String
	return m, nil
}

// validateManaged refuses what this service will not store.
//
// Ahead of the database rather than instead of it. The CHECK constraints are
// the guarantee; these produce a message naming the field, because a caller
// who gets `saml_sp_acs_url_https` back has to go and read a migration to find
// out what they did.
func validateManaged(m Managed) error {
	entity := strings.TrimSpace(m.EntityID)
	switch {
	case entity == "":
		return invalidRegistration("entity_id", "is required")
	case len(entity) > MaxEntityIDBytes:
		return invalidRegistration("entity_id",
			fmt.Sprintf("must be %d bytes or fewer", MaxEntityIDBytes))
	}

	acs := strings.TrimSpace(m.ACSURL)
	switch {
	case acs == "":
		return invalidRegistration("acs_url", "is required")
	case len(acs) > MaxACSURLBytes:
		return invalidRegistration("acs_url",
			fmt.Sprintf("must be %d bytes or fewer", MaxACSURLBytes))
	case !strings.HasPrefix(acs, "https://"):
		// An assertion is a bearer credential for the length of its window.
		// Delivering one over cleartext hands it to every hop in between.
		return invalidRegistration("acs_url", "must be an https URL")
	}

	if m.CertificatePEM != "" {
		if _, err := ParseCertificate(m.CertificatePEM); err != nil {
			return invalidRegistration("certificate", "is not a PEM X.509 certificate")
		}
	}
	if m.WantSignedRequests && strings.TrimSpace(m.CertificatePEM) == "" {
		// The flag with nothing behind it is the "stored and enforced by
		// nothing" shape P4-08 spent a task removing. Refused rather than
		// accepted and quietly ineffective.
		return invalidRegistration("certificate",
			"is required when want_signed_requests is set, because there would otherwise be nothing to verify against")
	}

	return nil
}

// ErrInvalidRegistration marks a caller mistake rather than a failure.
var ErrInvalidRegistration = errors.New("saml: invalid registration")

// FieldError names the field a caller got wrong.
type FieldError struct {
	Field string
	Issue string
}

func (e FieldError) Error() string {
	return fmt.Sprintf("%s: %s %s", ErrInvalidRegistration, e.Field, e.Issue)
}

func (e FieldError) Unwrap() error { return ErrInvalidRegistration }

func invalidRegistration(field, issue string) error {
	return FieldError{Field: field, Issue: issue}
}

// translateWriteError turns a constraint violation into something a caller can
// act on.
//
// The unique index on a live entity ID is the one a caller hits by accident,
// and "duplicate key value violates unique constraint
// saml_service_providers_entity_id_live" is not an answer anybody can use.
func translateWriteError(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Constraint {
		case "saml_service_providers_entity_id_live":
			return ErrEntityIDTaken
		case "saml_sp_acs_url_https":
			return invalidRegistration("acs_url", "must be an https URL")
		case "saml_sp_entity_id_present":
			return invalidRegistration("entity_id", "is required")
		case "saml_sp_signed_needs_cert":
			return invalidRegistration("certificate",
				"is required when want_signed_requests is set")
		}
	}
	return fmt.Errorf("saml: writing the registration: %w", err)
}

// nullableText stores an empty string as NULL.
//
// The column is nullable and the CHECK is written against NULL, so an empty
// string would satisfy "not null" while meaning "absent" — the two must not
// come apart.
func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

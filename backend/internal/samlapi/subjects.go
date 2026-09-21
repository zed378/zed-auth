package samlapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What an assertion says about a user (P4-08 F-6, C-5).
//
// Two rules, and the second is the one that gets broken by accident.
//
// **The NameID is persistent and per-service-provider**, minted once and
// stored. Never the email address.
//
// **The attributes are an allow list**, intersected with what the registration
// says this service provider receives. The direction matters: a new attribute
// added to a user reaches no service provider until somebody registers it. The
// alternative — release everything and let each provider ignore what it does
// not want — leaks every future attribute to every existing integration on the
// day it is introduced.

// ErrNoSuchUser is a session naming a user that is no longer there.
var ErrNoSuchUser = errors.New("samlapi: no such user")

// SubjectStore builds the subject of an assertion.
type SubjectStore struct {
	NameIDs *saml.NameIDs
	Now     func() time.Time
}

// NewSubjectStore returns a SubjectStore.
func NewSubjectStore() *SubjectStore {
	return &SubjectStore{NameIDs: saml.NewNameIDs()}
}

func (s *SubjectStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// For assembles what this service will say about a user to one service
// provider.
//
// Everything it reads is inside the caller's tenant transaction, so a user of
// another organization is not merely refused — it is invisible.
func (s *SubjectStore) For(
	ctx context.Context, tx *postgres.Tx, userID string, reg saml.Registration,
) (saml.Subject, error) {
	// The attributes this service COULD release. Read before narrowing, so the
	// narrowing is visible in one place rather than spread across the query.
	var (
		email       string
		displayName sql.NullString
		username    sql.NullString
		status      string
		roleKeys    []string
	)
	err := tx.QueryRow(ctx, `
		SELECT u.email, u.display_name, u.username, u.status,
		       coalesce(
		         (SELECT array_agg(DISTINCT k ORDER BY k)
		            FROM user_grants g, unnest(g.role_keys) AS k
		           WHERE g.user_id = u.id AND g.project_id = a.project_id),
		         '{}')
		  FROM users u
		  JOIN applications a ON a.id = $2
		 WHERE u.id = $1`,
		userID, reg.ApplicationID,
	).Scan(&email, &displayName, &username, &status, pq.Array(&roleKeys))

	if errors.Is(err, sql.ErrNoRows) {
		return saml.Subject{}, ErrNoSuchUser
	}
	if err != nil {
		return saml.Subject{}, fmt.Errorf("samlapi: reading the subject: %w", err)
	}

	// A user who is not active does not get an assertion, whatever the session
	// says. A session outlives a deactivation by design — revoking every one is
	// P3-11's job and is not instant — so this is the check that stops a
	// deactivated user from signing in to a service provider in the meantime.
	if status != "active" {
		return saml.Subject{}, fmt.Errorf("%w: the account is %s", ErrNoSuchUser, status)
	}

	nameID, err := s.NameIDs.For(ctx, tx, reg.OrgID, userID, reg.ID, s.now())
	if err != nil {
		return saml.Subject{}, err
	}

	// Everything this service knows that a service provider could be
	// registered for. `saml.Release` intersects it with the registration; what
	// is not in `reg.Release` does not leave this function.
	available := availableAttributes(email, roleKeys, displayName, username)

	return saml.Subject{
		NameID: nameID,
		// Persistent, matching what the metadata advertises. A format that
		// said `emailAddress` while the value is a random identifier would be
		// a lie a service provider acts on.
		NameIDFormat: saml.NameIDFormatPersistent,
		Attributes:   saml.Release(available, reg.Release),
	}, nil
}

func nullable(v sql.NullString) []string {
	if !v.Valid || v.String == "" {
		return nil
	}
	return []string{v.String}
}

// availableAttributes is every attribute a subject can carry.
//
// Its keys must be exactly saml.ReleasableAttributes, and a test asserts it.
// A key here that the list lacks could never be requested; a name in the list
// that is missing here would be accepted by the API and release nothing, which
// is the failure the list exists to prevent.
func availableAttributes(
	email string, roleKeys []string, displayName, username sql.NullString,
) map[string][]string {
	return map[string][]string{
		"email":        {email},
		"role_keys":    roleKeys,
		"display_name": nullable(displayName),
		"username":     nullable(username),
	}
}

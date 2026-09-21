package saml

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What a service provider knows a user by (P4-08 C-5).
//
// Never an email address. Two failures follow from one, and both are permanent
// by the time anybody notices: a service provider keyed on an address hands a
// departed user's account to whoever is issued that address next, and the same
// address at two service providers lets them correlate a user who never agreed
// to be correlated.
//
// So: a random identifier per (user, service provider), minted once and stable
// afterwards. Across service providers the values are unrelated, which is the
// property the `persistent` NameID format exists to provide.

// nameIDBytes is the entropy behind one identifier.
//
// 32 bytes. It is not a secret — a service provider stores it in the clear and
// so does this service — but it must be unguessable, because guessing one is
// claiming to be that user at that service provider if anything downstream ever
// accepts a NameID without an assertion around it.
const nameIDBytes = 32

// NameIDs mints and reads per-service-provider identifiers.
type NameIDs struct{}

// NewNameIDs returns a NameIDs.
func NewNameIDs() *NameIDs { return &NameIDs{} }

// For returns the identifier this user is known by at this service provider,
// minting one on first sign-in.
//
// The insert is `ON CONFLICT DO NOTHING` followed by a read, rather than a read
// followed by an insert. Two sign-ins racing on a user's first visit both find
// nothing and both mint; the primary key makes one of them lose, and the loser
// reads the winner's value instead of failing. A read-then-insert would issue
// two identities and store whichever landed last.
func (n *NameIDs) For(
	ctx context.Context, tx *postgres.Tx, orgID, userID, spID string, now time.Time,
) (string, error) {
	minted, err := newNameID()
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO saml_name_ids (user_id, sp_id, org_id, name_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, sp_id) DO NOTHING`,
		userID, spID, orgID, minted, now); err != nil {
		return "", fmt.Errorf("saml: minting a NameID: %w", err)
	}

	// Read back unconditionally. On the first sign-in this returns what was
	// just written; on every later one, and on a lost race, it returns the
	// value the service provider already knows.
	var stored string
	err = tx.QueryRow(ctx, `
		SELECT name_id FROM saml_name_ids WHERE user_id = $1 AND sp_id = $2`,
		userID, spID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		// Unreachable: the insert above either wrote the row or found one.
		// Handled rather than ignored, because the alternative is returning an
		// empty NameID, which is an assertion about nobody.
		return "", fmt.Errorf("saml: no NameID after minting one")
	}
	if err != nil {
		return "", fmt.Errorf("saml: reading the NameID: %w", err)
	}
	return stored, nil
}

// newNameID returns an opaque identifier.
//
// base64url without padding: it travels in XML, in a service provider's
// database, and quite often in a URL, and `+`, `/` and `=` are the characters
// that get mangled on that journey.
func newNameID() (string, error) {
	b := make([]byte, nameIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("saml: generating a NameID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

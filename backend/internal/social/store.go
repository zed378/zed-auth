package social

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

type Store struct{}

func NewStore() *Store { return &Store{} }

// BySubject reads only links visible in the transaction's organization. An
// unknown subject and another organization's link have the same result.
// It never provisions a user or falls back to matching an email (ADR-030).
func (s *Store) BySubject(ctx context.Context, tx *postgres.Tx, provider Provider, subject string) (Identity, error) {
	if !provider.Valid() {
		return Identity{}, ErrInvalidProvider
	}
	if !validSubject(subject) {
		return Identity{}, ErrInvalidSubject
	}

	var identity Identity
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, provider, provider_subject, linked_at
		FROM user_identities WHERE provider = $1 AND provider_subject = $2`,
		string(provider), subject).Scan(&identity.ID, &identity.UserID,
		&identity.Provider, &identity.Subject, &identity.LinkedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Identity{}, ErrNotFound
	case err != nil:
		return Identity{}, fmt.Errorf("social: reading identity: %w", err)
	}
	return identity, nil
}

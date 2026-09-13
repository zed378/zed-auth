package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Sessions as a resource a person manages (P3-09).
//
// Specification: MEMORY/specs/P3-09-session-management-api.md.
//
// Everything here takes the user the session must belong to, and binds the two
// in the same statement. "Look the session up, then check whose it is" is two
// steps a later edit can separate; a WHERE clause with both is one that cannot.

// Reasons a person-driven revocation records.
const (
	// ReasonSelfService is a user ending one of their own sessions.
	ReasonSelfService Reason = "self_service"

	// ReasonLogoutOthers is a user ending every session but the current one.
	ReasonLogoutOthers Reason = "logout_others"
)

// ListPosition is where a page of sessions resumes: the last row of the
// previous page. Zero means the first page.
type ListPosition struct {
	CreatedAt time.Time
	ID        string
}

// ListLive reads a user's usable sessions, newest first.
//
// Usable means not revoked, not past its absolute expiry and, when the idle
// timeout is positive, used within it. A session past its idle timeout is dead
// — Lookup refuses it — and listing it would offer to revoke something that can
// no longer be used.
func (s *Store) ListLive(
	ctx context.Context, tx *postgres.Tx, userID string, idle time.Duration, now time.Time,
	after ListPosition, limit int,
) ([]Session, error) {
	var idleFloor any
	if idle > 0 {
		idleFloor = now.Add(-idle)
	}
	var afterAt, afterID any
	if after.ID != "" {
		afterAt, afterID = after.CreatedAt, after.ID
	}

	rows, err := tx.Query(ctx, `
		SELECT `+columns+`
		  FROM sessions
		 WHERE user_id = $1
		   AND revoked_at IS NULL
		   AND expires_at > $2
		   AND ($3::timestamptz IS NULL OR last_seen_at > $3)
		   AND ($4::timestamptz IS NULL OR (created_at, id) < ($4, $5::uuid))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $6`, userID, now, idleFloor, afterAt, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("session: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Session{}
	for rows.Next() {
		session, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("session: listing: %w", err)
		}
		out = append(out, session)
	}
	return out, rows.Err()
}

// RevokeOwned ends one session, only if it belongs to the user.
//
// `found` is false when no such session exists for that user — including one
// belonging to someone else, which must be indistinguishable from none. When
// found is true and `revoked` is false, the session was already over; that is
// not an error, because ending an ended session is what a double-click does.
func (s *Store) RevokeOwned(
	ctx context.Context, tx *postgres.Tx, sessionID, userID string, now time.Time,
) (session Session, found, revoked bool, err error) {
	var alreadyRevoked bool
	row := tx.QueryRow(ctx, `
		SELECT `+columns+`, revoked_at IS NOT NULL
		  FROM sessions
		 WHERE id = $1 AND user_id = $2
		 FOR UPDATE`, sessionID, userID)

	var methods jsonStrings
	err = row.Scan(
		&session.ID, &session.UserID, &session.OrgID, &methods,
		&session.IP, &session.UserAgent,
		&session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt,
		&alreadyRevoked,
	)
	session.AuthMethods = methods
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, false, false, nil
	case err != nil:
		return Session{}, false, false, fmt.Errorf("session: reading for revocation: %w", err)
	}

	if alreadyRevoked || !now.Before(session.ExpiresAt) {
		return session, true, false, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, sessionID, now); err != nil {
		return Session{}, false, false, fmt.Errorf("session: revoking: %w", err)
	}
	return session, true, true, nil
}

// RevokeAllForUserExcept ends every live session a user has but one.
//
// `keep` must be non-empty. An empty keep would match nothing in `id <> $2`
// and quietly become "revoke all" — a different, larger action than the one
// the caller asked for — so it is refused instead.
func (s *Store) RevokeAllForUserExcept(
	ctx context.Context, tx *postgres.Tx, userID, keep string, now time.Time,
) ([]string, []string, error) {
	if keep == "" {
		return nil, nil, fmt.Errorf("session: revoking all but one needs the one to keep")
	}

	rows, err := tx.Query(ctx, `
		UPDATE sessions SET revoked_at = $3
		 WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL AND expires_at > $3
		RETURNING id, COALESCE(token_hash, '')`, userID, keep, now)
	if err != nil {
		return nil, nil, fmt.Errorf("session: revoking others: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids, hashes []string
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, nil, fmt.Errorf("session: revoking others: %w", err)
		}
		ids = append(ids, id)
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	return ids, hashes, rows.Err()
}

// --- the manager ------------------------------------------------------------------------

// ListLive reads a user's usable sessions under the manager's policy.
func (m *Manager) ListLive(
	ctx context.Context, tx *postgres.Tx, userID string, policy Policy, now time.Time,
	after ListPosition, limit int,
) ([]Session, error) {
	return m.store.ListLive(ctx, tx, userID, policy.IdleTimeout, now, after, limit)
}

// Revocation is what a person-driven revocation did.
//
// **These revocations are NOT audited here**, unlike Revoke and
// RevokeAllForUser. Their only caller is the Management API, whose events must
// go through management.Audit: that is what attaches the request id and the
// client address, and what tells the API's AuditGuard the mutation was
// recorded. An event written here would satisfy neither, and the guard would
// report every revocation as an unaudited mutation. The caller audits from the
// fields below.
type Revocation struct {
	// Found is false when the session does not exist for that user.
	Found bool

	// SessionIDs are the sessions actually ended by this call.
	SessionIDs []string

	// UserID is whose sessions they were.
	UserID string

	// Invalidate removes the ended sessions from the cache. Call it after the
	// transaction commits, never before — see Revoke.
	Invalidate func(context.Context) error
}

func noInvalidation(context.Context) error { return nil }

// RevokeOwned ends one of a user's sessions. The caller audits; see Revocation.
func (m *Manager) RevokeOwned(
	ctx context.Context, tx *postgres.Tx, sessionID, userID string, now time.Time,
) (Revocation, error) {
	session, found, revoked, err := m.store.RevokeOwned(ctx, tx, sessionID, userID, now)
	if err != nil {
		return Revocation{}, err
	}
	if !found {
		return Revocation{Invalidate: noInvalidation}, nil
	}
	if !revoked {
		return Revocation{Found: true, Invalidate: noInvalidation}, nil
	}

	hash := m.hashFor(ctx, tx, sessionID)
	expires := session.ExpiresAt
	return Revocation{
		Found:      true,
		SessionIDs: []string{session.ID},
		UserID:     session.UserID,
		Invalidate: func(ctx context.Context) error {
			if hash == "" {
				return nil
			}
			return m.cache.Invalidate(ctx, hash, expires)
		},
	}, nil
}

// RevokeOthers ends every session a user has except `keep`. The caller audits;
// see Revocation.
func (m *Manager) RevokeOthers(
	ctx context.Context, tx *postgres.Tx, userID, keep string, now time.Time,
) (Revocation, error) {
	ids, hashes, err := m.store.RevokeAllForUserExcept(ctx, tx, userID, keep, now)
	if err != nil {
		return Revocation{}, err
	}
	if len(ids) == 0 {
		return Revocation{Found: true, UserID: userID, Invalidate: noInvalidation}, nil
	}

	until := now.Add(MaxAbsoluteLifetime)
	return Revocation{
		Found:      true,
		SessionIDs: ids,
		UserID:     userID,
		Invalidate: func(ctx context.Context) error {
			var failed error
			for _, hash := range hashes {
				if err := m.cache.Invalidate(ctx, hash, until); err != nil {
					failed = err
				}
			}
			return failed
		},
	}, nil
}

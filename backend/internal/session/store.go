package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The durable record. PostgreSQL is authoritative (ADR-003); Redis in cache.go
// is a lookup copy in front of it.
//
// Sessions are the one place the tenant-scoped storage API cannot be the only
// path, because the session is what establishes which tenant a request belongs
// to. LookupByTokenHash therefore runs instance-scoped — deliberately, narrowly,
// and through the audited mechanism P0-08 built for exactly this.

// ErrNotFound means no usable session. Absent, expired, revoked and never
// existed all collapse into it on purpose: the caller redirects to login in
// every case, and distinguishing them for the browser is a free oracle.
var ErrNotFound = errors.New("session: not found")

// Store persists sessions.
type Store struct{}

func NewStore() *Store { return &Store{} }

// New is the input to Create.
type New struct {
	UserID      string
	OrgID       string
	AuthMethods []string
	IP          string
	UserAgent   string
}

// columns selects auth_methods as JSON.
//
// database/sql hands a text[] back as the raw Postgres array literal and there
// is no standard scanner for it — the same wall internal/oauth/client hit.
// Letting Postgres serialise to JSON means it does the escaping it already
// knows how to do, and encoding/json does the reading.
const columns = `
	id, user_id, org_id, to_jsonb(auth_methods),
	COALESCE(host(ip), ''), COALESCE(user_agent, ''),
	created_at, last_seen_at, expires_at`

// maxUserAgent bounds what is stored.
//
// A user agent is attacker-controlled and unbounded; the sessions screen shows
// it and Phase 3 compares it. 512 bytes is far more than any real browser
// sends and stops the column being a place to park data.
const maxUserAgent = 512

// Create writes a new session and returns it with its token.
//
// The token is returned here and nowhere else. No read path can reconstruct
// it, because only the hash was stored (PG-14).
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, in New, policy Policy, now time.Time,
) (Session, Token, error) {
	if len(in.AuthMethods) == 0 {
		// A session with no recorded factor would make P1-07's `amr` claim a
		// lie, and an untrustworthy claim is worse than an absent one.
		return Session{}, Token{}, fmt.Errorf("session: auth_methods is required")
	}

	token, hash, err := NewToken()
	if err != nil {
		return Session{}, Token{}, err
	}

	policy, _ = policy.Sanitize()
	expires := now.Add(policy.AbsoluteLifetime)

	var ip any
	if parsed := net.ParseIP(in.IP); parsed != nil {
		ip = parsed.String()
	}

	agent := in.UserAgent
	if len(agent) > maxUserAgent {
		agent = agent[:maxUserAgent]
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO sessions (user_id, org_id, auth_methods, ip, user_agent, token_hash,
		                      created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $4::inet, $5, $6, $7, $7, $8)
		RETURNING `+columns,
		in.UserID, in.OrgID, in.AuthMethods, ip, agent, hash, now, expires)

	session, err := scan(row)
	if err != nil {
		return Session{}, Token{}, fmt.Errorf("session: creating: %w", err)
	}

	return session, token, nil
}

// LookupByTokenHash resolves a token hash to a live session.
//
// This is the bootstrap: the session is what establishes which tenant a
// request belongs to, so the read that resolves a cookie cannot itself be
// tenant-scoped. It goes through session_by_token_hash, a SECURITY DEFINER
// function granted to auth_app that does exactly this one thing.
//
// Not instance scope. `sessions_tenant_isolation` is `org_id =
// current_org_id()`, so with no tenant set the comparison is NULL and no row
// matches — instance scope means "no tenant", not "every tenant". Using it
// here would also log cross-tenant access on every cache miss and trip the
// UnexpectedInstanceScopedAccess alert, which would be both noisy and untrue:
// resolving a cookie is not reaching across tenants, it is finding out which
// tenant you are in.
//
// Liveness is filtered inside the function, so a revoked or expired row never
// travels back through the cache layer where a later read might use it.
func (s *Store) LookupByTokenHash(
	ctx context.Context, db *postgres.DB, hash string, now time.Time,
) (Session, error) {
	row := db.SQL().QueryRowContext(ctx,
		`SELECT id, user_id, org_id, auth_methods, ip, user_agent,
		        created_at, last_seen_at, expires_at
		   FROM session_by_token_hash($1, $2)`, hash, now)

	session, err := scan(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("session: looking up: %w", err)
	}
	return session, nil
}

// Touch records that a session was used.
//
// Throttled by the caller, not here: NFR-5 forbids a write on every
// authenticated request, and the enforcing idle bound is the Redis TTL. This
// column is the durable fallback and the value the sessions screen shows.
func (s *Store) Touch(ctx context.Context, db *postgres.DB, orgID, id string, now time.Time) error {
	return db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, id, now)
		return err
	})
}

// Revoke marks one session revoked.
//
// A status transition rather than a delete: the row is needed for the audit
// trail and for the sessions screen to show that something was ended.
//
// Returns the session so the caller can invalidate the cache and audit the
// event without reading it again.
func (s *Store) Revoke(
	ctx context.Context, tx *postgres.Tx, id string, now time.Time,
) (Session, error) {
	row := tx.QueryRow(ctx, `
		UPDATE sessions SET revoked_at = $2
		 WHERE id = $1 AND revoked_at IS NULL
		RETURNING `+columns, id, now)

	session, err := scan(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Already revoked, or not visible in this tenant scope. Both are
		// "nothing to do" and neither is an error worth propagating: a logout
		// pressed twice is not a failure.
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("session: revoking: %w", err)
	}
	return session, nil
}

// RevokeAllForUser ends every live session a user has.
//
// The "log out everywhere" docs/PLAN/05 § Session & logout asks for, and the
// response to a stolen cookie. Returns the token hashes so the caller can
// invalidate each cache entry — the hashes never leave this package's callers
// and are not the tokens themselves.
func (s *Store) RevokeAllForUser(
	ctx context.Context, tx *postgres.Tx, userID string, now time.Time,
) ([]string, []string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE sessions SET revoked_at = $2
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2
		RETURNING id, COALESCE(token_hash, '')`, userID, now)
	if err != nil {
		return nil, nil, fmt.Errorf("session: revoking all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids, hashes []string
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, nil, fmt.Errorf("session: revoking all: %w", err)
		}
		ids = append(ids, id)
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("session: revoking all: %w", err)
	}

	return ids, hashes, nil
}

// Sweep deletes sessions that can no longer be used.
//
// DoD item 4: expired sessions are cleaned up rather than accumulating. Runs on
// the same pattern as P0-12's partition maintenance — a bounded batch on a
// schedule, so a long-neglected table is caught up over several runs instead of
// producing one enormous transaction.
//
// Revoked rows are kept for a grace period rather than deleted at once: they
// are what the sessions screen shows to explain why a user was signed out, and
// what an incident investigation reads.
func (s *Store) Sweep(ctx context.Context, db *postgres.DB, olderThan time.Time, limit int) (int, error) {
	// Through sweep_expired_sessions for the same reason LookupByTokenHash
	// goes through its own function: this is instance-wide maintenance with no
	// tenant to scope to, and `sessions_tenant_isolation` means auth_app
	// deletes nothing with none set. P0-12 solved the identical problem for
	// partition maintenance the identical way.
	var deleted int
	err := db.SQL().QueryRowContext(ctx,
		`SELECT sweep_expired_sessions($1, $2)`, olderThan, limit).Scan(&deleted)
	if err != nil {
		return 0, fmt.Errorf("session: sweeping: %w", err)
	}
	return deleted, nil
}

func scan(row interface{ Scan(...any) error }) (Session, error) {
	var (
		s       Session
		methods jsonStrings
	)
	err := row.Scan(
		&s.ID, &s.UserID, &s.OrgID, &methods,
		&s.IP, &s.UserAgent,
		&s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt,
	)
	s.AuthMethods = methods
	return s, err
}

// jsonStrings scans a JSON array of strings, always yielding a non-nil slice.
type jsonStrings []string

func (j *jsonStrings) Scan(src any) error {
	*j = jsonStrings{}

	var raw []byte
	switch v := src.(type) {
	case nil:
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("session: cannot scan %T into a string array", src)
	}

	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("session: decoding auth_methods: %w", err)
	}
	if out != nil {
		*j = out
	}
	return nil
}

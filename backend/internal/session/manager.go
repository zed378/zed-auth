package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Manager composes the durable store and the cache into the operations the
// rest of the service calls.
//
// The split matters: store.go is correct and slower, cache.go makes it fast,
// and this file is the only place that knows both exist. Adding the cache
// after the semantics were settled — rather than designing around it — is what
// keeps "revocation is immediate" a property of the system rather than a
// property of the cache's TTL.

// Reason explains why a session ended. "The user logged out" and "an
// administrator revoked this" are different facts, and a log that conflates
// them cannot answer why somebody was signed out.
type Reason string

const (
	ReasonLogout    Reason = "logout"
	ReasonAdmin     Reason = "admin"
	ReasonReauth    Reason = "reauth"
	ReasonLogoutAll Reason = "logout_all"
)

// touchInterval throttles last_seen_at writes.
//
// NFR-5: no database write on every authenticated request. The enforcing idle
// bound is the cache TTL and the session's own expiry; this column is the
// durable fallback and what the sessions screen shows, so a minute of drift
// costs nothing and a write per request would cost a great deal.
const touchInterval = time.Minute

type Manager struct {
	db    *postgres.DB
	store *Store
	cache *Cache
	audit *audit.Writer
	log   *slog.Logger
}

func NewManager(db *postgres.DB, cache *Cache, auditor *audit.Writer, log *slog.Logger) *Manager {
	return &Manager{db: db, store: NewStore(), cache: cache, audit: auditor, log: log}
}

// Create starts a session and returns the token for the cookie.
//
// Takes the caller's transaction so the session and its audit event commit
// together (ADR-012). The cache is populated after the caller commits, not
// here — a cached session for a transaction that rolled back would be a
// session that exists only in Redis.
func (m *Manager) Create(
	ctx context.Context, tx *postgres.Tx, in New, policy Policy, now time.Time,
) (Session, Token, error) {
	session, token, err := m.store.Create(ctx, tx, in, policy, now)
	if err != nil {
		return Session{}, Token{}, err
	}

	if err := m.audit.Write(ctx, tx, audit.Event{
		OrgID:       session.OrgID,
		ActorUserID: session.UserID,
		Type:        audit.EventSessionCreated,
		Payload: map[string]any{
			// The internal id, never the token or its hash. PG-14 is what
			// makes this line safe to write.
			"session_id":   session.ID,
			"auth_methods": session.AuthMethods,
			"expires_at":   session.ExpiresAt,
		},
		IP: session.IP,
	}); err != nil {
		return Session{}, Token{}, fmt.Errorf("session: auditing creation: %w", err)
	}

	return session, token, nil
}

// Lookup resolves a cookie value to a live session.
//
// The hot path: PLAN/12 gives /oauth/authorize 150ms at p95 for everything,
// so the common case is one Redis round trip.
func (m *Manager) Lookup(ctx context.Context, presented string, policy Policy, now time.Time) (Session, error) {
	// Before any lookup. A malformed cookie is free for an attacker to send
	// and should not become a database query.
	if !ValidToken(presented) {
		return Session{}, ErrNotFound
	}

	hash := HashToken(presented)

	if cached, ok := m.cache.Get(ctx, hash); ok {
		if !cached.Live(policy.IdleTimeout, now) {
			return Session{}, ErrNotFound
		}
		m.touch(ctx, cached, now)
		return cached, nil
	}

	start := time.Now()
	session, err := m.store.LookupByTokenHash(ctx, m.db, hash, now)
	if err != nil {
		return Session{}, err
	}
	if m.cache != nil && m.cache.Observer != nil {
		m.cache.Observer.Lookup("database", time.Since(start))
	}

	// Idle expiry is checked here rather than in SQL because it depends on the
	// caller's policy, which is per organization and not known to the query.
	if session.ExpiredIdle(policy.IdleTimeout, now) {
		return Session{}, ErrNotFound
	}

	// May legitimately refuse: a revocation landing during this read writes a
	// tombstone, and declining to cache is the correct outcome.
	if _, err := m.cache.Put(ctx, hash, session); err != nil && m.log != nil {
		m.log.Warn("caching session failed", "error", err.Error())
	}

	m.touch(ctx, session, now)
	return session, nil
}

// touch records activity, at most once per touchInterval per session.
//
// Best effort and deliberately not fatal: failing a login because a bookkeeping
// write failed would trade a real outage for a cosmetic inaccuracy.
func (m *Manager) touch(ctx context.Context, s Session, now time.Time) {
	if now.Sub(s.LastSeenAt) < touchInterval {
		return
	}
	if err := m.store.Touch(ctx, m.db, s.OrgID, s.ID, now); err != nil && m.log != nil {
		m.log.Warn("recording session activity failed", "session_id", s.ID, "error", err.Error())
	}
}

// Revoke ends one session, effective on the next request.
//
// The ordering is the guarantee: PostgreSQL commits first, then the cache is
// invalidated. Doing it the other way lets a concurrent reader repopulate from
// the pre-commit state.
//
// Because the invalidation must follow the commit, it cannot be inside the
// caller's transaction — so this takes the transaction for the write and does
// the cache work after the caller commits, via the returned function.
func (m *Manager) Revoke(
	ctx context.Context, tx *postgres.Tx, sessionID string, reason Reason, actorUserID string, now time.Time,
) (func(context.Context) error, error) {
	session, err := m.store.Revoke(ctx, tx, sessionID, now)
	if errors.Is(err, ErrNotFound) {
		// Already revoked or already gone. A logout pressed twice is not a
		// failure, and there is nothing left to invalidate.
		return func(context.Context) error { return nil }, nil
	}
	if err != nil {
		return nil, err
	}

	if err := m.audit.Write(ctx, tx, audit.Event{
		OrgID:       session.OrgID,
		ActorUserID: actorUserID,
		Type:        audit.EventSessionRevoked,
		Payload: map[string]any{
			"session_id": session.ID,
			"user_id":    session.UserID,
			"reason":     string(reason),
		},
	}); err != nil {
		return nil, fmt.Errorf("session: auditing revocation: %w", err)
	}

	hash := m.hashFor(ctx, tx, sessionID)
	expires := session.ExpiresAt

	return func(ctx context.Context) error {
		if hash == "" {
			return nil
		}
		return m.cache.Invalidate(ctx, hash, expires)
	}, nil
}

// RevokeAllForUser ends every live session a user has.
func (m *Manager) RevokeAllForUser(
	ctx context.Context, tx *postgres.Tx, userID, orgID string, actorUserID string, now time.Time,
) (func(context.Context) error, error) {
	ids, hashes, err := m.store.RevokeAllForUser(ctx, tx, userID, now)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return func(context.Context) error { return nil }, nil
	}

	if err := m.audit.Write(ctx, tx, audit.Event{
		OrgID:       orgID,
		ActorUserID: actorUserID,
		Type:        audit.EventSessionRevoked,
		Payload: map[string]any{
			"session_ids": ids,
			"user_id":     userID,
			"reason":      string(ReasonLogoutAll),
			"count":       len(ids),
		},
	}); err != nil {
		return nil, fmt.Errorf("session: auditing revocation: %w", err)
	}

	// A far-future expiry for the tombstones: the individual expiries are not
	// worth a query each, and a tombstone that outlives its session costs one
	// short Redis key.
	until := now.Add(MaxAbsoluteLifetime)

	return func(ctx context.Context) error {
		var failed error
		for _, hash := range hashes {
			if err := m.cache.Invalidate(ctx, hash, until); err != nil {
				failed = err
			}
		}
		return failed
	}, nil
}

// hashFor reads a session's token hash, for cache invalidation.
//
// Kept off the Session struct on purpose (PG-14): no read path returns a
// credential-shaped value, so this is the one place that asks for it and it
// does not travel further than the invalidation call.
func (m *Manager) hashFor(ctx context.Context, tx *postgres.Tx, sessionID string) string {
	var hash string
	_ = tx.QueryRow(ctx,
		`SELECT COALESCE(token_hash, '') FROM sessions WHERE id = $1`, sessionID).Scan(&hash)
	return hash
}

// Sweep removes sessions that can no longer be used.
//
// `retain` is how long a revoked or expired session is kept before deletion:
// the sessions screen shows it to explain why a user was signed out, and an
// incident investigation reads it.
func (m *Manager) Sweep(ctx context.Context, retain time.Duration, limit int, now time.Time) (int, error) {
	return m.store.Sweep(ctx, m.db, now.Add(-retain), limit)
}

// IsLive reports whether a session is still usable, by its id.
//
// By id rather than by token, because the caller — P1-07's refresh grant —
// holds a refresh token that records which session authorised it and never
// the session's cookie. That is PG-14's separation of credential from
// identifier paying off in a second place: the id is safe to store on another
// row and to ask about later.
func (m *Manager) IsLive(ctx context.Context, sessionID string, now time.Time) bool {
	if sessionID == "" {
		return false
	}

	var live bool
	err := m.db.SQL().QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM session_live($1, $2)
		)`, sessionID, now).Scan(&live)
	if err != nil {
		// Fail closed: an unverifiable session is not a live one. The cost is
		// a re-login, which is recoverable; the alternative is honouring a
		// refresh token for a session that may have been revoked.
		if m.log != nil {
			m.log.Warn("checking session liveness failed", "session_id", sessionID, "error", err.Error())
		}
		return false
	}
	return live
}

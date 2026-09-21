package saml

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// An AuthnRequest is answered once (P4-08 C-3, A-3).
//
// The correlation an SP-initiated login rests on is `InResponseTo`: the service
// provider knows the assertion answers a login IT started, rather than one an
// attacker started in the user's browser. That is only worth something if the
// request is answered once — an id that can be answered twice is an assertion an
// attacker can have re-issued.
//
// Same shape as the assertion replay store, for the same reason: the guarantee
// belongs to the database, not to a sequence of statements that can interleave.

// PendingLifetime is how long an AuthnRequest waits for its answer.
//
// Fifteen minutes. Long enough for a user to authenticate, including a second
// factor, a password reset prompt, and a moment of confusion; short enough that
// a captured request is not usable tomorrow. It bounds the table as well as the
// window.
const PendingLifetime = 15 * time.Minute

var (
	// ErrNoSuchRequest is an InResponseTo naming nothing.
	ErrNoSuchRequest = errors.New("saml: no such AuthnRequest")

	// ErrAlreadyAnswered is a second answer to one request.
	//
	// Distinct from ErrNoSuchRequest on purpose: an operator reading the log
	// needs to tell a replay from a typo, and the two have different responses.
	ErrAlreadyAnswered = errors.New("saml: the AuthnRequest has already been answered")
)

// Pending is one AuthnRequest waiting for its answer.
type Pending struct {
	ID         string
	SPID       string
	RelayState string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// Requests records and consumes AuthnRequests.
type Requests struct{}

// NewRequests returns a Requests.
func NewRequests() *Requests { return &Requests{} }

// Record stores an AuthnRequest before the user is sent to authenticate.
//
// A duplicate id is refused rather than overwritten. A service provider reusing
// an id is either broken or replaying, and silently accepting the second one
// would let a replayed request inherit a fresh window.
func (r *Requests) Record(
	ctx context.Context, tx *postgres.Tx, orgID string, p Pending,
) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("saml: the AuthnRequest has no id")
	}
	if !p.ExpiresAt.After(p.CreatedAt) {
		return fmt.Errorf("saml: the AuthnRequest expires before it was made")
	}

	var relay any
	if p.RelayState != "" {
		relay = p.RelayState
	}

	result, err := tx.Exec(ctx, `
		INSERT INTO saml_authn_requests (id, org_id, sp_id, relay_state, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING`,
		p.ID, orgID, p.SPID, relay, p.CreatedAt, p.ExpiresAt)
	if err != nil {
		return fmt.Errorf("saml: recording the AuthnRequest: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("saml: recording the AuthnRequest: %w", err)
	}
	if affected == 0 {
		return ErrAlreadyAnswered
	}
	return nil
}

// Consume marks a request answered and returns what it carried.
//
// One statement. `UPDATE … WHERE consumed_at IS NULL … RETURNING` is what makes
// this single-use: the row is claimed and read together, so two answers arriving
// at once cannot both find it unclaimed. A `SELECT` followed by an `UPDATE`
// would have a window between them, and an attacker replaying a captured
// request chooses exactly when to arrive.
//
// The expiry is part of the same condition rather than a check afterwards. A
// request consumed and then found to be expired has already been marked
// answered, which turns a stale login into an unexplained "already answered" on
// the user's next attempt.
func (r *Requests) Consume(
	ctx context.Context, tx *postgres.Tx, requestID string, now time.Time,
) (Pending, error) {
	if strings.TrimSpace(requestID) == "" {
		return Pending{}, ErrNoSuchRequest
	}

	var (
		p     Pending
		relay sql.NullString
	)
	err := tx.QueryRow(ctx, `
		UPDATE saml_authn_requests
		   SET consumed_at = $2
		 WHERE id = $1
		   AND consumed_at IS NULL
		   AND expires_at > $2
		RETURNING id, sp_id::text, relay_state, created_at, expires_at`,
		requestID, now).Scan(&p.ID, &p.SPID, &relay, &p.CreatedAt, &p.ExpiresAt)

	if errors.Is(err, sql.ErrNoRows) {
		// Nothing matched. Which of the three reasons is worth telling apart,
		// because they mean different things to an operator: never existed,
		// already answered, or expired.
		return Pending{}, r.explain(ctx, tx, requestID, now)
	}
	if err != nil {
		return Pending{}, fmt.Errorf("saml: consuming the AuthnRequest: %w", err)
	}
	p.RelayState = relay.String
	return p, nil
}

// explain says why a consume matched nothing.
//
// A second query, run only on the failure path, so the common case stays one
// statement. It cannot itself be raced into a wrong answer that matters: every
// outcome here is a refusal, and the refusals differ only in their message.
func (r *Requests) explain(ctx context.Context, tx *postgres.Tx, requestID string, now time.Time) error {
	var consumed sql.NullTime
	var expires time.Time
	err := tx.QueryRow(ctx,
		`SELECT consumed_at, expires_at FROM saml_authn_requests WHERE id = $1`, requestID).
		Scan(&consumed, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSuchRequest
	}
	if err != nil {
		return fmt.Errorf("saml: consuming the AuthnRequest: %w", err)
	}
	if consumed.Valid {
		return ErrAlreadyAnswered
	}
	if !expires.After(now) {
		return fmt.Errorf("%w: it expired at %s", ErrNoSuchRequest, expires.Format(time.RFC3339))
	}
	// Matched nothing, exists, unconsumed and unexpired: another transaction
	// claimed it between the update and this read, which is a replay that lost
	// the race.
	return ErrAlreadyAnswered
}

// Prune removes requests that can no longer be answered.
func (r *Requests) Prune(ctx context.Context, tx *postgres.Tx, now time.Time) (int64, error) {
	result, err := tx.Exec(ctx, `DELETE FROM saml_authn_requests WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("saml: pruning AuthnRequests: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("saml: pruning AuthnRequests: %w", err)
	}
	return affected, nil
}

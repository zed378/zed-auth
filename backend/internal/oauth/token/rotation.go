package token

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Refresh-token rotation and reuse detection (P3-06).
//
// Specification: MEMORY/specs/P3-06-refresh-rotation.md.
//
// `docs/PLAN/17`'s Phase 3 criterion is one sentence — a rotated refresh token
// cannot be reused — and the hard part is not detecting reuse. It is telling
// reuse apart from a client that retried after a network timeout.
//
// Get that wrong in the strict direction and real users are logged out
// constantly, which trains an operations team to disable the protection. That
// outcome is worse than not having built it: the control is gone AND everybody
// believes it is there. `RotationGrace` below exists for exactly that, and its
// size is argued rather than picked.

// RotationGrace is how long the immediately-preceding token stays usable after
// it has been rotated (card step 5).
//
// **Thirty seconds.** The window bounds one specific situation: a client sent a
// refresh, this service rotated and answered, and the answer did not arrive —
// a dropped connection, a proxy timeout, a process killed mid-response. The
// client still holds the old token and will retry with it, because that is the
// only token it has.
//
// Thirty seconds is longer than the gap between a failed request and its retry
// (which is immediate, or one backoff step) and shorter than any human-scale
// interval. Longer would widen the period in which a stolen token works;
// shorter would start logging out clients on slow networks, which is the
// failure that gets the whole control switched off.
//
// The window is not the only condition. A retry is admitted ONLY when the
// replacement is untouched — see `Lineage.LegitimateRetry`.
const RotationGrace = 30 * time.Second

// ErrRefreshReuse is a token presented after it was rotated away.
//
// It means one of two things: the token was stolen, or a client is misbehaving.
// **The safe action is the same for both**, which is why they are not
// distinguished — revoke the whole family.
var ErrRefreshReuse = errors.New("token: a rotated refresh token was presented again")

// Lineage is what happened to a presented token.
type Lineage struct {
	ID       string
	OrgID    string
	FamilyID string

	Revoked bool

	// ReplacedBy is the token that superseded this one, if any.
	ReplacedBy string

	// ReplacedAt is when that happened — read from the replacement's own row,
	// so there is no second column to disagree with the link.
	ReplacedAt time.Time

	// SuccessorSpent is whether the replacement has itself been used or
	// revoked.
	SuccessorSpent bool
}

// Rotated reports whether this token has been superseded.
func (l Lineage) Rotated() bool { return l.ReplacedBy != "" }

// LegitimateRetry reports whether presenting this rotated token again is a
// client retrying rather than an attacker replaying.
//
// **Both conditions, and the second is the one that matters.**
//
// The time window alone would be a thirty-second hole: an attacker who
// intercepts a token has every reason to use it immediately, so "recently
// rotated" describes the theft case at least as well as the retry case.
//
// What actually separates them is whether the REPLACEMENT was ever used. A
// retry happens because the replacement never arrived — so it is sitting
// untouched. If the replacement has been used, the client did receive it, and a
// presentation of the old token is somebody else holding a copy.
func (l Lineage) LegitimateRetry(now time.Time, grace time.Duration) bool {
	if !l.Rotated() || l.Revoked || l.SuccessorSpent {
		return false
	}
	if l.ReplacedAt.IsZero() {
		return false
	}
	return now.Sub(l.ReplacedAt) <= grace
}

// LookupLineage reports what happened to a presented token, live or not.
//
// Through `refresh_token_lineage`, a second SECURITY DEFINER function rather
// than a relaxation of `refresh_token_by_hash`: the liveness filter on the
// happy path is load-bearing, and this question cannot be answered without
// seeing past it.
func (s *RefreshStore) LookupLineage(
	ctx context.Context, db *postgres.DB, presented string,
) (Lineage, error) {
	var (
		l          Lineage
		replacedBy sql.NullString
		replacedAt sql.NullTime
	)

	err := db.SQL().QueryRowContext(ctx, `
		SELECT id, org_id, family_id, revoked, replaced_by, replaced_at, successor_spent
		  FROM refresh_token_lineage($1)`,
		HashRefresh(presented),
	).Scan(&l.ID, &l.OrgID, &l.FamilyID, &l.Revoked, &replacedBy, &replacedAt, &l.SuccessorSpent)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Not a token this service ever issued. NOT reuse — answering "reuse"
		// here would let anybody kill a family by guessing, and there is no
		// family to kill anyway.
		return Lineage{}, ErrRefreshNotFound
	case err != nil:
		return Lineage{}, fmt.Errorf("token: reading refresh lineage: %w", err)
	}

	l.ReplacedBy = replacedBy.String
	l.ReplacedAt = replacedAt.Time
	return l, nil
}

// Rotate issues a successor and links the presented token to it.
//
// **One transaction, and the link is written in it.** A successor issued
// without the link would be a live token whose predecessor still works, which
// is the state reuse detection exists to make impossible.
//
// The family's absolute expiry is INHERITED rather than recomputed. That is the
// whole of card step 6's "absolute family lifetime": recomputing it on every
// rotation is how continuous refreshing extends a session forever, and it is a
// one-word difference from doing it correctly.
// `supersede` names a successor that this rotation is REPLACING — set only on
// the legitimate-retry path, where the presented token was already rotated and
// the successor never reached the client. Empty on the ordinary path, where a
// second rotation of one predecessor is reuse.
func (s *RefreshStore) Rotate(
	ctx context.Context, tx *postgres.Tx, presented Refresh, supersede string, now time.Time,
) (RefreshToken, error) {
	var familyExpires time.Time
	err := tx.QueryRow(ctx,
		`SELECT family_expires_at FROM refresh_tokens WHERE id = $1`,
		presented.ID).Scan(&familyExpires)
	if err != nil {
		return RefreshToken{}, fmt.Errorf("token: reading the family's expiry: %w", err)
	}

	if !familyExpires.After(now) {
		// The family has aged out between the lookup and here. Refused rather
		// than rotated: a successor would carry an expiry already past, and
		// `refresh_tokens_family_outlives_token` would refuse the insert with a
		// constraint error that reads like a bug.
		return RefreshToken{}, ErrRefreshNotFound
	}

	successor, err := s.issueInFamily(ctx, tx, presented, familyExpires, now)
	if err != nil {
		return RefreshToken{}, err
	}

	if supersede != "" {
		// The retry path. The successor the client never received is revoked
		// before the link moves, so the family never holds two live successors
		// — if the lost response DID eventually arrive, that token is already
		// dead and its presentation is an ordinary refusal rather than a
		// family kill.
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked = true WHERE id = $1`, supersede); err != nil {
			return RefreshToken{}, fmt.Errorf("token: revoking a superseded successor: %w", err)
		}
	}

	// The link is conditional on the predecessor still pointing where this
	// rotation expects. On the ordinary path that is NULL, so two concurrent
	// rotations cannot both win; on the retry path it is the successor being
	// replaced, so a retry cannot race another retry either.
	var result sql.Result
	if supersede == "" {
		result, err = tx.Exec(ctx, `
			UPDATE refresh_tokens
			   SET replaced_by = $2
			 WHERE id = $1
			   AND replaced_by IS NULL`,
			presented.ID, successor.id)
	} else {
		result, err = tx.Exec(ctx, `
			UPDATE refresh_tokens
			   SET replaced_by = $2
			 WHERE id = $1
			   AND replaced_by = $3`,
			presented.ID, successor.id, supersede)
	}
	if err != nil {
		return RefreshToken{}, fmt.Errorf("token: linking a rotated refresh token: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return RefreshToken{}, fmt.Errorf("token: linking a rotated refresh token: %w", err)
	}
	if affected == 0 {
		// Another request rotated this token first. The conditional `WHERE`
		// above is what makes that a losable race rather than two successors
		// from one predecessor — and losing it is reuse, because the other
		// request already holds the successor.
		//
		// **This is also a second, independent refusal of reuse**, and a
		// mutation run made that visible: disabling detection in the handler
		// entirely left `TestARotatedRefreshTokenCannotBeReused` green, because
		// a rotated token still cannot rotate again. What detection adds on top
		// is the FAMILY KILL and the alert — which is why those are what the
		// mutation list targets, rather than the refusal they sit behind.
		return RefreshToken{}, ErrRefreshReuse
	}

	return successor.token, nil
}

// issued is a freshly stored token and its row id.
type issued struct {
	token RefreshToken
	id    string
}

// issueInFamily stores a successor carrying its family's original expiry.
func (s *RefreshStore) issueInFamily(
	ctx context.Context, tx *postgres.Tx, in Refresh, familyExpires time.Time, now time.Time,
) (issued, error) {
	plaintext, err := newRefreshPlaintext()
	if err != nil {
		return issued{}, err
	}

	var sessionID any
	if in.SessionID != "" {
		sessionID = in.SessionID
	}

	// The token's own expiry is capped by the family's. Without the cap a
	// successor issued shortly before the family ages out would claim a
	// lifetime reaching past it, and `refresh_tokens_family_outlives_token`
	// would refuse the insert.
	expires := now.Add(RefreshTokenLifetime)
	if expires.After(familyExpires) {
		expires = familyExpires
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens
			(user_id, client_id, org_id, session_id, family_id, token_hash,
			 scope, expires_at, family_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		in.UserID, in.ClientID, in.OrgID, sessionID, in.FamilyID,
		HashRefresh(plaintext), scopeOrEmpty(in.Scope), expires, familyExpires,
	).Scan(&id)
	if err != nil {
		return issued{}, fmt.Errorf("token: storing a rotated refresh token: %w", err)
	}

	return issued{token: RefreshToken{plaintext: plaintext}, id: id}, nil
}

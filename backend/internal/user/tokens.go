package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Invitation and password-reset tokens (P1-19.4, P1-19.5).
//
// One table with a `purpose` discriminator, as P0-07 shaped it: both flows have
// the same security properties — single-use, short-lived, stored hashed — and
// the same abuse surface, so two tables would be two places to get the same
// thing right.

// Purposes, matching the user_tokens_purpose_valid CHECK.
const (
	PurposeInvite = "invite"
	PurposeReset  = "password_reset"

	// PurposeReportNotMe is the link in a login-anomaly notice (P3-08). It
	// grants one power — signing the owner out everywhere and sending them a
	// reset link — and can neither sign anybody in nor set a password.
	PurposeReportNotMe = "report_not_me"
)

// SetsPassword reports whether a token of this purpose may be used to set a
// password.
//
// **An allow-list, not a deny-list, and that is the entire point.** The
// set-password page used to consume whatever purpose a token carried, which was
// harmless while every issued purpose legitimately set passwords. It stopped
// being harmless the moment a token with a DIFFERENT power existed: P3-08's
// `report_not_me` link lives in an email for a week, and without this check
// anybody holding one could open `/password/set` with it and take the account —
// the precise power that link was designed never to have.
//
// Found while building that flow, before any such token was issued. A deny-list
// would have repeated the mistake the next time a purpose was added; an
// allow-list makes a new purpose unable to set a password until somebody decides
// in writing that it should.
func SetsPassword(purpose string) bool {
	switch purpose {
	case PurposeInvite, PurposeReset:
		return true
	default:
		return false
	}
}

// Lifetimes.
//
// An invitation gets three days because it is sent to somebody who may not be
// at their desk, and re-inviting is an administrator's time rather than the
// user's. A reset gets one hour because the person requesting it is, by
// definition, sitting there waiting for it — a longer window buys them nothing
// and leaves a credential live in a mailbox.
const (
	InviteLifetime = 72 * time.Hour
	ResetLifetime  = time.Hour

	// ReportNotMeLifetime is a week, unlike a reset's hour, because the person
	// reading an anomaly notice did not ask for it and may open it days later.
	// Its power is narrow enough that a longer life costs little: the worst a
	// stolen link can do is sign the real owner out and email them a reset.
	ReportNotMeLifetime = 7 * 24 * time.Hour
)

// tokenBytes is the entropy in a token.
//
// 32 bytes, the same as a client secret and a session token. At that size
// guessing is not a threat model, which is also why the stored form is a plain
// SHA-256 rather than a slow KDF — ADR-016's reasoning, arriving in a second
// place: a slow hash here would be self-inflicted amplification on a path an
// anonymous caller can reach.
const tokenBytes = 32

// ErrTokenInvalid means the token does not exist, has expired, or was used.
//
// **One error for all three.** Telling them apart would let a holder of an
// expired token learn that it was once real, and would let anybody with a
// guess learn whether it exists.
var ErrTokenInvalid = errors.New("user: the link is not valid")

// Token is an issued credential, held only long enough to put in an email.
type Token struct {
	// Plaintext exists in memory and in the message. It is never stored, never
	// logged and never returned by an API.
	Plaintext string

	UserID    string
	Purpose   string
	ExpiresAt time.Time
}

// HashToken is the stored form.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// IssueToken creates a single-use credential for a user.
//
// **Every unused token of the same purpose is consumed first.** Issuing a
// second reset link without retiring the first would leave two live
// credentials for one account, and the one the user did not ask for is the one
// nobody is watching. It also makes "I clicked the old link" a clean failure
// rather than a silent success on a stale request.
func (s *Store) IssueToken(
	ctx context.Context, tx *postgres.Tx, userID, purpose string, lifetime time.Duration, now time.Time,
) (Token, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, fmt.Errorf("user: generating a token: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)

	if _, err := tx.Exec(ctx, `
		UPDATE user_tokens SET used_at = $3
		 WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`,
		userID, purpose, now); err != nil {
		return Token{}, fmt.Errorf("user: retiring previous tokens: %w", err)
	}

	orgID := tx.OrgID()
	if orgID == "" {
		return Token{}, fmt.Errorf("user: issuing a token without a tenant scope")
	}

	expires := now.Add(lifetime)
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_tokens (user_id, org_id, purpose, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, orgID, purpose, HashToken(plaintext), expires, now); err != nil {
		return Token{}, fmt.Errorf("user: storing a token: %w", err)
	}

	return Token{Plaintext: plaintext, UserID: userID, Purpose: purpose, ExpiresAt: expires}, nil
}

// Claim is what a valid token resolves to.
type Claim struct {
	UserID  string
	OrgID   string
	Purpose string
}

// LookupToken resolves a token WITHOUT consuming it.
//
// For rendering the set-password form: the person has clicked a link and has
// not yet chosen a password, so consuming it here would burn the credential on
// a page view and leave them unable to submit the form they are looking at.
//
// Runs through `user_token_by_hash`, a SECURITY DEFINER function, because the
// caller is anonymous and the TOKEN is what names the organization — the same
// bootstrap problem P1-11's session cookie has, answered the same way. Instance
// scope would not do: `current_org_id()` is NULL there, so the tenant policy is
// false for every row.
//
// Safe precisely because the only way in is a 256-bit secret: there is no id to
// enumerate and no filter to bypass.
func (s *Store) LookupToken(ctx context.Context, db *postgres.DB, plaintext string, now time.Time) (Claim, error) {
	if plaintext == "" {
		return Claim{}, ErrTokenInvalid
	}

	var c Claim
	err := db.WithInstanceScope(ctx, "resolving an invite or reset link before its tenant is known",
		func(tx *postgres.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT user_id, org_id, purpose FROM user_token_by_hash($1, $2)`,
				HashToken(plaintext), now).Scan(&c.UserID, &c.OrgID, &c.Purpose)
		})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Claim{}, ErrTokenInvalid
	case err != nil:
		return Claim{}, fmt.Errorf("user: looking up a token: %w", err)
	}
	return c, nil
}

// ConsumeToken marks a token used and returns who it belongs to.
//
// **A single conditional UPDATE, not a read followed by a write.** Two
// requests arriving with the same token at the same moment would both pass a
// read-then-check, and both would set a password — the second one silently
// overwriting the first, on an account neither party fully controls. Here the
// database decides: `WHERE used_at IS NULL ... RETURNING` produces a row for
// exactly one of them.
func (s *Store) ConsumeToken(
	ctx context.Context, tx *postgres.Tx, plaintext, purpose string, now time.Time,
) (Claim, error) {
	if plaintext == "" {
		return Claim{}, ErrTokenInvalid
	}

	var c Claim
	err := tx.QueryRow(ctx, `
		UPDATE user_tokens
		   SET used_at = $2
		 WHERE token_hash = $1
		   AND purpose = $3
		   AND used_at IS NULL
		   AND expires_at > $2
		RETURNING user_id, org_id, purpose`,
		HashToken(plaintext), now, purpose).Scan(&c.UserID, &c.OrgID, &c.Purpose)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Claim{}, ErrTokenInvalid
	case err != nil:
		return Claim{}, fmt.Errorf("user: consuming a token: %w", err)
	}
	return c, nil
}

// RetireTokens consumes every unused token for a user.
//
// Called when an account is deactivated: a live invitation or reset link for a
// deactivated user is a way back in that nobody is looking at.
func (s *Store) RetireTokens(ctx context.Context, tx *postgres.Tx, userID string, now time.Time) (int64, error) {
	result, err := tx.Exec(ctx,
		`UPDATE user_tokens SET used_at = $2 WHERE user_id = $1 AND used_at IS NULL`, userID, now)
	if err != nil {
		return 0, fmt.Errorf("user: retiring tokens: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

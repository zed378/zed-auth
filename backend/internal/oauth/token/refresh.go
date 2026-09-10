package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Refresh tokens.
//
// **Opaque, not a JWT, and that is a decision rather than a default.**
//
// A refresh token must be revocable, so it must be looked up — which a
// stateless JWT would defeat. But there is a second reason, and it is the one
// worth writing down: P1-03 found that sixteen distinct base64url encodings of
// one RSA signature all verify. That makes a JWT's string form non-canonical,
// so reuse detection keyed on the token string can be defeated by mutating a
// single character. P3-06 will build reuse detection on top of this, and an
// opaque random value has exactly one representation — its SHA-256 is
// canonical, and the trap cannot be walked into.
//
// **Phase 1 does not rotate.** The card assigns rotation and reuse detection
// to P3-06. The columns are populated so that phase changes behaviour rather
// than storage: every issuance starts its own family with a bounded absolute
// lifetime, and `replaced_by` stays null until there is something to point at.

// ErrRefreshNotFound means the token is unknown, expired, revoked, or its
// family has aged out. One answer for all of them: distinguishing would tell a
// holder of a dead token that it was once real.
var ErrRefreshNotFound = errors.New("token: refresh token not found")

const refreshBytes = 32

// RefreshToken is a plaintext refresh token on its way to the client.
//
// The same redacting type as client.Secret and session.Token, for the same
// reason and with the same lesson behind it: P1-05 found that fmt.Stringer
// alone leaks under %d, so Formatter covers every verb.
type RefreshToken struct{ plaintext string }

func (t RefreshToken) Format(f fmt.State, verb rune) { _, _ = io.WriteString(f, "[REDACTED]") }
func (t RefreshToken) String() string                { return "[REDACTED]" }
func (t RefreshToken) GoString() string              { return "token.RefreshToken{[REDACTED]}" }
func (t RefreshToken) Reveal() string                { return t.plaintext }
func (t RefreshToken) IsZero() bool                  { return t.plaintext == "" }

func (t RefreshToken) MarshalJSON() ([]byte, error) {
	return nil, errors.New("token: a RefreshToken must not be marshalled directly; " +
		"the token response builds its own body")
}

// HashRefresh returns the stored form.
//
// Unsalted SHA-256, per ADR-016: 256 bits of entropy settles brute force, and
// this is looked up on the token endpoint's hot path.
func HashRefresh(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Refresh is a stored refresh token.
type Refresh struct {
	ID        string
	UserID    string
	ClientID  string
	OrgID     string
	SessionID string
	FamilyID  string
	Scope     []string
	ExpiresAt time.Time
}

// RefreshStore persists refresh tokens.
type RefreshStore struct{}

func NewRefreshStore() *RefreshStore { return &RefreshStore{} }

// Issue creates a refresh token and returns its plaintext once.
//
// `familyID` empty starts a new family. P3-06 will pass an existing one when
// rotating, which is why the parameter exists before anything uses it.
func (s *RefreshStore) Issue(
	ctx context.Context, tx *postgres.Tx, in Refresh, familyID string, now time.Time,
) (RefreshToken, string, error) {
	buf := make([]byte, refreshBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return RefreshToken{}, "", fmt.Errorf("token: generating refresh token: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)

	expires := now.Add(RefreshTokenLifetime)
	familyExpires := now.Add(FamilyLifetime)

	var sessionID any
	if in.SessionID != "" {
		sessionID = in.SessionID
	}
	var family any
	if familyID != "" {
		family = familyID
	}

	var id, storedFamily string
	err := tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens
			(user_id, client_id, org_id, session_id, family_id, token_hash,
			 scope, expires_at, family_expires_at)
		VALUES ($1, $2, $3, $4, COALESCE($5::uuid, gen_random_uuid()), $6, $7, $8, $9)
		RETURNING id, family_id`,
		in.UserID, in.ClientID, in.OrgID, sessionID, family,
		HashRefresh(plaintext), scopeOrEmpty(in.Scope), expires, familyExpires,
	).Scan(&id, &storedFamily)
	if err != nil {
		return RefreshToken{}, "", fmt.Errorf("token: storing refresh token: %w", err)
	}

	return RefreshToken{plaintext: plaintext}, storedFamily, nil
}

// Lookup resolves a presented refresh token, before a tenant is known.
//
// Through refresh_token_by_hash, a SECURITY DEFINER function, for the same
// reason sessions and client_id needed one: the token endpoint receives a bare
// refresh token and must discover which tenant it belongs to before it can
// scope anything, and `refresh_tokens_tenant_isolation` matches nothing with
// no tenant set.
//
// Liveness is filtered inside the function — revoked, expired, or family aged
// out — so a dead token is not merely reported dead, it is not returned, and
// cannot be acted on by a caller that forgot to check.
func (s *RefreshStore) Lookup(
	ctx context.Context, db *postgres.DB, presented string, now time.Time,
) (Refresh, error) {
	var (
		r         Refresh
		sessionID sql.NullString
		scope     jsonStrings
	)

	err := db.SQL().QueryRowContext(ctx, `
		SELECT id, user_id, client_id, org_id, session_id, family_id, scope, expires_at
		  FROM refresh_token_by_hash($1, $2)`,
		HashRefresh(presented), now,
	).Scan(&r.ID, &r.UserID, &r.ClientID, &r.OrgID, &sessionID, &r.FamilyID, &scope, &r.ExpiresAt)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Refresh{}, ErrRefreshNotFound
	case err != nil:
		return Refresh{}, fmt.Errorf("token: looking up refresh token: %w", err)
	}

	r.SessionID = sessionID.String
	r.Scope = scope
	return r, nil
}

// jsonStrings scans a JSON array of strings, always yielding a non-nil slice.
//
// The array column travels out as JSON for the reason internal/oauth/client
// and internal/session both found: database/sql hands a text[] back as a raw
// Postgres array literal and there is no standard scanner for it.
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
		return fmt.Errorf("token: cannot scan %T into a string array", src)
	}

	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("token: decoding scope: %w", err)
	}
	if out != nil {
		*j = out
	}
	return nil
}

// scopeOrEmpty keeps a nil slice from binding as SQL NULL against a NOT NULL
// column — the same trap internal/oauth/client hit.
func scopeOrEmpty(scope []string) []string {
	if scope == nil {
		return []string{}
	}
	return scope
}

// Revoke marks one refresh token unusable.
func (s *RefreshStore) Revoke(ctx context.Context, tx *postgres.Tx, id string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE id = $1`, id); err != nil {
		return fmt.Errorf("token: revoking refresh token: %w", err)
	}
	return nil
}

// RevokeFamily marks an entire lineage unusable.
//
// Unused in Phase 1 and present because P3-06's reuse detection is exactly
// this call: presenting a token that has already been replaced revokes
// everything descended from the same original issuance. Writing it now costs
// nothing and means that phase adds a trigger rather than a mechanism.
func (s *RefreshStore) RevokeFamily(ctx context.Context, tx *postgres.Tx, familyID string) (int64, error) {
	result, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE family_id = $1 AND NOT revoked`, familyID)
	if err != nil {
		return 0, fmt.Errorf("token: revoking refresh family: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// RevokeForSessionAndClient revokes every live refresh token issued to one
// client for one session.
//
// This is what /oauth/revoke does when it is handed an ACCESS token. RFC 7009
// §2.1 says presenting one SHOULD invalidate the refresh token behind it, and
// an access token names exactly the pair a refresh token was issued against:
// its session and its client.
//
// Both columns, never just the session. A session commonly backs several
// applications — that is what single sign-on IS — so revoking by session alone
// would let one client's logout throw away every other application's refresh
// token, which is a denial of service one integrator can inflict on the rest
// of an estate by calling a documented endpoint correctly.
func (s *RefreshStore) RevokeForSessionAndClient(
	ctx context.Context, tx *postgres.Tx, sessionID, clientID string,
) (int64, error) {
	if sessionID == "" || clientID == "" {
		// A client_credentials token has no session. Refusing to build a
		// predicate out of an empty string keeps this from quietly matching
		// every row with a NULL session_id.
		return 0, nil
	}

	result, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		   SET revoked = true
		 WHERE session_id = $1 AND client_id = $2 AND NOT revoked`, sessionID, clientID)
	if err != nil {
		return 0, fmt.Errorf("token: revoking refresh tokens for a session: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// RevokeAllForUser revokes every live refresh token a user holds.
//
// The other half of "log out of all sessions" (P1-10 step 4). Ending every
// session without this leaves the user with live refresh tokens, and an
// application holding one mints a fresh access token minutes later — so the
// button would end the browser sessions and quietly leave every integration
// signed in, which is not what anybody pressing it means.
//
// Tenant-scoped like every other write here, so RLS confines it to one
// organization without this query carrying an org_id predicate somebody could
// forget.
func (s *RefreshStore) RevokeAllForUser(
	ctx context.Context, tx *postgres.Tx, userID string,
) (int64, error) {
	if userID == "" {
		return 0, nil
	}

	result, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE user_id = $1 AND NOT revoked`, userID)
	if err != nil {
		return 0, fmt.Errorf("token: revoking every refresh token for a user: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

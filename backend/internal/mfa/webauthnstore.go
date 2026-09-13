package mfa

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// WebAuthn credentials in `user_mfa_factors` (P3-05).
//
// The same table as TOTP, which is `P3-01`'s decision and still the right one:
// two tables would mean two challenge steps, two rate limits and two audit
// shapes. The columns this type uses — `credential_id`, `public_key`,
// `sign_count` — were added by `20260912000028` because `docs/PLAN/04` names
// them, and until now nothing wrote to them.
//
// **There is no secret here.** `secret_encrypted` stays null for this factor
// type, and that is a genuine advantage worth stating rather than a gap: a
// database read that yields TOTP secrets yields every second factor at once,
// while one that yields WebAuthn public keys yields public keys.

// WebAuthnStore reads and writes registered authenticators.
type WebAuthnStore struct{}

func NewWebAuthnStore() *WebAuthnStore { return &WebAuthnStore{} }

// Credentials reads every WebAuthn factor a user holds.
//
// Both pending and active, with the status returned rather than filtered: the
// login ceremony wants active ones only, and the registration ceremony wants
// all of them so it can exclude an authenticator that is already enrolled.
// Filtering here would have made the second impossible without a second query.
func (s *WebAuthnStore) Credentials(
	ctx context.Context, tx *postgres.Tx, userID string,
) ([]StoredCredential, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, credential_id, public_key, COALESCE(sign_count, 0), status,
		       COALESCE(label, ''), COALESCE(data->>'user_verified', 'false')
		  FROM user_mfa_factors
		 WHERE user_id = $1
		   AND type = $2
		   AND credential_id IS NOT NULL
		 ORDER BY created_at`, userID, string(TypeWebAuthn))
	if err != nil {
		return nil, fmt.Errorf("mfa: reading WebAuthn credentials: %w", err)
	}
	defer rows.Close()

	var out []StoredCredential
	for rows.Next() {
		var (
			c            StoredCredential
			credentialID string
			publicKey    string
			signCount    int64
			verified     string
		)
		if err := rows.Scan(&c.FactorID, &credentialID, &publicKey, &signCount,
			&c.Status, &c.Label, &verified); err != nil {
			return nil, fmt.Errorf("mfa: reading a WebAuthn credential: %w", err)
		}

		// Both columns are `text` — `docs/PLAN/04` names them that way — so both
		// hold base64url.
		//
		// For `credential_id` that is also what the browser uses, so a log line
		// and a devtools panel compare directly. For `public_key` it is a
		// necessity rather than a nicety: a COSE key is CBOR, which is bytes
		// and is never valid UTF-8, so writing it to a text column raw would be
		// rejected by Postgres or silently mangled by a driver that tried.
		raw, err := decodeCredentialID(credentialID)
		if err != nil {
			return nil, fmt.Errorf("mfa: a stored credential id is not base64url: %w", err)
		}
		key, err := decodeCredentialID(publicKey)
		if err != nil {
			return nil, fmt.Errorf("mfa: a stored public key is not base64url: %w", err)
		}

		c.CredentialID = raw
		c.PublicKey = key
		c.SignCount = uint32(signCount) // #nosec G115 -- bounded on write; see RecordSignCount.
		c.UserVerified = verified == "true"
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mfa: reading WebAuthn credentials: %w", err)
	}
	return out, nil
}

// InsertCredential stores a proven registration.
func (s *WebAuthnStore) InsertCredential(
	ctx context.Context, tx *postgres.Tx, in NewCredential, now time.Time,
) (string, error) {
	// What is neither secret nor indexed. The AAGUID identifies the
	// authenticator MODEL, which an operator needs when a vendor's firmware
	// turns out to be broken; the transports tell a browser whether to look at
	// USB, NFC or the platform.
	data, err := json.Marshal(map[string]any{
		"aaguid":        EncodeCredentialID(in.AAGUID),
		"transports":    in.Transports,
		"user_verified": in.UserVerified,
	})
	if err != nil {
		return "", fmt.Errorf("mfa: encoding credential data: %w", err)
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO user_mfa_factors
		    (user_id, org_id, type, label, credential_id, public_key, sign_count,
		     data, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, $9)
		RETURNING id`,
		in.UserID, in.OrgID, string(TypeWebAuthn), nullableLabel(in.Label),
		EncodeCredentialID(in.CredentialID), EncodeCredentialID(in.PublicKey),
		int64(in.SignCount), data, now).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("mfa: storing a WebAuthn credential: %w", err)
	}
	return id, nil
}

// Activate makes a registered credential answerable.
func (s *WebAuthnStore) Activate(ctx context.Context, tx *postgres.Tx, factorID string) error {
	return (&Store{}).Activate(ctx, tx, factorID)
}

// Delete removes one credential (card step 3, F-3).
//
// Individually removable is the requirement, and it is what makes several
// credentials worth having: a user who loses one device removes that one and
// keeps the others, rather than resetting everything and re-enrolling.
func (s *WebAuthnStore) Delete(ctx context.Context, tx *postgres.Tx, factorID string) error {
	return (&Store{}).Delete(ctx, tx, factorID)
}

// RecordSignCount stores the counter an assertion reported.
//
// Unconditional, unlike `P3-02`'s replay bound, and the difference is the
// point: TOTP's `WHERE last_used_counter < $2` is what ENFORCES the bound,
// because a code is presented by the client and the database is the only place
// two simultaneous presentations can be serialised. A WebAuthn counter is
// compared in the verifier BEFORE this is called, against a value the
// authenticator signed — so by the time this runs the decision is made, and a
// conditional clause here would silently drop the write for an authenticator
// that legitimately reports zero every time.
func (s *WebAuthnStore) RecordSignCount(
	ctx context.Context, tx *postgres.Tx, factorID string, count uint32, at time.Time,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE user_mfa_factors
		   SET sign_count = $2, last_used_at = $3, updated_at = now()
		 WHERE id = $1`,
		// int64 because Postgres has no unsigned type. A uint32 cannot
		// overflow int64, so this is the boundary rather than a narrowing.
		factorID, int64(count), at)
	if err != nil {
		return fmt.Errorf("mfa: recording a signature counter: %w", err)
	}
	return nil
}

func nullableLabel(label string) any {
	if label == "" {
		return nil
	}
	return label
}

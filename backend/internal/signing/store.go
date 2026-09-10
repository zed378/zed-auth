package signing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/config"
)

// Purpose separates key sets that must not be shared.
//
// SAML assertion signing uses a different key set from OIDC tokens (P4-07),
// and the schema keeps them apart so that rotating one never touches the
// other. "One current per purpose" is a partial unique index, not a
// convention.
type Purpose string

const (
	PurposeOIDC Purpose = "oidc"
	PurposeSAML Purpose = "saml"
)

// SecretResolver reads a private key given its reference (P0-14).
type SecretResolver interface {
	Resolve(ref config.SecretRef) ([]byte, error)
}

// Store reads and writes signing keys.
//
// It takes a *sql.DB rather than the tenant-scoped storage wrapper on purpose:
// signing keys belong to the INSTANCE, not to an organization. The table has
// no org_id and no row-level security policy, because a key that were
// tenant-scoped could not sign a token for a user whose tenant is being
// determined by that very token.
type Store struct {
	db       *sql.DB
	resolver SecretResolver
	purpose  Purpose
}

// NewStore returns a Store for one purpose.
func NewStore(db *sql.DB, resolver SecretResolver, purpose Purpose) *Store {
	return &Store{db: db, resolver: resolver, purpose: purpose}
}

// Load reads every non-retired key and resolves the private key of the
// current one.
//
// Only the current key's private half is resolved. A `next` or `previous` key
// does not sign, so the process has no reason to hold material it is not
// allowed to use — and not holding it removes a way to use it by mistake.
func (s *Store) Load(ctx context.Context) (*KeySet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kid, algorithm, status, public_key, private_key_ref
		FROM signing_keys
		WHERE purpose = $1 AND status <> 'retired'
		ORDER BY kid`, string(s.purpose))
	if err != nil {
		return nil, fmt.Errorf("querying signing keys: %w", err)
	}
	defer rows.Close()

	var keys []*Key

	for rows.Next() {
		var kid, algorithm, status, publicPEM, privateRef string
		if err := rows.Scan(&kid, &algorithm, &status, &publicPEM, &privateRef); err != nil {
			return nil, fmt.Errorf("scanning signing key: %w", err)
		}

		alg := Algorithm(algorithm)
		if !alg.Valid() {
			return nil, fmt.Errorf("%w: key %q is recorded as %q",
				ErrUnsupportedAlgorithm, kid, algorithm)
		}

		public, err := ParsePublicKey([]byte(publicPEM))
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", kid, err)
		}

		// The algorithm in the row must match the key that is actually
		// stored. A mismatch means a row was edited by hand or a key was
		// replaced without its metadata, and signing with the wrong algorithm
		// for a key type fails in ways that are hard to read.
		derived, err := AlgorithmFor(public)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", kid, err)
		}
		if derived != alg {
			return nil, fmt.Errorf("%w: key %q is recorded as %s but is an %s key",
				ErrUnsupportedAlgorithm, kid, alg, derived)
		}

		key := &Key{
			KID:       kid,
			Algorithm: alg,
			Status:    Status(status),
			Public:    public,
		}

		if key.Status == StatusCurrent {
			material, err := s.resolver.Resolve(config.SecretRef(privateRef))
			if err != nil {
				// The reference is named, never the material it points at.
				return nil, fmt.Errorf("resolving private key for %q: %w", kid, err)
			}

			signer, err := ParsePrivateKey(material)
			if err != nil {
				return nil, fmt.Errorf("private key for %q: %w", kid, err)
			}
			key.private = signer
		}

		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading signing keys: %w", err)
	}

	return NewKeySet(keys)
}

// Insert records a newly generated key in the `next` state.
//
// The private key reference is stored, never the material — the schema has a
// CHECK constraint refusing anything containing a PEM private key header,
// which is the "simplification" that would otherwise silently violate
// docs/PLAN/02's constraint while every test still passed (P0-07).
func (s *Store) Insert(ctx context.Context, pair *KeyPair, privateRef string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO signing_keys (kid, purpose, algorithm, public_key, private_key_ref, status)
		VALUES ($1, $2, $3, $4, $5, 'next')`,
		pair.KID, string(s.purpose), string(pair.Algorithm), pair.PublicPEM, privateRef)
	if err != nil {
		return fmt.Errorf("inserting signing key: %w", err)
	}
	return nil
}

// Rotation describes what a rotation did, for the audit event and the operator.
type Rotation struct {
	Promoted   string // was next, now current
	Demoted    string // was current, now previous
	Retired    []string
	AlreadyRan bool
}

// ErrNoNextKey means there is nothing to promote.
var ErrNoNextKey = errors.New("no key in the next state to promote")

// Rotate promotes `next` to `current` and demotes the old `current`.
//
// One transaction, and the ORDER inside it matters. The old current is demoted
// BEFORE the new one is promoted, because the database has a partial unique
// index allowing only one `current` per purpose — promoting first would
// violate it. That constraint is doing real work here: it makes "two keys
// signing at once" unrepresentable rather than merely unlikely.
//
// Retirement is deliberately NOT part of this. A key moves to `previous` and
// stays there until an operator retires it separately, after a full token
// lifetime plus margin has passed. Retiring in the same step would invalidate
// every token issued in the seconds before the rotation — which is precisely
// the outage the overlap window exists to prevent.
func (s *Store) Rotate(ctx context.Context) (*Rotation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var next string
	err = tx.QueryRowContext(ctx, `
		SELECT kid FROM signing_keys
		WHERE purpose = $1 AND status = 'next'
		ORDER BY created_at
		LIMIT 1
		FOR UPDATE`, string(s.purpose)).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoNextKey
	}
	if err != nil {
		return nil, fmt.Errorf("selecting next key: %w", err)
	}

	result := &Rotation{Promoted: next}

	// Demote first. See the ordering note above.
	var demoted string
	err = tx.QueryRowContext(ctx, `
		UPDATE signing_keys SET status = 'previous'
		WHERE purpose = $1 AND status = 'current'
		RETURNING kid`, string(s.purpose)).Scan(&demoted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// First rotation on a fresh deployment: there is nothing to demote.
	case err != nil:
		return nil, fmt.Errorf("demoting current key: %w", err)
	default:
		result.Demoted = demoted
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE signing_keys SET status = 'current', activated_at = now()
		WHERE purpose = $1 AND kid = $2`, string(s.purpose), next); err != nil {
		return nil, fmt.Errorf("promoting next key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing rotation: %w", err)
	}

	return result, nil
}

// Retire moves a `previous` key out of the published key set.
//
// Separate from Rotate, and only ever applied to a `previous` key. Retiring a
// `current` key would leave nothing signing; retiring a `next` key would
// discard a key consumers may already have cached.
//
// After this, tokens signed by that key stop verifying. That is the intended
// effect and the reason it is a distinct, deliberate command.
func (s *Store) Retire(ctx context.Context, kid string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE signing_keys SET status = 'retired', retired_at = now()
		WHERE purpose = $1 AND kid = $2 AND status = 'previous'`,
		string(s.purpose), kid)
	if err != nil {
		return fmt.Errorf("retiring key: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("retiring key: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("key %q is not in the previous state; only a previous key can be retired", kid)
	}

	return nil
}

// List returns every key including retired ones, for the operator command.
func (s *Store) List(ctx context.Context) ([]KeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kid, algorithm, status,
		       created_at, activated_at, retired_at
		FROM signing_keys
		WHERE purpose = $1
		ORDER BY created_at`, string(s.purpose))
	if err != nil {
		return nil, fmt.Errorf("listing signing keys: %w", err)
	}
	defer rows.Close()

	var out []KeyInfo
	for rows.Next() {
		var info KeyInfo
		if err := rows.Scan(&info.KID, &info.Algorithm, &info.Status,
			&info.CreatedAt, &info.ActivatedAt, &info.RetiredAt); err != nil {
			return nil, fmt.Errorf("scanning key info: %w", err)
		}
		out = append(out, info)
	}

	return out, rows.Err()
}

// KeyInfo is the operator's view of a key. It deliberately carries no key
// material of any kind, not even the public half — the operator command prints
// this, and a public key in terminal output is noise that trains people to
// skim past key-shaped text.
type KeyInfo struct {
	KID         string
	Algorithm   string
	Status      string
	CreatedAt   sql.NullTime
	ActivatedAt sql.NullTime
	RetiredAt   sql.NullTime
}

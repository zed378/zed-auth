package management

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// `Idempotency-Key` on POST, per docs/PLAN/05 Part B.
//
// The header exists so that an automated provisioning retry is safe. Getting
// it right is mostly about being strict in the two places where being lenient
// looks helpful:
//
//   - A key reused with a DIFFERENT body is a conflict, not a replay. Silently
//     returning the first result would make a client's second, different
//     intention disappear — and the client would have no way to notice.
//   - A key whose first request is still IN FLIGHT is a conflict, not a queue.
//     Waiting would hold a connection for as long as the first request takes;
//     running would defeat the header entirely.

// IdempotencyTTL is how long a record is honoured.
//
// Twenty-four hours: long enough for any retry that is not a bug, short enough
// that the table is not a permanent log of everything anybody ever posted.
const IdempotencyTTL = 24 * time.Hour

// maxKeyLength bounds the header.
//
// A key is caller-chosen and becomes part of a primary key. 255 is generous
// against every convention anybody uses (a UUID is 36) and stops the header
// being a way to write large values into an indexed column.
const maxKeyLength = 255

// Replay is a stored response.
type Replay struct {
	Status   int
	Response json.RawMessage
}

// IdempotencyStore remembers what a keyed request answered.
type IdempotencyStore struct{}

func NewIdempotencyStore() *IdempotencyStore { return &IdempotencyStore{} }

// HashRequest fingerprints a request for comparison.
//
// The METHOD and PATH are inside the hash as well as being stored, because a
// key reused across two endpoints is a different request even when the body
// happens to match — and a client that reuses a key by accident is far more
// likely to do it across endpoints than within one.
//
// The body is hashed and never stored: storing it would put whatever a caller
// sent, including a password on a user-creation call, into a durable table.
func HashRequest(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// ErrInFlight means the first request with this key has not answered yet.
var ErrInFlight = errors.New("management: a request with this key is still in flight")

// Begin claims a key for a request, or reports what to do instead.
//
// Three outcomes, and they are the whole design:
//
//	(nil, nil)          the key is ours; run the handler and call Complete
//	(*Replay, nil)      the same request already answered; return that
//	(nil, err)          a Fault the caller should write
//
// The claim is an INSERT rather than a check-then-insert. Two concurrent
// requests with the same key race on the primary key, and exactly one wins —
// which is the same reasoning P1-06 gives for redeeming an authorization code
// with GETDEL rather than GET-then-DEL. A check-then-insert passes every
// sequential test and runs the handler twice under concurrency, which is the
// precise failure the header exists to prevent.
func (s *IdempotencyStore) Begin(
	ctx context.Context, tx *postgres.Tx, orgID, clientID, key, method, path string, body []byte, now time.Time,
) (*Replay, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	hash := HashRequest(method, path, body)

	// ON CONFLICT DO NOTHING, then read. The insert is the lock.
	result, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (org_id, client_id, key, request_hash, method, path, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (org_id, client_id, key) DO NOTHING`,
		orgID, clientID, key, hash, method, path, now.Add(IdempotencyTTL))
	if err != nil {
		return nil, fmt.Errorf("management: claiming an idempotency key: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 1 {
		return nil, nil
	}

	// Somebody else holds the key. Which somebody, and for what?
	var (
		storedHash string
		status     sql.NullInt64
		response   []byte
		expiresAt  time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT request_hash, status, response, expires_at
		  FROM idempotency_records
		 WHERE org_id = $1 AND client_id = $2 AND key = $3`,
		orgID, clientID, key,
	).Scan(&storedHash, &status, &response, &expiresAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Expired and swept between the insert and this read. Vanishingly
		// rare, and the honest answer is to let the caller retry rather than
		// to invent a result.
		return nil, Fault{
			Class: Conflict, Message: "This request could not be completed. Please retry.",
			Reason: "the idempotency record disappeared between claim and read",
		}
	case err != nil:
		return nil, fmt.Errorf("management: reading an idempotency record: %w", err)
	}

	if !expiresAt.After(now) {
		// The row is past its life but not yet swept. Treated as absent, and
		// re-claimed, so an expired key does not block a caller forever.
		if _, err := tx.Exec(ctx, `
			UPDATE idempotency_records
			   SET request_hash = $4, method = $5, path = $6,
			       status = NULL, response = NULL,
			       created_at = $7, expires_at = $8
			 WHERE org_id = $1 AND client_id = $2 AND key = $3`,
			orgID, clientID, key, hash, method, path, now, now.Add(IdempotencyTTL)); err != nil {
			return nil, fmt.Errorf("management: reclaiming an expired idempotency key: %w", err)
		}
		return nil, nil
	}

	if storedHash != hash {
		// The same key, a different request. Refused rather than replayed:
		// returning the first result would make the caller's second,
		// different intention disappear with no way to notice.
		return nil, Fault{
			Class:   Conflict,
			Message: "This Idempotency-Key was already used for a different request.",
			Reason:  "request hash mismatch",
		}
	}

	if !status.Valid {
		// The first request is still running. Refused rather than queued:
		// waiting holds a connection for as long as the first takes, and
		// running defeats the header.
		return nil, Fault{
			Class:   Conflict,
			Message: "A request with this Idempotency-Key is still in progress.",
			Reason:  ErrInFlight.Error(),
		}
	}

	return &Replay{Status: int(status.Int64), Response: response}, nil
}

// Complete stores what the handler answered, so a replay can return it.
//
// The bytes are stored verbatim, as a string rather than as a []byte — pgx maps
// []byte to bytea, and the column is text. An empty body is stored as an empty
// string, which is NOT NULL and so still satisfies the schema's both-or-neither
// constraint; a 204 is a real answer and replaying it as `null` would invent
// content the first request never sent.
func (s *IdempotencyStore) Complete(
	ctx context.Context, tx *postgres.Tx, orgID, clientID, key string, status int, response []byte,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		   SET status = $4, response = $5
		 WHERE org_id = $1 AND client_id = $2 AND key = $3`,
		orgID, clientID, key, status, string(response))
	if err != nil {
		return fmt.Errorf("management: storing an idempotent response: %w", err)
	}
	return nil
}

// --- the middleware's view of the store ---------------------------------------------

// DBClaims is the IdempotencyStore with transactions attached.
//
// Each of the three operations runs in its own committed transaction, and the
// separation is the point rather than an accident of structure. In particular
// the claim COMMITS before the handler runs: a claim held open inside the
// handler's transaction would make a concurrent duplicate block on the primary
// key until the first request finished, rather than be refused — the request
// would be slow instead of rejected, which is the failure the whole header
// exists to avoid.
type DBClaims struct {
	Store *IdempotencyStore
	DB    *postgres.DB
}

func NewDBClaims(db *postgres.DB) *DBClaims {
	return &DBClaims{Store: NewIdempotencyStore(), DB: db}
}

func (c *DBClaims) Begin(
	ctx context.Context, orgID, clientID, key, method, path string, body []byte, now time.Time,
) (*Replay, error) {
	var replay *Replay
	err := c.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		replay, err = c.Store.Begin(ctx, tx, orgID, clientID, key, method, path, body, now)
		return err
	})
	return replay, err
}

func (c *DBClaims) Complete(ctx context.Context, orgID, clientID, key string, status int, body []byte) error {
	return c.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return c.Store.Complete(ctx, tx, orgID, clientID, key, status, body)
	})
}

// Release drops an unfinished claim.
//
// Deliberately on a background context rather than the request's: the request
// context is very often already cancelled by the time this runs — a client that
// disconnected is one of the reasons a handler did not finish — and using it
// would leave the claim in place for exactly the callers most likely to retry.
//
// `status IS NULL` in the predicate is what keeps this from being a way to
// delete a COMPLETED record. Without it, a caller who could get a handler to
// fail after a successful sibling stored its answer could erase that answer.
func (c *DBClaims) Release(orgID, clientID, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return c.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM idempotency_records
			 WHERE org_id = $1 AND client_id = $2 AND key = $3 AND status IS NULL`,
			orgID, clientID, key)
		if err != nil {
			return fmt.Errorf("management: releasing an idempotency claim: %w", err)
		}
		return nil
	})
}

// Sweep deletes expired records.
//
// PostgreSQL has no TTL, so this is the cleanup job Redis would not need — and
// it is exactly the kind of job P0-20 found can fail silently for days. It
// returns a count so a caller can emit a metric rather than trusting it ran.
//
// Through a SECURITY DEFINER function rather than a DELETE, and not by choice:
// instance scope sets current_org_id() to NULL, under which
// `idempotency_tenant_isolation` matches no row at all. A plain DELETE here
// reports success and removes nothing, which the integration test caught only
// because it asserts the COUNT rather than the absence of an error.
func (s *IdempotencyStore) Sweep(ctx context.Context, db *postgres.DB, now time.Time, limit int) (int64, error) {
	var deleted int64

	err := db.WithInstanceScope(ctx, "sweeping expired idempotency records", func(tx *postgres.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT sweep_idempotency_records($1, $2)`, now, limit,
		).Scan(&deleted); err != nil {
			return fmt.Errorf("management: sweeping idempotency records: %w", err)
		}
		return nil
	})

	return deleted, err
}

// ValidateKey checks the header before it becomes part of a primary key.
func ValidateKey(key string) error {
	switch {
	case key == "":
		return Fault{
			Class: Invalid, Message: "The Idempotency-Key header must not be empty.",
			Reason: "empty idempotency key",
		}
	case len(key) > maxKeyLength:
		return Fault{
			Class: Invalid, Message: "The Idempotency-Key header is too long.",
			Reason: "idempotency key over the length bound",
		}
	}

	// Printable ASCII only. A key travels into a primary key, a log line and
	// an error message; a control character or a newline in any of those is a
	// log-injection primitive rather than a client's choice of identifier.
	for _, r := range key {
		if r < 0x21 || r > 0x7E {
			return Fault{
				Class: Invalid, Message: "The Idempotency-Key header contains characters that are not allowed.",
				Reason: "idempotency key is not printable ASCII",
			}
		}
	}
	return nil
}

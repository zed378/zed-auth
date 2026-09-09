package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Persistence for registered clients.
//
// Every method takes a *postgres.Tx rather than opening its own transaction,
// for the reason ADR-012 gives: the audit event commits with the action that
// caused it. An application created without its audit record, or a record for
// a creation that rolled back, both produce a log that cannot be trusted — and
// an untrusted log is worse than none, because decisions get made from it.
//
// The transaction is tenant-scoped, so RLS (P0-08) confines every query here
// to one organization without this file containing an org_id predicate that
// somebody could forget.

// ErrNotFound means no such application is visible in the current scope.
//
// "Visible" is load-bearing: another tenant's application is not-found rather
// than forbidden, because a "forbidden" would confirm the row exists.
var ErrNotFound = errors.New("client: application not found")

// ErrPublicClientSecret means a secret was requested for a client that cannot
// keep one.
var ErrPublicClientSecret = errors.New("client: public clients have no secret")

// Store reads and writes applications.
type Store struct {
	audit *audit.Writer
}

func NewStore(auditor *audit.Writer) *Store { return &Store{audit: auditor} }

// Record is an application as stored, with its credential state.
//
// Credentials holds only hashes, so this type cannot carry a plaintext secret
// no matter how it is serialised.
type Record struct {
	Application
	Credentials Credentials
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// columns selects the array columns as JSON.
//
// database/sql hands a text[] back as the raw Postgres array literal —
// {"https://a/cb","https://b/cb"} — and there is no standard scanner for it.
// The alternatives were parsing that literal by hand, which means getting
// quoting and backslash escapes right for values that come from user input, or
// taking a second SQL driver as a dependency purely for its array codec.
//
// Letting Postgres serialise to JSON avoids both: it already knows how to
// escape its own values, and encoding/json already knows how to read them.
const columns = `
	id, org_id, project_id, name, type,
	client_secret_hash, previous_client_secret_hash, previous_client_secret_expires_at,
	to_jsonb(redirect_uris), to_jsonb(post_logout_redirect_uris), to_jsonb(grant_types),
	created_at, updated_at`

// jsonStrings scans a JSON array of strings.
//
// Always yields a non-nil slice, so a client with no post-logout URIs reads
// back as an empty list rather than as nil — which is what stops a round trip
// through the store turning "[]" into a NULL on the next write.
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
		return fmt.Errorf("client: cannot scan %T into a string array", src)
	}

	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("client: decoding array column: %w", err)
	}
	if out != nil {
		*j = out
	}
	return nil
}

// scan reads one row. The array columns come back as []string from the pgx
// driver directly — no array wrapper type, which is why lib/pq is not a
// dependency here even though it is in the module graph.
func scan(row interface{ Scan(...any) error }) (Record, error) {
	var (
		rec           Record
		secretHash    sql.NullString
		previousHash  sql.NullString
		previousUntil sql.NullTime
		redirects     jsonStrings
		postLogout    jsonStrings
		grants        jsonStrings
	)

	err := row.Scan(
		&rec.ID, &rec.OrgID, &rec.ProjectID, &rec.Name, &rec.Type,
		&secretHash, &previousHash, &previousUntil,
		&redirects, &postLogout, &grants,
		&rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return Record{}, err
	}

	rec.RedirectURIs = redirects
	rec.PostLogoutRedirectURIs = postLogout
	rec.GrantTypes = grants

	rec.Credentials = Credentials{
		Hash:              secretHash.String,
		PreviousHash:      previousHash.String,
		PreviousExpiresAt: previousUntil.Time,
	}
	return rec, nil
}

// Create registers an application and, for a confidential client, issues its
// secret.
//
// The Secret is returned here and nowhere else in this package. There is no
// method that reads one back, because there is nothing to read it from: only
// the hash was stored.
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, app Application, actorUserID string,
) (Record, Secret, error) {
	if err := app.Validate(); err != nil {
		return Record{}, Secret{}, err
	}

	// Canonicalise before storing, so the exact comparison at authorization
	// time has a predictable target. Validate already accepted these; this
	// takes the canonical form it returned.
	var err error
	if app.RedirectURIs, err = canonicalise(app.RedirectURIs, app.Type); err != nil {
		return Record{}, Secret{}, err
	}
	if app.PostLogoutRedirectURIs, err = canonicalise(app.PostLogoutRedirectURIs, app.Type); err != nil {
		return Record{}, Secret{}, err
	}

	var (
		secret Secret
		hash   sql.NullString
	)
	if app.Type.IsConfidential() {
		s, h, err := Generate()
		if err != nil {
			return Record{}, Secret{}, err
		}
		secret, hash = s, sql.NullString{String: h, Valid: true}
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO applications (
			org_id, project_id, name, type, client_secret_hash,
			redirect_uris, post_logout_redirect_uris, grant_types)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+columns,
		app.OrgID, app.ProjectID, app.Name, string(app.Type), hash,
		app.RedirectURIs, app.PostLogoutRedirectURIs, app.GrantTypes,
	)

	rec, err := scan(row)
	if err != nil {
		return Record{}, Secret{}, fmt.Errorf("client: creating application: %w", err)
	}

	// Whether a secret was issued, never the secret or its hash.
	if err := s.audit.Write(ctx, tx, audit.Event{
		OrgID:       rec.OrgID,
		ActorUserID: actorUserID,
		Type:        audit.EventApplicationCreated,
		Payload: map[string]any{
			"application_id": rec.ID,
			"project_id":     rec.ProjectID,
			"type":           string(rec.Type),
			"secret_issued":  rec.Credentials.HasSecret(),
			"redirect_uris":  rec.RedirectURIs,
			"grant_types":    rec.GrantTypes,
		},
	}); err != nil {
		return Record{}, Secret{}, fmt.Errorf("client: auditing creation: %w", err)
	}

	return rec, secret, nil
}

// Get reads one application by its id, which is also its client_id.
func (s *Store) Get(ctx context.Context, tx *postgres.Tx, id string) (Record, error) {
	row := tx.QueryRow(ctx, `SELECT `+columns+` FROM applications WHERE id = $1`, id)

	rec, err := scan(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	case err != nil:
		return Record{}, fmt.Errorf("client: reading application: %w", err)
	}
	return rec, nil
}

// Update changes the mutable parts of a registration.
//
// The id, org, project and type are not among them: a client's type
// determines whether it can hold a secret, so changing it would either strand
// a secret on a now-public client or leave a confidential one without any.
// Delete and re-register instead, which is visible in the audit log as two
// events rather than one silent reclassification.
func (s *Store) Update(
	ctx context.Context, tx *postgres.Tx, id string, app Application, actorUserID string,
) (Record, error) {
	before, err := s.Get(ctx, tx, id)
	if err != nil {
		return Record{}, err
	}

	// Validate against the STORED type, not one the caller supplied. Otherwise
	// an update claiming to be a `web` client would be validated under rules
	// the row does not actually live by.
	app.Type = before.Type
	app.Name = firstNonEmpty(app.Name, before.Name)
	if err := app.Validate(); err != nil {
		return Record{}, err
	}

	if app.RedirectURIs, err = canonicalise(app.RedirectURIs, before.Type); err != nil {
		return Record{}, err
	}
	if app.PostLogoutRedirectURIs, err = canonicalise(app.PostLogoutRedirectURIs, before.Type); err != nil {
		return Record{}, err
	}

	row := tx.QueryRow(ctx, `
		UPDATE applications
		   SET name = $2, redirect_uris = $3, post_logout_redirect_uris = $4, grant_types = $5
		 WHERE id = $1
		RETURNING `+columns,
		id, app.Name,
		app.RedirectURIs, app.PostLogoutRedirectURIs, app.GrantTypes,
	)

	after, err := scan(row)
	if err != nil {
		return Record{}, fmt.Errorf("client: updating application: %w", err)
	}

	// Redirect URIs are recorded with their values, before and after. They are
	// not secret, and this is the only place a widened redirect URI shows up.
	if err := s.audit.Write(ctx, tx, audit.Event{
		OrgID:       after.OrgID,
		ActorUserID: actorUserID,
		Type:        audit.EventApplicationUpdated,
		Payload: map[string]any{
			"application_id":          after.ID,
			"redirect_uris_before":    before.RedirectURIs,
			"redirect_uris_after":     after.RedirectURIs,
			"post_logout_uris_before": before.PostLogoutRedirectURIs,
			"post_logout_uris_after":  after.PostLogoutRedirectURIs,
			"grant_types_before":      before.GrantTypes,
			"grant_types_after":       after.GrantTypes,
		},
	}); err != nil {
		return Record{}, fmt.Errorf("client: auditing update: %w", err)
	}

	return after, nil
}

// RotateSecret issues a new secret, keeping the previous one valid for the
// overlap.
//
// A zero overlap retires the old secret immediately, for a secret believed
// compromised.
func (s *Store) RotateSecret(
	ctx context.Context, tx *postgres.Tx, id string, overlap time.Duration, now time.Time, actorUserID string,
) (Secret, time.Time, error) {
	rec, err := s.Get(ctx, tx, id)
	if err != nil {
		return Secret{}, time.Time{}, err
	}

	if rec.Type.IsPublic() {
		return Secret{}, time.Time{}, fmt.Errorf("%w: %s is a %s client",
			ErrPublicClientSecret, id, rec.Type)
	}

	var (
		secret  Secret
		rotated Credentials
	)
	if overlap <= 0 {
		secret, rotated, err = rec.Credentials.RotateNow()
	} else {
		secret, rotated, err = rec.Credentials.Rotate(overlap, now)
	}
	if err != nil {
		return Secret{}, time.Time{}, err
	}

	previousHash := sql.NullString{String: rotated.PreviousHash, Valid: rotated.PreviousHash != ""}
	previousUntil := sql.NullTime{Time: rotated.PreviousExpiresAt, Valid: rotated.PreviousHash != ""}

	if _, err := tx.Exec(ctx, `
		UPDATE applications
		   SET client_secret_hash = $2,
		       previous_client_secret_hash = $3,
		       previous_client_secret_expires_at = $4
		 WHERE id = $1`,
		id, rotated.Hash, previousHash, previousUntil,
	); err != nil {
		return Secret{}, time.Time{}, fmt.Errorf("client: rotating secret: %w", err)
	}

	if err := s.audit.Write(ctx, tx, audit.Event{
		OrgID:       rec.OrgID,
		ActorUserID: actorUserID,
		Type:        audit.EventApplicationSecretRotated,
		Payload: map[string]any{
			"application_id":             id,
			"previous_secret_expires_at": previousUntil.Time,
			"immediate":                  overlap <= 0,
		},
	}); err != nil {
		return Secret{}, time.Time{}, fmt.Errorf("client: auditing rotation: %w", err)
	}

	return secret, rotated.PreviousExpiresAt, nil
}

// Delete removes a registration.
func (s *Store) Delete(ctx context.Context, tx *postgres.Tx, id string, actorUserID string) error {
	rec, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM applications WHERE id = $1`, id); err != nil {
		return fmt.Errorf("client: deleting application: %w", err)
	}

	return s.audit.Write(ctx, tx, audit.Event{
		OrgID:       rec.OrgID,
		ActorUserID: actorUserID,
		Type:        audit.EventApplicationDeleted,
		Payload: map[string]any{
			"application_id": id,
			"name":           rec.Name,
			"type":           string(rec.Type),
		},
	})
}

// Authenticate verifies a presented client secret.
//
// The path P1-07's token endpoint takes. Returns the record on success so the
// caller does not read it twice.
//
// A public client always fails here, and deliberately so rather than being
// treated as "no secret required": a caller presenting a secret for a public
// client is confused about which client it holds, and silently accepting it
// would let a misconfigured deployment believe it was authenticating.
func (s *Store) Authenticate(
	ctx context.Context, tx *postgres.Tx, clientID, presented string, now time.Time,
) (Record, error) {
	rec, err := s.Get(ctx, tx, clientID)
	if err != nil {
		return Record{}, err
	}

	if !rec.Type.IsConfidential() {
		return Record{}, fmt.Errorf("%w: %s is a %s client", ErrPublicClientSecret, clientID, rec.Type)
	}
	if !rec.Credentials.Verify(presented, now) {
		return Record{}, ErrNotFound
	}

	return rec, nil
}

// canonicalise validates each URI and returns the form to store.
//
// Always returns a non-nil slice. A nil []string binds as SQL NULL, and both
// array columns are NOT NULL DEFAULT '{}' — so returning the caller's nil
// through unchanged turned "this client has no post-logout URIs", which is the
// common case, into a constraint violation.
func canonicalise(uris []string, t Type) ([]string, error) {
	out := make([]string, 0, len(uris))
	for _, uri := range uris {
		canonical, err := ValidateRedirectURI(uri, t)
		if err != nil {
			return nil, err
		}
		out = append(out, canonical)
	}
	return out, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ByClientID resolves a public client_id, before a tenant is known.
//
// /oauth/authorize receives a client_id in a URL and must resolve it to decide
// which organization the request belongs to, so this read cannot be
// tenant-scoped — the same bootstrap problem sessions have, reached from the
// other direction, and given the same narrow SECURITY DEFINER door.
//
// The function it calls returns no secret columns at all, so the Record it
// produces has empty Credentials by construction. That is not an omission to
// remember: there is nothing for the query to return.
func (s *Store) ByClientID(ctx context.Context, db *postgres.DB, clientID string) (Application, error) {
	var (
		app        Application
		redirects  jsonStrings
		postLogout jsonStrings
		grants     jsonStrings
	)

	err := db.SQL().QueryRowContext(ctx, `
		SELECT id, org_id, project_id, name, type,
		       redirect_uris, post_logout_redirect_uris, grant_types
		  FROM application_by_client_id($1)`, clientID,
	).Scan(&app.ID, &app.OrgID, &app.ProjectID, &app.Name, &app.Type,
		&redirects, &postLogout, &grants)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Application{}, fmt.Errorf("%w: %s", ErrNotFound, clientID)
	case err != nil:
		return Application{}, fmt.Errorf("client: resolving client_id: %w", err)
	}

	app.RedirectURIs = redirects
	app.PostLogoutRedirectURIs = postLogout
	app.GrantTypes = grants
	return app, nil
}

// Package postgres owns database access, and owns the tenant context that
// row-level security depends on.
//
// The central idea: there is no exported way to run a query without declaring
// a scope. Every path goes through WithTenant or WithInstanceScope, both of
// which run inside a transaction that sets the tenant before any statement
// executes. A repository method cannot forget, because it never holds a
// queryable handle that has not already been scoped.
//
// That is a deliberate constraint rather than a convenience. PLAN/08-AUTHORIZATION.md
// Part B wants "a misscoped query can't leak data across organizations" to be
// a property of the system, and a policy in the database only delivers that if
// the application reliably sets the context the policy reads.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/stdlib" //nolint:revive // registers the pgx driver

	"github.com/zed378/zed-auth/backend/internal/config"
)

var _ = stdlib.GetDefaultDriver

// Common failures callers distinguish.
var (
	// ErrNoTenantContext means a query was attempted without a scope. It should
	// be impossible through this package's API; if it surfaces, something has
	// obtained a raw handle.
	ErrNoTenantContext = errors.New("no tenant context set")

	// ErrEmptyOrgID guards the specific mistake of passing the zero value,
	// which would set an empty tenant and quietly match nothing.
	ErrEmptyOrgID = errors.New("organization id is empty")
)

// DB is the database handle. It intentionally does not embed *sql.DB, so
// callers cannot reach Query or Exec directly and bypass tenant scoping.
type DB struct {
	db  *sql.DB
	log *slog.Logger
}

// Open connects and verifies the connection, returning a handle whose only
// query paths are scoped.
func Open(ctx context.Context, cfg config.PostgresConfig, log *slog.Logger) (*DB, error) {
	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &DB{db: db, log: log}, nil
}

// Close releases the pool.
func (d *DB) Close() error { return d.db.Close() }

// Name identifies this dependency to the readiness endpoint.
func (d *DB) Name() string { return "postgres" }

// Check reports whether the database is usable, for /readyz.
//
// It runs outside any tenant context and reads nothing from a policy-protected
// table, so it neither needs a scope nor discloses one.
func (d *DB) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.db.PingContext(ctx)
}

// Tx is a scoped transaction. Every query it exposes runs with a tenant
// context already established.
type Tx struct {
	tx    *sql.Tx
	orgID string // empty for an instance-scoped transaction
}

// OrgID returns the tenant this transaction is scoped to, or "" for
// instance scope.
func (t *Tx) OrgID() string { return t.orgID }

// IsInstanceScoped reports whether this transaction bypasses tenant filtering.
func (t *Tx) IsInstanceScoped() bool { return t.orgID == "" }

// Query, QueryRow and Exec forward to the underlying transaction. They are
// methods on Tx rather than on DB so that reaching them requires having gone
// through a scoping function first.
func (t *Tx) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, q, args...)
}

func (t *Tx) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, q, args...)
}

func (t *Tx) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, q, args...)
}

// WithTenant runs fn inside a transaction scoped to orgID.
//
// The scoping uses SET LOCAL, which is transaction-scoped. A plain SET would
// persist on the pooled connection and leak this request's tenant into
// whichever request reused it next — the precise cross-tenant leak this whole
// mechanism exists to prevent, and one that would appear only under
// concurrency, intermittently, in production.
//
// fn returning an error rolls back. A panic rolls back and re-panics rather
// than leaving a transaction open holding locks.
func (d *DB) WithTenant(ctx context.Context, orgID string, fn func(*Tx) error) error {
	if orgID == "" {
		return ErrEmptyOrgID
	}
	return d.withScope(ctx, orgID, fn)
}

// WithInstanceScope runs fn without tenant filtering, for the operations that
// genuinely span organizations.
//
// PLAN/08 Part B requires this path to be "explicit, documented, and auditable
// — not the normal path with the filter omitted". Three things follow:
//
//   - It is a differently-named function, so using it is a visible choice in a
//     diff rather than an omission.
//   - It requires a reason string, which is logged. An unexplained
//     cross-tenant query should be uncomfortable to write.
//   - It writes an audit event once the audit writer exists (P0-12).
//
// Legitimate uses are narrow: an INSTANCE_OWNER listing organizations, the
// instance audit log, signing key rotation, and background jobs such as
// partition maintenance. Anything reachable by an organization admin is not
// one of them.
func (d *DB) WithInstanceScope(ctx context.Context, reason string, fn func(*Tx) error) error {
	if reason == "" {
		return errors.New("instance-scoped access requires a reason")
	}

	d.log.LogAttrs(ctx, slog.LevelInfo, "instance-scoped database access",
		slog.String("reason", reason))

	return d.withScope(ctx, "", fn)
}

func (d *DB) withScope(ctx context.Context, orgID string, fn func(*Tx) error) (err error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				d.log.LogAttrs(ctx, slog.LevelError, "rollback failed",
					slog.String("error", rbErr.Error()))
			}
		}
	}()

	// Set the tenant before anything else runs. Passing orgID as a parameter
	// rather than interpolating it into the statement matters even though it
	// comes from trusted code: set_config is the parameterizable form, and
	// SET LOCAL is not, so building the SQL by hand would be the one place in
	// this package where a value reaches a statement as text
	// (SECURITY/02 §8, and AGENTS.md's parameterized-queries-only rule).
	//
	// The empty string for instance scope makes current_org_id() return NULL,
	// which every policy evaluates as false. Instance-scoped work therefore
	// still cannot read a policy-protected table by accident — it has to query
	// something not under RLS, or the caller has to be the owner. That is a
	// deliberate second line of defence behind the naming.
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	if err := fn(&Tx{tx: tx, orgID: orgID}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// AssertRoleIsNotPrivileged verifies at startup that the connected role cannot
// bypass row-level security.
//
// This exists because of how the failure looks without it. Point
// AUTH_POSTGRES_DSN at auth_owner — entirely plausible while debugging a
// permissions error — and every RLS policy silently stops applying. Nothing
// errors, no test fails, and cross-tenant isolation is gone. The integration
// tests cannot catch it either, since they would then be inspecting the owner.
//
// It is cheap and runs once, so it runs on every boot.
func (d *DB) AssertRoleIsNotPrivileged(ctx context.Context) error {
	var role string
	var isSuper, bypassRLS bool

	err := d.db.QueryRowContext(ctx, `
		SELECT current_user, rolsuper, rolbypassrls
		FROM pg_roles
		WHERE rolname = current_user`).Scan(&role, &isSuper, &bypassRLS)
	if err != nil {
		return fmt.Errorf("inspect database role: %w", err)
	}

	if isSuper {
		return fmt.Errorf("database role %q is a superuser: row-level security would be bypassed entirely, "+
			"so every organization's data would be visible to every request. Connect as the application role "+
			"(auth_app), not the schema owner", role)
	}
	if bypassRLS {
		return fmt.Errorf("database role %q has BYPASSRLS: row-level security would be silently disabled. "+
			"Connect as the application role (auth_app)", role)
	}

	var ownedTables int
	err = d.db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_tables
		WHERE schemaname = 'public' AND tableowner = current_user`).Scan(&ownedTables)
	if err != nil {
		return fmt.Errorf("inspect table ownership: %w", err)
	}
	if ownedTables > 0 {
		return fmt.Errorf("database role %q owns %d table(s): a table owner bypasses row-level security "+
			"on tables it owns. Migrations run as the owner; the service must not", role, ownedTables)
	}

	d.log.LogAttrs(ctx, slog.LevelInfo, "database role verified",
		slog.String("role", role),
		slog.Bool("superuser", false),
		slog.Bool("bypassrls", false),
		slog.Int("owned_tables", 0),
	)
	return nil
}

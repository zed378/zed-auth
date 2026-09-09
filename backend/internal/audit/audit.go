// Package audit writes the append-only event log.
//
// PLAN/09-SECURITY.md § Audit requires every identity- or permission-changing
// event to be recorded, and PLAN/04 makes the table append-only at the
// database level — the application role has no UPDATE or DELETE privilege on
// it (SECURITY/02 §19).
//
// One entry point exists so that no feature invents its own audit format. The
// value of an audit log is that an investigator can reason about it uniformly;
// six subtly different shapes for "a role was assigned" destroys that long
// before any single one of them is wrong.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// EventType names what happened, as noun.verb.outcome.
//
// Constants rather than free strings: the console filters on these
// (UI-UX/08 Audit Log), alerting keys on them (PLAN/13 § Alerting), and a
// typo in a literal produces an event that is silently never matched by either.
type EventType string

// Authentication (P1-12, P1-14).
const (
	EventLoginSucceeded EventType = "user.login.success"
	EventLoginFailed    EventType = "user.login.failed"
	EventLogout         EventType = "user.logout"
	EventUserLockedOut  EventType = "user.lockout"

	EventSessionCreated EventType = "session.created"
	EventSessionRevoked EventType = "session.revoked"

	EventTokenRevoked EventType = "token.revoked"
	EventTokenReuse   EventType = "token.reuse_detected"
)

// User lifecycle (P1-19).
const (
	EventUserCreated       EventType = "user.created"
	EventUserUpdated       EventType = "user.updated"
	EventUserDeactivated   EventType = "user.deactivated"
	EventUserInvited       EventType = "user.invited"
	EventPasswordChanged   EventType = "user.password.changed"
	EventPasswordResetSent EventType = "user.password.reset_requested"

	// EventPasswordRejected records a password refused by policy or by the
	// breach corpus (P1-02). The payload names the rules, never the password.
	EventPasswordRejected EventType = "user.password.rejected"

	// EventPasswordBreachCheckSkipped records a password accepted without a
	// corpus check because the corpus could not be reached.
	//
	// This is the audit half of ADR-015. Failing open is defensible because
	// each occurrence leaves a record naming the user whose password was not
	// checked — which is what makes a later re-check possible, and what makes
	// "the breach check has been down for three weeks" a finding rather than
	// an archaeology project.
	EventPasswordBreachCheckSkipped EventType = "user.password.breach_check_skipped"
)

// Authorization (P2-01, P2-03, P4-01, P4-02).
const (
	EventRoleCreated  EventType = "role.created"
	EventRoleUpdated  EventType = "role.updated"
	EventRoleDeleted  EventType = "role.deleted"
	EventRoleAssigned EventType = "role.assigned"
	EventRoleRevoked  EventType = "role.revoked"

	EventProjectGrantCreated EventType = "project_grant.created"
	EventProjectGrantRevoked EventType = "project_grant.revoked"

	EventManagerRoleAssigned EventType = "manager_role.assigned"
	EventManagerRoleRevoked  EventType = "manager_role.revoked"
)

// Configuration and instance-level (P1-05, P1-16, P1-03, P0-08).
const (
	EventOrganizationCreated   EventType = "organization.created"
	EventOrganizationSuspended EventType = "organization.suspended"
	EventPolicyUpdated         EventType = "policy.updated"
	EventPolicyActivated       EventType = "policy.activated"

	EventApplicationCreated       EventType = "application.created"
	EventApplicationSecretRotated EventType = "application.secret_rotated"
	EventApplicationDeleted       EventType = "application.deleted"

	EventSigningKeyRotated EventType = "signing_key.rotated"

	// EventInstanceScopedAccess records a use of the cross-tenant database
	// path. PLAN/08 Part B requires that path to be auditable, not merely
	// possible.
	EventInstanceScopedAccess EventType = "instance.scoped_access"
)

// Event is one audit record.
type Event struct {
	// OrgID is the owning tenant. Empty means instance-level: an action that
	// belongs to no single organization, such as signing key rotation.
	OrgID string

	// ActorUserID is who did it. Empty is legitimate and common — a failed
	// login against an address that does not exist has no authenticated actor,
	// and neither does a scheduled job.
	ActorUserID string

	Type EventType

	// Payload carries the detail. It is redacted before storage using the same
	// rules as the logger (P0-09): an audit log that records a password
	// attempt is a credential store, and it is one with a longer retention
	// than the logs.
	Payload map[string]any

	// IP and RequestID tie the event to the request that caused it, which is
	// what makes an incident timeline reconstructable.
	IP        string
	RequestID string
}

// Observer receives maintenance outcomes, so the metrics package does not have
// to be imported here and this package stays usable without it.
type Observer interface {
	// PartitionRunway reports how many months of runway remain.
	PartitionRunway(months int)
	// PartitionMaintenanceFailed reports a failed run.
	PartitionMaintenanceFailed()
}

// Writer records events.
type Writer struct {
	db  *postgres.DB
	log *slog.Logger

	// observer is optional; nil means metrics are not wired.
	observer Observer

	// forwarder ships events to an external SIEM. PLAN/09 § Audit says the log
	// should "ideally" be forwarded; this is the seam, a no-op until P5-08
	// wires a real one.
	forwarder Forwarder
}

// Forwarder ships an event beyond this database.
//
// An audit log that lives only in the database it audits is one an attacker
// with database access can reason about. Forwarding puts a copy somewhere they
// would have to compromise separately (SECURITY/02 §19).
type Forwarder interface {
	Forward(ctx context.Context, e Event) error
}

// NewWriter builds a Writer. Pass nil for forwarder until one exists.
func NewWriter(db *postgres.DB, log *slog.Logger, forwarder Forwarder) *Writer {
	return &Writer{db: db, log: log, forwarder: forwarder}
}

// SetObserver wires maintenance reporting. Set once at startup.
func (w *Writer) SetObserver(o Observer) { w.observer = o }

// Write records an event inside an existing transaction.
//
// This is the primary entry point, and taking a *postgres.Tx rather than
// opening its own transaction is the whole design decision (ADR-012):
//
// The event commits with the action that caused it. A role assignment that
// succeeds without its audit record, or an audit record for an assignment that
// rolled back, both produce a log that cannot be trusted — and a log that
// cannot be trusted is worse than no log, because decisions get made from it.
//
// The cost is real and accepted: if the audit write fails, the action fails.
// For an identity provider that is the right trade. PLAN/09 requires every
// permission-changing event to be captured, and "captured unless the insert
// happened to fail" is not that.
func (w *Writer) Write(ctx context.Context, tx *postgres.Tx, e Event) error {
	if e.Type == "" {
		return errors.New("audit: event type is required")
	}

	// The tenant of the transaction wins over whatever the caller passed. A
	// mismatch would be refused by row-level security anyway, but failing here
	// gives an error that names the problem instead of a policy violation that
	// does not.
	if !tx.IsInstanceScoped() {
		if e.OrgID != "" && e.OrgID != tx.OrgID() {
			return fmt.Errorf("audit: event org %q does not match the transaction's tenant %q",
				e.OrgID, tx.OrgID())
		}
		e.OrgID = tx.OrgID()
	} else if e.OrgID != "" {
		return fmt.Errorf("audit: event carries org %q but the transaction is instance-scoped", e.OrgID)
	}

	payload, err := redactPayload(e.Payload)
	if err != nil {
		return fmt.Errorf("audit: encode payload: %w", err)
	}

	const q = `
		INSERT INTO events (org_id, actor_user_id, event_type, payload, ip, request_id)
		VALUES (NULLIF($1,'')::uuid, NULLIF($2,'')::uuid, $3, $4::jsonb, NULLIF($5,'')::inet, NULLIF($6,''))`

	requestID := e.RequestID
	if requestID == "" {
		requestID = observability.RequestIDFromContext(ctx)
	}

	if _, err := tx.Exec(ctx, q, e.OrgID, e.ActorUserID, string(e.Type), payload, e.IP, requestID); err != nil {
		return fmt.Errorf("audit: write %s: %w", e.Type, err)
	}

	w.forward(ctx, e)
	return nil
}

// WriteStandalone records an event that has no surrounding transaction.
//
// A failed login is the motivating case: nothing else is being written, so
// there is no business transaction for the event to join. It still needs a
// transaction of its own, because the tenant context RLS reads is set with
// SET LOCAL (P0-08).
func (w *Writer) WriteStandalone(ctx context.Context, e Event) error {
	if e.OrgID == "" {
		return errors.New("audit: WriteStandalone requires an org; use WriteInstanceLevel for instance events")
	}
	return w.db.WithTenant(ctx, e.OrgID, func(tx *postgres.Tx) error {
		return w.Write(ctx, tx, e)
	})
}

// WriteInstanceLevel records an event belonging to no single organization.
//
// Signing key rotation and cross-tenant database access are the cases. These
// rows have a NULL org_id and are visible only from the instance-scoped path
// (migration 000008).
func (w *Writer) WriteInstanceLevel(ctx context.Context, reason string, e Event) error {
	e.OrgID = ""
	return w.db.WithInstanceScope(ctx, reason, func(tx *postgres.Tx) error {
		return w.Write(ctx, tx, e)
	})
}

// forward ships the event onward, without letting a forwarding failure fail
// the action.
//
// This is the opposite trade from the database write, deliberately. The
// database copy is the record of truth and must commit with the action; the
// forwarded copy is a convenience for an external system, and an unreachable
// SIEM must not be able to stop logins from working. The failure is logged so
// it is visible rather than silent.
func (w *Writer) forward(ctx context.Context, e Event) {
	if w.forwarder == nil {
		return
	}
	if err := w.forwarder.Forward(ctx, e); err != nil {
		w.log.LogAttrs(ctx, slog.LevelWarn, "audit forwarding failed",
			slog.String("event_type", string(e.Type)),
			slog.String("error", err.Error()),
		)
	}
}

// redactPayload applies the logger's rules to the payload before storage.
//
// Redaction happens here rather than at the call sites because a call site
// that forgets produces a credential sitting in an append-only table that
// nothing can delete — the append-only guarantee makes a mistake permanent.
func redactPayload(p map[string]any) ([]byte, error) {
	if p == nil {
		return []byte(`{}`), nil
	}

	clean := make(map[string]any, len(p))
	for k, v := range p {
		if observability.IsSensitiveKey(k) {
			clean[k] = observability.Redacted
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			b, err := redactPayload(nested)
			if err != nil {
				return nil, err
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			clean[k] = m
			continue
		}
		clean[k] = v
	}
	return json.Marshal(clean)
}

// --- Reading ----------------------------------------------------------------

// Record is a stored event, as read back.
type Record struct {
	ID          int64
	OrgID       string
	ActorUserID string
	Type        EventType
	Payload     map[string]any
	IP          string
	RequestID   string
	CreatedAt   time.Time
}

// Filter narrows a query. The zero value matches everything in the tenant.
type Filter struct {
	Types  []EventType
	Actor  string
	After  time.Time
	Before time.Time

	// Limit caps the page. Zero means DefaultLimit; anything above MaxLimit is
	// clamped, because an unbounded query against a table designed to grow
	// without bound is a denial of service against the database.
	Limit int

	// Cursor is the ID to read before, for descending pagination. Keyset
	// rather than OFFSET: OFFSET on a large table scans and discards, and it
	// skips or repeats rows when new events arrive mid-pagination — which they
	// constantly do here.
	Cursor int64
}

// Pagination bounds.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// Query reads events for the transaction's tenant, newest first.
//
// Row-level security does the tenant filtering (P0-08), so this deliberately
// adds no org_id predicate: doing so would imply the filtering lives here,
// and someone would eventually remove it as redundant.
func (w *Writer) Query(ctx context.Context, tx *postgres.Tx, f Filter) ([]Record, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	q := `
		SELECT id, COALESCE(org_id::text,''), COALESCE(actor_user_id::text,''),
		       event_type, payload, COALESCE(host(ip),''), COALESCE(request_id,''), created_at
		FROM events
		WHERE ($1::bigint IS NULL OR id < $1)
		  AND ($2::text[] IS NULL OR event_type = ANY($2))
		  AND ($3::uuid IS NULL OR actor_user_id = $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at < $5)
		ORDER BY created_at DESC, id DESC
		LIMIT $6`

	var cursor any
	if f.Cursor > 0 {
		cursor = f.Cursor
	}
	var types any
	if len(f.Types) > 0 {
		ts := make([]string, len(f.Types))
		for i, t := range f.Types {
			ts[i] = string(t)
		}
		types = ts
	}
	var actor, after, before any
	if f.Actor != "" {
		actor = f.Actor
	}
	if !f.After.IsZero() {
		after = f.After
	}
	if !f.Before.IsZero() {
		before = f.Before
	}

	rows, err := tx.Query(ctx, q, cursor, types, actor, after, before, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	out := make([]Record, 0, limit)
	for rows.Next() {
		var r Record
		var payload []byte
		if err := rows.Scan(&r.ID, &r.OrgID, &r.ActorUserID, &r.Type, &payload,
			&r.IP, &r.RequestID, &r.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("audit: scan: %w", err)
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &r.Payload)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("audit: iterate: %w", err)
	}

	// One row beyond the limit was requested purely to detect a next page
	// without a second COUNT query.
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}

	return out, next, nil
}

// --- Partition maintenance --------------------------------------------------

// EnsurePartitions creates the events partitions for the coming months.
//
// This exists because its absence is a scheduled outage. When the last
// partition's range ends, every INSERT into events fails — and since every
// security-sensitive action writes an audit event (PLAN/09 § Audit), every
// such action fails with it. At midnight on the first of a month, with no
// deploy and no code change to point at.
//
// Called at startup and daily by Run.
func (w *Writer) EnsurePartitions(ctx context.Context, monthsAhead int) error {
	return w.db.WithInstanceScope(ctx, "audit: ensure events partitions exist", func(tx *postgres.Tx) error {
		rows, err := tx.Query(ctx, `SELECT * FROM ensure_events_partitions_ahead($1)`, monthsAhead)
		if err != nil {
			return fmt.Errorf("ensure partitions: %w", err)
		}
		defer rows.Close()

		var created []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			created = append(created, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		w.log.LogAttrs(ctx, slog.LevelInfo, "events partitions ensured",
			slog.Int("months_ahead", monthsAhead),
			slog.Any("partitions", created),
		)

		// Report the runway actually present rather than the runway requested.
		// If partition creation silently stopped working, the gauge drifts
		// toward zero weeks before the month boundary where it becomes an
		// outage — which is the whole reason this metric exists (P0-12).
		if w.observer != nil {
			w.observer.PartitionRunway(len(created) - 1)
		}
		return nil
	})
}

// Run maintains partitions until ctx is cancelled.
//
// Daily rather than monthly: a monthly tick that fires while the service
// happens to be down has to wait a month for its next chance, and monthsAhead
// runway is what covers the gap. Daily makes a missed tick unremarkable.
func (w *Writer) Run(ctx context.Context, monthsAhead int) {
	if err := w.EnsurePartitions(ctx, monthsAhead); err != nil {
		// Not fatal at startup: the current month's partition already exists,
		// so the service works today. Failing to boot over a maintenance task
		// would turn a future problem into an immediate outage.
		w.log.LogAttrs(ctx, slog.LevelError, "initial partition maintenance failed",
			slog.String("error", err.Error()))
		if w.observer != nil {
			w.observer.PartitionMaintenanceFailed()
		}
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.EnsurePartitions(ctx, monthsAhead); err != nil {
				w.log.LogAttrs(ctx, slog.LevelError, "partition maintenance failed",
					slog.String("error", err.Error()))
				if w.observer != nil {
					w.observer.PartitionMaintenanceFailed()
				}
			}
		}
	}
}

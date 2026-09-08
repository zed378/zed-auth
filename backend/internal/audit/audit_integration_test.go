//go:build integration

package audit

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The stack is started by the tests themselves — no database prepared in
// advance, no environment variables (P0-15).
func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func newWriter(t *testing.T, fwd Forwarder) (*Writer, *postgres.DB) {
	t.Helper()

	log := observability.NewLogger(io.Discard, observability.Options{Level: "error", Format: "json"})
	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: envOr("AUTH_TEST_APP_DSN",
			"postgres://auth_app:local_dev_only@localhost:5432/auth?sslmode=disable"),
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	}, log)
	if err != nil {
		t.Fatalf("PostgreSQL unreachable despite the test stack being up: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return NewWriter(db, log, fwd), db
}

// seedOrg creates a tenant using the owner connection, which is not subject to
// row-level security.
func seedOrg(t *testing.T, name string) string {
	t.Helper()

	ownerDSN := envOr("AUTH_TEST_OWNER_DSN",
		"postgres://auth_owner:local_dev_only@localhost:5432/auth?sslmode=disable")
	log := observability.NewLogger(io.Discard, observability.Options{Level: "error", Format: "json"})

	owner, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: ownerDSN, MaxOpenConns: 2, MaxIdleConns: 1,
	}, log)
	if err != nil {
		t.Fatalf("owner connection unavailable despite the test stack being up: %v", err)
	}

	var orgID, instanceID string
	err = owner.WithInstanceScope(context.Background(), "test: seed tenant", func(tx *postgres.Tx) error {
		if err := tx.QueryRow(context.Background(),
			`INSERT INTO instances (name) VALUES ($1) RETURNING id`, "audit-"+name).Scan(&instanceID); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(),
			`INSERT INTO organizations (instance_id, name) VALUES ($1, $2) RETURNING id`,
			instanceID, "audit-"+name).Scan(&orgID)
	})
	if err != nil {
		owner.Close()
		t.Fatalf("seed tenant: %v", err)
	}

	t.Cleanup(func() {
		owner.WithInstanceScope(context.Background(), "test: cleanup", func(tx *postgres.Tx) error {
			tx.Exec(context.Background(), `DELETE FROM events WHERE org_id = $1`, orgID)
			tx.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
			tx.Exec(context.Background(), `DELETE FROM instances WHERE id = $1`, instanceID)
			return nil
		})
		owner.Close()
	})

	return orgID
}

func TestWriteAndQuery(t *testing.T) {
	w, _ := newWriter(t, nil)
	orgID := seedOrg(t, "write-query")
	ctx := context.Background()

	if err := w.WriteStandalone(ctx, Event{
		OrgID: orgID,
		Type:  EventLoginSucceeded,
		Payload: map[string]any{
			"method": "password",
		},
		IP:        "203.0.113.4",
		RequestID: "req-abc123",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got []Record
	if err := w.db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		got, _, err = w.Query(ctx, tx, Filter{})
		return err
	}); err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].Type != EventLoginSucceeded {
		t.Errorf("type = %q, want %q", got[0].Type, EventLoginSucceeded)
	}
	if got[0].IP != "203.0.113.4" {
		t.Errorf("ip = %q, want the recorded address", got[0].IP)
	}
	if got[0].RequestID != "req-abc123" {
		t.Errorf("request_id = %q — without it an event cannot be tied to its request", got[0].RequestID)
	}
	if got[0].Payload["method"] != "password" {
		t.Errorf("payload lost: %v", got[0].Payload)
	}
}

// The append-only guarantee makes a mistake permanent: a credential written
// here cannot be deleted, by anyone, ever. Redaction happens in the writer for
// that reason rather than at call sites that might forget.
func TestPayloadIsRedactedBeforeStorage(t *testing.T) {
	w, _ := newWriter(t, nil)
	orgID := seedOrg(t, "redaction")
	ctx := context.Background()

	if err := w.WriteStandalone(ctx, Event{
		OrgID: orgID,
		Type:  EventLoginFailed,
		Payload: map[string]any{
			"email":         "victim@example.com",
			"password":      "hunter2",
			"access_token":  "eyJhbGciOi.super.secret",
			"client_secret": "cs_live_abcdef",
			"attributes":    map[string]any{"salary": 100000},
			"nested": map[string]any{
				"refresh_token": "rt_should_not_survive",
				"safe":          "kept",
			},
		},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var rec Record
	if err := w.db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		out, _, err := w.Query(ctx, tx, Filter{})
		if err != nil {
			return err
		}
		if len(out) != 1 {
			t.Fatalf("want 1 event, got %d", len(out))
		}
		rec = out[0]
		return nil
	}); err != nil {
		t.Fatalf("query: %v", err)
	}

	for _, secret := range []string{"hunter2", "eyJhbGciOi.super.secret", "cs_live_abcdef", "rt_should_not_survive"} {
		for k, v := range rec.Payload {
			if s, ok := v.(string); ok && strings.Contains(s, secret) {
				t.Errorf("secret %q survived redaction in key %q", secret, k)
			}
		}
		if nested, ok := rec.Payload["nested"].(map[string]any); ok {
			for k, v := range nested {
				if s, ok := v.(string); ok && strings.Contains(s, secret) {
					t.Errorf("secret %q survived redaction in nested key %q", secret, k)
				}
			}
		}
	}

	// Redaction must not be so broad it destroys the record's usefulness.
	if rec.Payload["email"] != "victim@example.com" {
		t.Errorf("a non-sensitive field was redacted: %v", rec.Payload["email"])
	}
	if nested, ok := rec.Payload["nested"].(map[string]any); ok {
		if nested["safe"] != "kept" {
			t.Errorf("a safe nested field was lost: %v", nested)
		}
	} else {
		t.Error("nested object was not preserved as an object")
	}
}

// An event and the action that caused it commit together (ADR-012). An audit
// record for an action that rolled back is a log that cannot be trusted, and a
// log that cannot be trusted is worse than none because decisions come from it.
func TestEventRollsBackWithItsTransaction(t *testing.T) {
	w, db := newWriter(t, nil)
	orgID := seedOrg(t, "rollback")
	ctx := context.Background()

	sentinel := errors.New("business logic failed")

	err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		if err := w.Write(ctx, tx, Event{Type: EventRoleAssigned}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the caller's error, got %v", err)
	}

	var n int
	if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&n)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d event(s) survived a rolled-back transaction", n)
	}
}

// The transaction's tenant is authoritative. A mismatch would be refused by
// RLS anyway, but failing here names the problem instead of surfacing a policy
// violation that does not.
func TestEventCannotClaimAnotherTenant(t *testing.T) {
	w, db := newWriter(t, nil)
	orgA := seedOrg(t, "claim-a")
	orgB := seedOrg(t, "claim-b")
	ctx := context.Background()

	err := db.WithTenant(ctx, orgA, func(tx *postgres.Tx) error {
		return w.Write(ctx, tx, Event{OrgID: orgB, Type: EventRoleAssigned})
	})
	if err == nil {
		t.Fatal("an event claimed a tenant other than its transaction's")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("the error should name the mismatch, got: %v", err)
	}
}

// PLAN/08 Part B requires the cross-tenant path to be auditable. These events
// have no org and are visible only from the instance-scoped path.
func TestInstanceLevelEvent(t *testing.T) {
	w, db := newWriter(t, nil)
	ctx := context.Background()

	if err := w.WriteInstanceLevel(ctx, "test: instance-level audit", Event{
		Type:    EventSigningKeyRotated,
		Payload: map[string]any{"kid": "key-2026-09"},
	}); err != nil {
		t.Fatalf("write instance-level: %v", err)
	}

	var found bool
	if err := db.WithInstanceScope(ctx, "test: read instance-level events", func(tx *postgres.Tx) error {
		recs, _, err := w.Query(ctx, tx, Filter{Types: []EventType{EventSigningKeyRotated}})
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.Payload["kid"] == "key-2026-09" {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !found {
		t.Error("the instance-level event was not readable from the instance-scoped path")
	}

	t.Cleanup(func() {
		db.WithInstanceScope(ctx, "test: cleanup", func(tx *postgres.Tx) error {
			tx.Exec(ctx, `DELETE FROM events WHERE org_id IS NULL AND payload->>'kid' = 'key-2026-09'`)
			return nil
		})
	})

	// A tenant must not see instance-level events.
	orgID := seedOrg(t, "instance-isolation")
	var n int
	if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM events WHERE org_id IS NULL`).Scan(&n)
	}); err != nil {
		t.Fatalf("tenant read: %v", err)
	}
	if n != 0 {
		t.Errorf("a tenant can see %d instance-level event(s)", n)
	}
}

func TestQueryFiltersAndPagination(t *testing.T) {
	w, db := newWriter(t, nil)
	orgID := seedOrg(t, "filters")
	ctx := context.Background()

	types := []EventType{EventLoginSucceeded, EventLoginFailed, EventRoleAssigned}
	for i := 0; i < 12; i++ {
		if err := w.WriteStandalone(ctx, Event{
			OrgID:   orgID,
			Type:    types[i%len(types)],
			Payload: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}

	t.Run("filter by type", func(t *testing.T) {
		var recs []Record
		if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
			var err error
			recs, _, err = w.Query(ctx, tx, Filter{Types: []EventType{EventLoginFailed}})
			return err
		}); err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(recs) != 4 {
			t.Errorf("want 4 login failures, got %d", len(recs))
		}
		for _, r := range recs {
			if r.Type != EventLoginFailed {
				t.Errorf("filter leaked a %q event", r.Type)
			}
		}
	})

	// Keyset pagination rather than OFFSET: OFFSET skips or repeats rows when
	// new events arrive mid-pagination, which on an audit log they constantly do.
	t.Run("keyset pagination is stable", func(t *testing.T) {
		var page1, page2 []Record
		var next int64

		if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
			var err error
			page1, next, err = w.Query(ctx, tx, Filter{Limit: 5})
			return err
		}); err != nil {
			t.Fatalf("page 1: %v", err)
		}
		if len(page1) != 5 || next == 0 {
			t.Fatalf("page 1: %d records, next=%d", len(page1), next)
		}

		// Write more events between pages — the case OFFSET gets wrong.
		if err := w.WriteStandalone(ctx, Event{OrgID: orgID, Type: EventUserCreated}); err != nil {
			t.Fatalf("interleaved write: %v", err)
		}

		if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
			var err error
			page2, _, err = w.Query(ctx, tx, Filter{Limit: 5, Cursor: next})
			return err
		}); err != nil {
			t.Fatalf("page 2: %v", err)
		}

		seen := map[int64]bool{}
		for _, r := range page1 {
			seen[r.ID] = true
		}
		for _, r := range page2 {
			if seen[r.ID] {
				t.Errorf("record %d appeared on both pages despite an interleaved write", r.ID)
			}
		}
	})

	t.Run("limit is clamped", func(t *testing.T) {
		var recs []Record
		if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
			var err error
			recs, _, err = w.Query(ctx, tx, Filter{Limit: 100000})
			return err
		}); err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(recs) > MaxLimit {
			t.Errorf("returned %d records, above the %d cap — an unbounded query against an "+
				"unbounded table is a denial of service", len(recs), MaxLimit)
		}
	})
}

// The absence of this is a scheduled outage: when the last partition's range
// ends, every INSERT into events fails, and with it every action that writes
// an audit event.
func TestEnsurePartitionsCreatesRunway(t *testing.T) {
	w, db := newWriter(t, nil)
	ctx := context.Background()

	if err := w.EnsurePartitions(ctx, 6); err != nil {
		t.Fatalf("ensure partitions: %v", err)
	}

	var count int
	if err := db.WithInstanceScope(ctx, "test: count partitions", func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM pg_inherits i
			JOIN pg_class p ON p.oid = i.inhparent
			WHERE p.relname = 'events'`).Scan(&count)
	}); err != nil {
		t.Fatalf("count partitions: %v", err)
	}

	if count < 7 {
		t.Errorf("%d partitions exist, want at least 7 (current month plus 6 ahead)", count)
	}
}

// Several instances start at once and all call this. Without the advisory lock
// they race between the existence check and the CREATE.
func TestEnsurePartitionsIsConcurrencySafe(t *testing.T) {
	w, _ := newWriter(t, nil)
	ctx := context.Background()

	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { errs <- w.EnsurePartitions(ctx, 4) }()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent call %d failed: %v", i, err)
		}
	}
}

// A new partition must inherit the append-only rule. Without it, a partition
// created next month would accept UPDATE and DELETE — and the audit log would
// stop being append-only for exactly the recent events an attacker would want
// to alter.
func TestNewPartitionsAreAppendOnly(t *testing.T) {
	w, db := newWriter(t, nil)
	ctx := context.Background()

	if err := w.EnsurePartitions(ctx, 6); err != nil {
		t.Fatalf("ensure partitions: %v", err)
	}

	var offenders []string
	if err := db.WithInstanceScope(ctx, "test: inspect partition privileges", func(tx *postgres.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT table_name
			FROM information_schema.role_table_grants
			WHERE grantee = 'auth_app'
			  AND table_name LIKE 'events_%'
			  AND privilege_type IN ('UPDATE','DELETE')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			offenders = append(offenders, name)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf("partitions accept UPDATE or DELETE: %v", offenders)
	}
}

// A SIEM being unreachable must not stop logins from working. The database
// copy is the record of truth; the forwarded copy is a convenience.
func TestForwardingFailureDoesNotFailTheAction(t *testing.T) {
	failing := forwarderFunc(func(context.Context, Event) error {
		return errors.New("SIEM unreachable")
	})

	w, _ := newWriter(t, failing)
	orgID := seedOrg(t, "forwarder")

	if err := w.WriteStandalone(context.Background(), Event{
		OrgID: orgID, Type: EventLoginSucceeded,
	}); err != nil {
		t.Errorf("a forwarding failure failed the action: %v", err)
	}
}

func TestForwarderReceivesTheEvent(t *testing.T) {
	received := make(chan Event, 1)
	w, _ := newWriter(t, forwarderFunc(func(_ context.Context, e Event) error {
		received <- e
		return nil
	}))
	orgID := seedOrg(t, "forwarder-receives")

	if err := w.WriteStandalone(context.Background(), Event{
		OrgID: orgID, Type: EventRoleAssigned,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case e := <-received:
		if e.Type != EventRoleAssigned {
			t.Errorf("forwarded type = %q", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("the forwarder never received the event")
	}
}

func TestWriteRejectsAnEmptyType(t *testing.T) {
	w, db := newWriter(t, nil)
	orgID := seedOrg(t, "empty-type")

	err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		return w.Write(context.Background(), tx, Event{})
	})
	if err == nil {
		t.Error("an event with no type was accepted")
	}
}

type forwarderFunc func(context.Context, Event) error

func (f forwarderFunc) Forward(ctx context.Context, e Event) error { return f(ctx, e) }

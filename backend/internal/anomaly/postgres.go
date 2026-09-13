package anomaly

import (
	"context"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The history and the audit trail, in Postgres (P3-08).
//
// **The history is the sessions table**, which every login already writes with
// its IP and user agent. There is no second table of "known devices" or "known
// locations": that would be a movement history of every user kept for its own
// sake, and it would disagree with the sessions table the moment one was pruned
// and the other was not.

// Tenant runs work inside a tenant-scoped transaction.
type Tenant interface {
	WithTenant(ctx context.Context, orgID string, fn func(*postgres.Tx) error) error
}

// PostgresHistory reads recent logins from `sessions`.
type PostgresHistory struct {
	DB Tenant
}

func (p *PostgresHistory) Recent(
	ctx context.Context, orgID, userID, excludeSessionID string, limit int,
) ([]Past, error) {
	var out []Past

	err := p.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		// Revoked sessions count. A session the user signed out of was still a
		// login they made from that device and that place, and excluding it
		// would make a device new again every time somebody used "sign out
		// everywhere" — which is exactly the moment a user is paying attention
		// and exactly the wrong one to send them a spurious notice.
		rows, err := tx.Query(ctx, `
			SELECT created_at, COALESCE(host(ip), ''), COALESCE(user_agent, '')
			  FROM sessions
			 WHERE user_id = $1
			   AND id <> $2
			 ORDER BY created_at DESC
			 LIMIT $3`, userID, nullableID(excludeSessionID), limit)
		if err != nil {
			return fmt.Errorf("anomaly: reading login history: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var p Past
			if err := rows.Scan(&p.At, &p.IP, &p.UserAgent); err != nil {
				return fmt.Errorf("anomaly: reading a past login: %w", err)
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// nullableID keeps an empty exclusion from comparing against a malformed uuid.
func nullableID(id string) any {
	if id == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return id
}

// Auditor is what AuditRecorder writes through.
type Auditor interface {
	Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error
}

// AuditRecorder writes `user.login.anomaly`.
type AuditRecorder struct {
	DB    Tenant
	Audit Auditor
}

func (a *AuditRecorder) RecordAnomaly(ctx context.Context, f Finding) error {
	return a.DB.WithTenant(ctx, f.OrgID, func(tx *postgres.Tx) error {
		payload := map[string]any{
			"session_id": f.SessionID,
			"signals":    SignalNames(f.Signals),
		}
		// The coarse place only, and only when known. Never the IP: the
		// session row already has it, and repeating it beside a location in an
		// audit payload with 24-month retention is how a movement history gets
		// built by accident.
		if f.Where != "" {
			payload["location"] = f.Where
		}

		return a.Audit.Write(ctx, tx, audit.Event{
			OrgID:       f.OrgID,
			ActorUserID: f.UserID,
			Type:        audit.EventLoginAnomaly,
			Payload:     payload,
		})
	})
}

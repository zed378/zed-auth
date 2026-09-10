// Package auditlog implements the Management API's read view of the audit log
// (P1-20).
//
// A separate package from internal/audit, which writes it. Not tidiness: this
// one imports internal/management for the error envelope and pagination, and
// management imports internal/audit — putting the reader beside the writer
// would close that loop.
//
// **There is one operation here and there will never be more.** `events` is
// append-only at the database level, where the application role holds no
// UPDATE and no DELETE on it (docs/SECURITY/02 §19). An API that offered a way
// around that would turn a privilege guarantee into a matter of trust, and a
// test asserts the router registers nothing but the read.
package auditlog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// MaxEventTypes bounds how many types one query may name.
//
// A filter is a convenience; an unbounded IN list is a way to make the planner
// do arbitrary work on a table that grows without bound.
const MaxEventTypes = 20

// Handler serves the audit log read.
type Handler struct {
	DB  *postgres.DB
	Log *slog.Logger
}

// Entry is one event as the API represents it.
type Entry struct {
	ID          int64
	CreatedAt   time.Time
	EventType   string
	ActorUserID string
	IP          string
	RequestID   string
	Payload     map[string]any
}

func (h *Handler) ListEvents(
	ctx context.Context, request api.ListEventsRequestObject,
) (api.ListEventsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	types := values(request.Params.EventType)
	if len(types) > MaxEventTypes {
		return nil, management.Fault{
			Class: management.Invalid, Message: "Too many event types in one query.",
			Details: []api.ErrorDetail{{
				Field: "event_type",
				Issue: fmt.Sprintf("name at most %d", MaxEventTypes),
			}},
			Reason: "event_type list over the bound",
		}
	}

	var actor string
	if request.Params.ActorId != nil {
		actor = request.Params.ActorId.String()
	}

	from, to := request.Params.From, request.Params.To
	if from != nil && to != nil && !to.After(*from) {
		// An empty range is almost always a mistake in the caller's clock
		// arithmetic, and answering it with an empty page reads as "nothing
		// happened" — which is the one answer an audit log must not give by
		// accident.
		return nil, management.Fault{
			Class: management.Invalid, Message: "That time range is empty.",
			Details: []api.ErrorDetail{{Field: "to", Issue: "must be after `from`"}},
			Reason:  "to is not after from",
		}
	}

	var rows []Entry
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		rows, err = h.query(ctx, tx, types, actor, from, to, cursor, size)
		return err
	}); err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, position)
	if err != nil {
		return nil, err
	}

	out := api.EventList{Events: make([]api.Event, 0, len(page.Items))}
	for _, entry := range page.Items {
		out.Events = append(out.Events, render(entry))
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListEvents200JSONResponse(out), nil
}

// query reads a page, newest first.
//
// **Descending, unlike every other list in this API.** An audit log is read
// from the end: the question is almost always "what just happened", and
// `events_org_created_at_idx` is `(org_id, created_at DESC)` precisely because
// that is the access pattern P0-07 expected.
//
// No org_id predicate. The transaction is scoped to one organization and RLS
// supplies it — which is what makes DoD item 3 true for EVERY filter
// combination rather than for the combinations somebody thought to check.
func (h *Handler) query(
	ctx context.Context, tx *postgres.Tx,
	types []string, actor string, from, to *time.Time,
	after management.Cursor, size int,
) ([]Entry, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, created_at, event_type, coalesce(actor_user_id::text, ''),
		       coalesce(host(ip), ''), coalesce(request_id, ''), payload::text
		  FROM events
		 WHERE (cardinality($1::text[]) = 0 OR event_type = ANY($1))
		   AND ($2::uuid IS NULL OR actor_user_id = $2)
		   AND ($3::timestamptz IS NULL OR created_at >= $3)
		   AND ($4::timestamptz IS NULL OR created_at < $4)
		   AND ($5::timestamptz IS NULL OR (created_at, id) < ($5, $6::bigint))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $7`,
		types, nullString(actor), nullTime(from), nullTime(to),
		cursorTime(after), cursorID(after), size+1)
	if err != nil {
		return nil, fmt.Errorf("auditlog: reading: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Entry
	for rows.Next() {
		var (
			entry   Entry
			payload string
		)
		if err := rows.Scan(&entry.ID, &entry.CreatedAt, &entry.EventType,
			&entry.ActorUserID, &entry.IP, &entry.RequestID, &payload); err != nil {
			return nil, fmt.Errorf("auditlog: reading: %w", err)
		}
		entry.Payload = decodePayload(payload)
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "an audit log read ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

// position encodes where a page ended.
//
// The id goes into the cursor as a decimal string, and `events` has a
// composite key of `(id, created_at)` on a partitioned table — so the id alone
// is not unique. The cursor carries both, which is what the comparison in
// `query` uses.
func position(e Entry) management.Cursor {
	return management.Cursor{After: e.CreatedAt, ID: fmt.Sprintf("%d", e.ID)}
}

func cursorTime(c management.Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.After
}

func cursorID(c management.Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.ID
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// render turns an entry into the API resource.
//
// The id is `<id>:<timestamp>` rather than a bare number, because `events` is
// partitioned on `created_at` and its primary key is the pair. A consumer
// treating a bare id as unique would eventually deduplicate two real events
// into one, which for an audit log is the failure that matters.
func render(e Entry) api.Event {
	out := api.Event{
		Id:         fmt.Sprintf("%d:%s", e.ID, e.CreatedAt.UTC().Format(time.RFC3339Nano)),
		EventType:  e.EventType,
		OccurredAt: e.CreatedAt,
	}
	// Left unspecified rather than set to null when absent. An entry with no
	// actor is the normal case for a failed login against an address that does
	// not exist, and omitting the key says "not applicable" more honestly than
	// an explicit null does.
	if e.ActorUserID != "" {
		if parsed, err := uuid.Parse(e.ActorUserID); err == nil {
			out.ActorUserId = nullable.NewNullableWithValue(parsed)
		}
	}
	if e.IP != "" {
		out.Ip = nullable.NewNullableWithValue(e.IP)
	}
	if e.RequestID != "" {
		out.RequestId = nullable.NewNullableWithValue(e.RequestID)
	}
	if len(e.Payload) > 0 {
		payload := e.Payload
		out.Payload = &payload
	}
	return out
}

func values(p *[]string) []string {
	if p == nil {
		return []string{}
	}
	out := make([]string, 0, len(*p))
	for _, v := range *p {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func intParam(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

func stringParam(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

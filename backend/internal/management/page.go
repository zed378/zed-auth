package management

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Cursor pagination, per docs/PLAN/05's example.
//
// The bound that matters is not page_size — it is what the cursor carries.
//
// **The cursor carries only the sort position** and nothing else. Every filter
// comes from the request and is re-applied on each page. That is what makes
// forging one uninteresting: a caller who crafts a cursor can choose a
// starting position within data they can already see, because RLS applies to
// the page query regardless of what the cursor says.
//
// A cursor carrying its own filter would be a filter the CLIENT controls,
// which is the shape of the bug where a crafted token pages past a permission
// check. It is worth being explicit that this is why the type has two fields
// and not more.

const (
	// DefaultPageSize is what a caller gets for asking for nothing.
	DefaultPageSize = 20

	// MaxPageSize bounds a list response.
	//
	// A caller asking for more is CLAMPED rather than refused: failing a list
	// request over a number is unhelpful when clamping is unambiguous, and an
	// integrator who asks for 1000 wants as many as they can have.
	MaxPageSize = 100
)

// Cursor is a position in a sorted list.
//
// Sorted by (created_at, id) everywhere, because created_at alone is not
// unique — two rows written in the same microsecond would make a page boundary
// non-deterministic, which shows up as a row that appears twice or never under
// concurrent inserts. The id is the tiebreak, and P0-12's events table already
// carries the same pair in its primary key for the same reason.
type Cursor struct {
	After time.Time `json:"a"`
	ID    string    `json:"i"`
}

// Encode renders a cursor as an opaque page token.
//
// Opaque BY CONTRACT rather than by encryption: the OpenAPI description says
// not to build on its contents, and nothing about it is a secret. Encrypting
// it would suggest it protects something, and the thing that protects the page
// is RLS.
func (c Cursor) Encode() (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("management: encoding a page token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeCursor reads a page token.
//
// A token that cannot be read is a VALIDATION_ERROR rather than an empty first
// page. Silently starting over would show a caller the beginning of a list
// they were halfway through, which reads as data loss and is much harder to
// diagnose than an error naming the parameter.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, Fault{
			Class: Invalid, Message: "The page token is not valid.",
			Reason: "page_token is not base64url",
		}
	}

	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, Fault{
			Class: Invalid, Message: "The page token is not valid.",
			Reason: "page_token is not a cursor",
		}
	}
	if c.ID == "" || c.After.IsZero() {
		return Cursor{}, Fault{
			Class: Invalid, Message: "The page token is not valid.",
			Reason: "page_token is missing a position",
		}
	}
	return c, nil
}

// PageSize clamps a requested size into the allowed range.
//
// Both ends. Zero and negative mean "unspecified" and get the default; a
// negative that fell through would become a LIMIT the database refuses, which
// is a 500 for a caller who typed a minus sign.
func PageSize(requested string) int {
	if requested == "" {
		return DefaultPageSize
	}

	n, err := strconv.Atoi(requested)
	if err != nil || n <= 0 {
		return DefaultPageSize
	}
	if n > MaxPageSize {
		return MaxPageSize
	}
	return n
}

// Page is one page of results plus the token for the next.
//
// The token is empty on the last page, which is what tells a caller to stop.
// Returning a token that yields an empty page instead would make every
// consumer's loop run one request longer than it needs to, forever.
type Page[T any] struct {
	Items         []T
	NextPageToken string
}

// Paginate turns `size+1` rows into a page.
//
// The caller fetches ONE more row than the page size; the extra is not
// returned and its only job is to answer "is there a next page" without a
// second COUNT query — which would be a second query whose answer can already
// have changed by the time it runs.
func Paginate[T any](rows []T, size int, position func(T) Cursor) (Page[T], error) {
	if len(rows) <= size {
		// Always non-nil, so a list endpoint returns [] rather than null. A
		// null where a consumer expects an array is a crash in most client
		// languages and a puzzle in the rest.
		if rows == nil {
			rows = []T{}
		}
		return Page[T]{Items: rows}, nil
	}

	items := rows[:size]
	token, err := position(items[len(items)-1]).Encode()
	if err != nil {
		return Page[T]{}, err
	}
	return Page[T]{Items: items, NextPageToken: token}, nil
}

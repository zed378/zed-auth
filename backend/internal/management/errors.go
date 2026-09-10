package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
)

// docs/PLAN/05's error envelope, and the one place a class becomes a status.
//
// One mapping rather than a status literal at each call site, because there
// will be dozens of endpoints and the value of a consistent envelope is that a
// client writes one error path. A handler that picks its own status is a
// handler that eventually picks a different one for the same condition.

// Class is what went wrong, in the vocabulary the mapping understands.
type Class int

const (
	// Unauthenticated: no credential, or one that is not usable.
	Unauthenticated Class = iota

	// Forbidden: authenticated, and lacking the role.
	Forbidden

	// NotFound: not visible in the caller's scope.
	//
	// Used for another organization's resource as well as for one that does
	// not exist, and the two are deliberately the same answer. A 403 would
	// confirm the id names something real, which is abuse case A-3's
	// disclosure (docs/SECURITY/02 §2, §14).
	NotFound

	// Invalid: the request is malformed or a field is unacceptable.
	Invalid

	// Conflict: an Idempotency-Key reused with a different body, or a
	// uniqueness violation.
	Conflict

	// RateLimited: over the client's bound.
	RateLimited

	// Internal: anything else.
	Internal
)

// Fault is an error with a class, carried to the middleware that writes it.
type Fault struct {
	Class   Class
	Message string

	// Details are field-level problems, rendered into the envelope's
	// details[] so UI-UX/15 can map them back to a form field.
	Details []api.ErrorDetail

	// RetryAfter is set for RateLimited.
	RetryAfter time.Duration

	// Reason is for the log and never for the response. "you are an ORG_ADMIN
	// and this needs ORG_OWNER" is useful to an operator and, told to a caller
	// probing an organization they do not administer, confirms it exists.
	Reason string
}

func (f Fault) Error() string {
	if f.Reason != "" {
		return f.Message + " (" + f.Reason + ")"
	}
	return f.Message
}

// Errorf builds a Fault.
func Errorf(class Class, message string) Fault {
	return Fault{Class: class, Message: message}
}

// mapping is the whole class-to-wire translation.
//
// A table rather than a switch, so that adding a class without deciding its
// status is a compile-time gap rather than a silent fall through to 500.
var mapping = map[Class]struct {
	status int
	code   api.ErrorCode
}{
	Unauthenticated: {http.StatusUnauthorized, api.UNAUTHENTICATED},
	Forbidden:       {http.StatusForbidden, api.PERMISSIONDENIED},
	NotFound:        {http.StatusNotFound, api.NOTFOUND},
	Invalid:         {http.StatusBadRequest, api.VALIDATIONERROR},
	Conflict:        {http.StatusConflict, api.CONFLICT},
	RateLimited:     {http.StatusTooManyRequests, api.RATELIMITED},
	Internal:        {http.StatusInternalServerError, api.INTERNAL},
}

// Status and Code report how a class is answered.
func (c Class) Status() int { return mapping[c].status }

func (c Class) Code() api.ErrorCode { return mapping[c].code }

// WriteError renders a Fault in docs/PLAN/05's envelope.
//
// An error that is not a Fault becomes Internal with a fixed message: an
// unexpected error's text is written for a developer and routinely names a
// table, a column or a query, none of which belongs in a response.
func WriteError(w http.ResponseWriter, err error) {
	fault := Fault{Class: Internal, Message: "An unexpected error occurred."}
	if !errors.As(err, &fault) {
		fault = Fault{Class: Internal, Message: "An unexpected error occurred."}
	}

	m, known := mapping[fault.Class]
	if !known {
		// Unreachable while the table covers every class, and handled anyway:
		// a class with no mapping must not answer 200.
		m = mapping[Internal]
	}

	h := w.Header()
	h.Set("Content-Type", "application/json")
	// No management response is cacheable. They are per-caller and
	// permission-dependent, so an intermediary that cached one would serve it
	// to somebody whose permissions differ.
	h.Set("Cache-Control", "no-store")

	if fault.Class == RateLimited && fault.RetryAfter > 0 {
		// Seconds, rounded up: RFC 9110's delay-seconds is an integer, and
		// rounding down would tell a well-behaved client to retry fractionally
		// too early, which the limiter would then refuse again.
		seconds := int(fault.RetryAfter.Seconds())
		if fault.RetryAfter > time.Duration(seconds)*time.Second {
			seconds++
		}
		h.Set("Retry-After", strconv.Itoa(seconds))
	}

	w.WriteHeader(m.status)

	var envelope api.Error
	envelope.Error.Code = m.code
	envelope.Error.Message = fault.Message
	if len(fault.Details) > 0 {
		envelope.Error.Details = &fault.Details
	}
	_ = json.NewEncoder(w).Encode(envelope)
}

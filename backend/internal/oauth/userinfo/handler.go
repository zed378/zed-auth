package userinfo

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Verifier checks a compact JWS of an expected type.
//
// An interface rather than *signing.Verifier so the handler's ordering,
// headers and error shapes can be tested without a key set — and so this
// package states what it needs, which is one function.
type Verifier interface {
	Verify(compact, wantType string) ([]byte, error)
}

// Subjects reads the user behind a validated token.
type Subjects interface {
	Subject(ctx context.Context, db *postgres.DB, token AccessToken, now time.Time) (Subject, string, error)
}

// Observer counts outcomes.
//
// The label is the outcome class, never the reason a token was refused: a
// metric that separated "expired" from "session revoked" would put into
// /metrics the same oracle the response body refuses to be, and /metrics is
// scraped and retained.
type Observer interface {
	UserInfo(outcome string, d time.Duration)
}

const (
	OutcomeOK           = "ok"
	OutcomeInvalidToken = "invalid_token"
	OutcomeNoScope      = "insufficient_scope"
	OutcomeError        = "error"
)

// Handler serves /oauth/userinfo.
type Handler struct {
	Issuer   string
	Verifier Verifier
	Subjects Subjects
	DB       *postgres.DB
	Observer Observer
	Log      *slog.Logger

	// Now is overridable for tests.
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ServeHTTP implements the endpoint.
//
// OIDC Core 5.3.1 requires both GET and POST. They do the same thing here,
// because the token travels in the header either way — the POST form body that
// RFC 6750 permits is not read, for the reason BearerToken gives about
// credentials in places that get logged.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := h.now()

	// Personal data addressed to one caller. Set before any branch, so that no
	// error path can return a cacheable response.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		h.body(w, api.VALIDATIONERROR, "This endpoint accepts GET and POST.")
		return
	}

	presented, ok := BearerToken(r.Header.Get("Authorization"))
	if !ok {
		// No credential at all. RFC 6750 §3.1: a request with no
		// authentication gets the bare challenge and NO error code — an error
		// code describes a credential that was presented, and there was none.
		h.answer(w, http.StatusUnauthorized, challenge{},
			api.UNAUTHENTICATED, "Authentication is required.")
		h.count(OutcomeInvalidToken, start)
		return
	}

	// `at+jwt` (RFC 9068). This is where an ID token presented as a bearer
	// credential is refused — before the payload is parsed, and long before
	// anything is read from the database.
	payload, err := h.Verifier.Verify(presented, signing.TypeAccessToken)
	if err != nil {
		h.refuse(w, start, "signature or type: "+err.Error())
		return
	}

	token, reason, err := Validate(payload, h.Issuer, h.now())
	switch {
	case errors.Is(err, ErrInsufficientScope):
		// 403, not 401, and it names the scope. Not a disclosure: the caller
		// already knows what it asked for, and RFC 6750 defines this code so a
		// client can tell "your token is broken" from "your token is fine and
		// does not cover this" — conflating them sends a working client into a
		// re-authentication loop that cannot help it.
		h.answer(w, http.StatusForbidden, challenge{
			code:        "insufficient_scope",
			description: "The openid scope is required.",
			scope:       ScopeOpenID,
		}, api.PERMISSIONDENIED, "This token does not carry the openid scope.")
		h.count(OutcomeNoScope, start)
		return
	case err != nil:
		h.refuse(w, start, reason)
		return
	}

	subject, email, err := h.Subjects.Subject(r.Context(), h.DB, token, h.now())
	switch {
	case errors.Is(err, ErrNoSubject):
		// The session was revoked, the user was deactivated, or the row is
		// gone. All three answer exactly as a forged token does — a caller
		// that could tell them apart could ask this endpoint whether somebody
		// has logged out.
		h.refuse(w, start, "no live subject")
		return
	case err != nil:
		if h.Log != nil {
			h.Log.Error("reading the userinfo subject failed", "error", err.Error())
		}
		w.WriteHeader(http.StatusInternalServerError)
		h.body(w, api.INTERNAL, "An unexpected error occurred.")
		h.count(OutcomeError, start)
		return
	}

	claims := Build(subject, email, token.Scopes())

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(claims); err != nil && h.Log != nil {
		h.Log.Warn("writing the userinfo response failed", "error", err.Error())
	}

	h.count(OutcomeOK, start)
}

// refuse answers every unusable token identically.
//
// One method rather than a branch per reason, so that adding a new refusal
// cannot accidentally add a new observable answer. `reason` goes to the log
// and never to the caller — and it is built from claims rather than from the
// credential, so the token itself cannot end up in a log line.
func (h *Handler) refuse(w http.ResponseWriter, start time.Time, reason string) {
	if h.Log != nil {
		h.Log.Info("userinfo refused an access token", "reason", reason)
	}
	h.answer(w, http.StatusUnauthorized, challenge{
		code:        "invalid_token",
		description: "The access token is not valid.",
	}, api.UNAUTHENTICATED, "The access token is not valid.")
	h.count(OutcomeInvalidToken, start)
}

// challenge is the content of an RFC 6750 WWW-Authenticate header.
type challenge struct {
	code        string
	description string
	scope       string
}

// String renders the header value.
//
// Assembled in one place rather than appended to across branches, because
// http.Header values cannot be changed after WriteHeader — an earlier version
// of this file set the header, wrote the status, and then tried to append
// `scope=` to it, which silently did nothing.
func (c challenge) String(realm string) string {
	parts := []string{`realm="` + realm + `"`}
	if c.code != "" {
		parts = append(parts,
			`error="`+c.code+`"`,
			`error_description="`+c.description+`"`)
	}
	if c.scope != "" {
		parts = append(parts, `scope="`+c.scope+`"`)
	}
	return "Bearer " + strings.Join(parts, ", ")
}

// answer writes the challenge header, the status, and the error envelope.
func (h *Handler) answer(
	w http.ResponseWriter, status int, c challenge, code api.ErrorCode, message string,
) {
	w.Header().Set("WWW-Authenticate", c.String(h.Issuer))
	w.WriteHeader(status)
	h.body(w, code, message)
}

// body writes PLAN/05's error envelope.
//
// The WWW-Authenticate header is what an OAuth client library reads; the
// envelope is what every other endpoint in this service speaks, and a client
// that reads bodies should not have to special-case this one.
func (h *Handler) body(w http.ResponseWriter, code api.ErrorCode, message string) {
	var envelope api.Error
	envelope.Error.Code = code
	envelope.Error.Message = message
	_ = json.NewEncoder(w).Encode(envelope)
}

func (h *Handler) count(outcome string, start time.Time) {
	if h.Observer != nil {
		h.Observer.UserInfo(outcome, h.now().Sub(start))
	}
}

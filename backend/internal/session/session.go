// Package session holds the browser session that makes single sign-on work.
//
// docs/PLAN/03's data flow, step 6: a user who authenticated once at this service
// reaches the second application without seeing a login screen. That is the
// whole reason to run a central identity provider rather than a login form per
// application, and it comes down to one cookie and one lookup.
//
// Everything else here exists because that cookie is a bearer credential with
// the same power as the password that created it. It lives in a browser for
// hours, travels on every navigation to this origin, and cannot be un-issued
// once stolen — only revoked, and only if revocation is genuinely immediate.
//
// Specification: MEMORY/specs/P1-11-session-management.md.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CookieName is the session cookie.
//
// The `__Host-` prefix is not decoration. A browser refuses to store a cookie
// with this prefix unless it is Secure, has Path=/, and has no Domain — which
// are exactly the attributes docs/PLAN/05 § Session & logout asks for. Writing them
// correctly protects against our own mistakes; the prefix makes the browser
// reject the mistake instead, including one introduced by a future change to
// this file.
//
// It works in local development too: browsers treat http://localhost as a
// secure context, so a Secure cookie is accepted there.
const CookieName = "__Host-zedauth_session"

// tokenBytes is the entropy in a session token.
//
// 256 bits, the same as a client secret and for the same reason (ADR-016): a
// bearer credential that is guessable is worth nothing, and at this size it is
// not guessable behind any hash. The number is load-bearing — shrink it and
// storing only a fast hash stops being defensible.
const tokenBytes = 32

// --- the token ---------------------------------------------------------------

// Token is a session token on its way to a browser.
//
// The same shape as client.Secret, for the same reason. P1-05 found that
// fmt.Stringer alone leaks under %d, because fmt consults String only for a
// handful of verbs and prints struct fields for the rest. Formatter covers
// every verb, so this cannot be printed by accident, and Reveal() is a
// conspicuous, greppable word at the one place the value legitimately escapes.
type Token struct {
	plaintext string
}

func (t Token) Format(f fmt.State, verb rune) { _, _ = io.WriteString(f, "[REDACTED]") }
func (t Token) String() string                { return "[REDACTED]" }
func (t Token) GoString() string              { return "session.Token{[REDACTED]}" }

// MarshalJSON refuses rather than redacting: a JSON field containing
// "[REDACTED]" is a value a consumer would store and then fail to use, having
// been told nothing was wrong.
func (t Token) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("session: a Token must not be marshalled; it belongs in a Set-Cookie header")
}

// Reveal returns the plaintext, for the Set-Cookie header and nowhere else.
func (t Token) Reveal() string { return t.plaintext }

func (t Token) IsZero() bool { return t.plaintext == "" }

// NewToken returns a fresh token and the hash to store.
//
// The plaintext exists here and in the response header. Nothing persists it,
// so a database dump contains no session credential — which is what makes
// PG-14's separation of credential from identifier complete rather than
// partial.
func NewToken() (Token, string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, "", fmt.Errorf("session: generating token: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)
	return Token{plaintext: plaintext}, HashToken(plaintext), nil
}

// HashToken returns the stored form.
//
// Unsalted SHA-256, per ADR-016's reasoning: there is no precomputation to
// defend against over 2^256, and it is looked up on the silent-SSO path where
// docs/PLAN/12 allows 150ms for the entire endpoint.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// tokenLength is what a well-formed token looks like: 32 bytes, base64url, no
// padding.
var tokenLength = base64.RawURLEncoding.EncodedLen(tokenBytes)

// ValidToken reports whether a presented cookie value is even shaped like a
// token.
//
// Checked before any lookup. A malformed cookie should not become a database
// query: it is free for an attacker to send and costs us a round trip, and
// there is no value in Redis or Postgres that a 4KB cookie could match.
func ValidToken(presented string) bool {
	if len(presented) != tokenLength {
		return false
	}
	for _, r := range presented {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// --- the session --------------------------------------------------------------

// Session is a live browser session.
//
// There is no token or token hash on this struct, deliberately. The store maps
// the hash and drops it, so no read path has a credential to return even by
// accident — PG-14's separation held in the type system rather than by
// remembering.
type Session struct {
	// ID is the internal identifier. Safe to display, join on, and audit.
	ID     string
	UserID string
	OrgID  string

	// AuthMethods records the factors actually used. P1-07 builds the `amr`
	// claim from it and Phase 3's step-up reads it, so it is populated
	// accurately from the start: a value that was never trustworthy cannot be
	// made trustworthy later.
	AuthMethods []string

	IP        string
	UserAgent string

	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// Live reports whether the session may still be used.
//
// Both bounds apply and the shorter one wins. The absolute lifetime caps how
// long one authentication is worth; the idle timeout caps how long an
// abandoned browser stays useful to whoever finds it.
func (s Session) Live(idleTimeout time.Duration, now time.Time) bool {
	return !s.ExpiredAbsolute(now) && !s.ExpiredIdle(idleTimeout, now)
}

func (s Session) ExpiredAbsolute(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// ExpiredIdle reports whether the session has been unused for too long.
//
// A zero or negative timeout means no idle bound, which is a legitimate
// configuration for a short absolute lifetime — not "expires immediately". The
// same reasoning as P1-02's max_age_days: a zero that logs everyone out is the
// kind of off-by-one that reads as a security control.
func (s Session) ExpiredIdle(idleTimeout time.Duration, now time.Time) bool {
	if idleTimeout <= 0 {
		return false
	}
	// Clock skew must not produce a negative age that wraps into an expiry.
	if s.LastSeenAt.After(now) {
		return false
	}
	return now.Sub(s.LastSeenAt) > idleTimeout
}

// --- policy --------------------------------------------------------------------

// Policy is how long a session lives, per organization.
//
// Read from organizations.settings the way P1-02 reads the password policy, so
// P2-10 can make it editable without any of this changing.
type Policy struct {
	AbsoluteLifetime time.Duration
	IdleTimeout      time.Duration
}

// DefaultPolicy matches P0-07's column default of session_lifetime_hours: 12.
//
// The idle timeout has no plan value — docs/PLAN/08 Part B specifies only the
// lifetime — so 2 hours is chosen here and stated rather than hidden: long
// enough that a user reading a long document is not logged out, short enough
// that an unattended machine stops being useful before the end of a working
// day.
var DefaultPolicy = Policy{
	AbsoluteLifetime: 12 * time.Hour,
	IdleTimeout:      2 * time.Hour,
}

// The bounds a configured lifetime is clamped into.
//
// A floor and a ceiling for the same reason P1-02 clamps min_length: without
// them, the mechanism built to give administrators control is the mechanism by
// which one of them removes it. A one-minute session is a denial of service on
// your own users; a one-year session is a password that never expires.
const (
	MinAbsoluteLifetime = 5 * time.Minute
	MaxAbsoluteLifetime = 30 * 24 * time.Hour
	MinIdleTimeout      = time.Minute
)

// Adjustment records a policy value this code had to correct, so a clamp is
// reported rather than silent. Mirrors authn.Adjustment.
type Adjustment struct {
	Field      string
	Configured string
	Applied    string
	Reason     string
}

// Sanitize clamps a policy into its bounds, reporting every correction.
func (p Policy) Sanitize() (Policy, []Adjustment) {
	var adjustments []Adjustment

	record := func(field string, configured, applied time.Duration, reason string) {
		adjustments = append(adjustments, Adjustment{
			Field:      field,
			Configured: configured.String(),
			Applied:    applied.String(),
			Reason:     reason,
		})
	}

	if p.AbsoluteLifetime < MinAbsoluteLifetime {
		record("session_lifetime_hours", p.AbsoluteLifetime, MinAbsoluteLifetime,
			"below the floor this service enforces regardless of configuration; "+
				"a session shorter than this is a denial of service on your own users")
		p.AbsoluteLifetime = MinAbsoluteLifetime
	}
	if p.AbsoluteLifetime > MaxAbsoluteLifetime {
		record("session_lifetime_hours", p.AbsoluteLifetime, MaxAbsoluteLifetime,
			"above the ceiling; a session this long is a credential that effectively never expires")
		p.AbsoluteLifetime = MaxAbsoluteLifetime
	}

	// An idle timeout longer than the absolute lifetime can never fire, which
	// makes it a setting that appears to do something and does not.
	if p.IdleTimeout > p.AbsoluteLifetime {
		record("idle_timeout", p.IdleTimeout, p.AbsoluteLifetime,
			"longer than the absolute lifetime, so it could never fire")
		p.IdleTimeout = p.AbsoluteLifetime
	}
	if p.IdleTimeout > 0 && p.IdleTimeout < MinIdleTimeout {
		record("idle_timeout", p.IdleTimeout, MinIdleTimeout, "below the floor")
		p.IdleTimeout = MinIdleTimeout
	}

	return p, adjustments
}

// WithLifetimeHours returns the policy with its absolute lifetime replaced by
// an organization's configured one (P2-10).
//
// The idle timeout is deliberately kept. It is a property of how this service
// treats inactivity rather than something an organization configures — there is
// no field for it in `settings`, and inventing one here would be a policy
// nobody asked for and nobody can see.
//
// Out-of-range hours are clamped by Sanitize, the same as every other source.
func (p Policy) WithLifetimeHours(hours int) Policy {
	if hours <= 0 {
		return p
	}
	out := p
	out.AbsoluteLifetime = time.Duration(hours) * time.Hour
	sanitized, _ := out.Sanitize()
	return sanitized
}

// PolicyFromHours builds a Policy from organizations.settings'
// session_lifetime_hours, keeping the default idle timeout.
func PolicyFromHours(hours int) Policy {
	if hours <= 0 {
		return DefaultPolicy
	}
	return Policy{
		AbsoluteLifetime: time.Duration(hours) * time.Hour,
		IdleTimeout:      DefaultPolicy.IdleTimeout,
	}
}

// --- the cookie ------------------------------------------------------------------

// Cookie builds the Set-Cookie for a new session.
//
// Secure is unconditional, and it used to be a parameter. gosec flagged that,
// and it was right for a better reason than the one it gives: a parameter is
// the only way the control could ever be off, and there is no case that needs
// it. The __Host- prefix requires Secure, so a non-Secure cookie would not be
// stored at all; and browsers treat http://localhost as a secure context, so
// local development works without an exemption. A flag whose only reachable
// value is true is a flag someone eventually passes false to.
//
// MaxAge is deliberately absent: this is a session cookie in the browser's
// sense, discarded when the browser closes. The server-side expiry is what
// bounds it, and a persistent cookie would outlive the session it names —
// leaving a browser holding a credential the server has already forgotten.
func Cookie(token Token) *http.Cookie {
	return &http.Cookie{
		Name:  CookieName,
		Value: token.Reveal(),

		// Required by the __Host- prefix, and correct independently: the
		// cookie is for this origin and every path under it, and for no
		// subdomain.
		Path:   "/",
		Domain: "",

		Secure:   true,
		HttpOnly: true,

		// Lax rather than Strict, and this is a requirement rather than a
		// compromise. /oauth/authorize is reached by a top-level GET
		// navigation from the consumer application; Strict withholds the
		// cookie on exactly that navigation, so silent SSO would never work.
		// Lax sends it on top-level GET and withholds it on cross-site POST,
		// which is the CSRF-relevant half.
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearCookie builds the Set-Cookie that removes the session cookie.
//
// Every attribute must match the one that set it or the browser treats it as a
// different cookie and keeps the original — a logout that appears to work and
// does not.
func ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Domain:   "",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// FromRequest extracts a well-formed session token from a request.
//
// Returns ok=false for absent and for malformed alike. The caller cannot
// distinguish them and should not: both mean "no session", and an error page
// that says which is a free oracle.
func FromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c == nil {
		return "", false
	}
	value := strings.TrimSpace(c.Value)
	if !ValidToken(value) {
		return "", false
	}
	return value, true
}

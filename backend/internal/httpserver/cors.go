package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// Cross-origin resource sharing (P1-29, closing PG-17).
//
// Two policies, because there are two genuinely different kinds of endpoint
// here and one answer is wrong for one of them.
//
//	public     /.well-known/*, /oauth/token     Access-Control-Allow-Origin: *
//	per-app    /oauth/userinfo, /v1/*           the calling application's list
//
// **Why `*` is right on the public set.** Those endpoints are not
// authenticated by anything a browser attaches on its own. The token endpoint
// hands nothing to a caller who cannot present a valid authorization code
// *and* the PKCE verifier that produced its challenge; the discovery documents
// are public by definition. An attacker's page reading them learns nothing it
// could not learn with curl. Without a browser-usable policy here, no public
// client can complete a login at all — which is the state `PG-17` recorded and
// `P1-27` measured.
//
// **Why `*` is wrong on the rest.** `/oauth/userinfo` returns an email address
// and `/v1/*` returns an organization's users. The origin permitted to READ
// such a response has to be one the application declared, in
// `applications.allowed_origins` — per application, so one tenant's registered
// origin cannot read another tenant's data. That was `PG-17`'s whole objection
// to an instance-wide allowlist.
//
// **Credentials are never allowed, anywhere.** Not on the public set, where
// `*` forbids it outright, and not on the restricted set either — where it
// would be permitted and is still not wanted. `/v1/*` and `/oauth/userinfo`
// authenticate a bearer token and nothing else; they never consult a cookie.
// Allowing credentials would let a browser attach this service's session
// cookie to a cross-origin request that has no use for it, which is ambient
// authority created for no reason. The console therefore does not send
// credentials on API calls, and does not need to.
//
// Everything else — the hosted login page, `/oauth/authorize`, `/oidc/logout`,
// the probes — gets no CORS headers at all. They are navigations, not fetches,
// and a navigation has never been subject to this.

const (
	headerOrigin = "Origin"
	headerACAO   = "Access-Control-Allow-Origin"
	headerACAM   = "Access-Control-Allow-Methods"
	headerACAH   = "Access-Control-Allow-Headers"
	headerACAC   = "Access-Control-Allow-Credentials"
	headerACMA   = "Access-Control-Max-Age"
	headerACRM   = "Access-Control-Request-Method"
	headerVary   = "Vary"

	// Ten minutes. Long enough that a console session is not re-preflighting
	// constantly, short enough that a changed registration takes effect the
	// same morning rather than the next day.
	preflightMaxAge = "600"
)

// OriginChecker answers whether a browser origin may read a response.
//
// An interface so this file has no opinion about where the answer comes from,
// and so the header logic can be tested without a database.
type OriginChecker interface {
	// AnyApplicationAllows reports whether SOME application registers this
	// origin. Used for the preflight, which carries no credential.
	AnyApplicationAllows(r *http.Request, origin string) bool

	// ApplicationAllows reports whether the application identified by
	// clientID registers this origin.
	ApplicationAllows(r *http.Request, clientID, origin string) bool
}

// PublicCORS allows any origin to read a response, without credentials.
func PublicCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerOrigin) == "" {
			// Not a cross-origin request. Adding the headers anyway would be
			// harmless and would also make every cache entry vary for nothing.
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set(headerACAO, "*")
		// `*` and credentials are mutually exclusive in the specification and
		// a browser refuses the combination outright. Stating it means nobody
		// later "fixes" that refusal by echoing the origin instead, which is
		// how a public endpoint acquires ambient authority.
		w.Header().Set(headerACAC, "false")

		if isPreflight(r) {
			w.Header().Set(headerACAM, "POST, GET, OPTIONS")
			w.Header().Set(headerACAH, "Content-Type, Authorization")
			w.Header().Set(headerACMA, preflightMaxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RestrictedCORS allows only origins the calling application registered.
//
// This is not an authorization check and must never be mistaken for one. A
// refused origin does not refuse the request: the handler still runs and the
// browser simply declines to hand the body to the page. What actually decides
// whether the request is permitted is the bearer check underneath, and these
// endpoints consult nothing a browser attaches on its own — so this layer is
// defence in depth over an API that has no ambient authority to abuse.
func RestrictedCORS(checker OriginChecker, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(headerOrigin)
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Vary first, and on every path including a refusal.
			//
			// The response headers now depend on Origin, so a cache that
			// ignored it would serve an allowed origin's headers to a
			// disallowed one.
			w.Header().Add(headerVary, headerOrigin)

			if isPreflight(r) {
				// A preflight carries no Authorization header — that is what
				// a preflight is — so the specific application cannot be
				// known yet. The actual request is checked against it.
				if checker != nil && checker.AnyApplicationAllows(r, origin) {
					w.Header().Set(headerACAO, origin)
					w.Header().Set(headerACAM, "GET, POST, PATCH, DELETE, OPTIONS")
					w.Header().Set(headerACAH, "Authorization, Content-Type, Idempotency-Key")
					w.Header().Set(headerACMA, preflightMaxAge)
				}
				// 204 either way. Answering 403 to a refused preflight tells a
				// page which origins are registered, one guess at a time.
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if allowed(checker, r, origin) {
				w.Header().Set(headerACAO, origin)
			} else if log != nil {
				// Worth a line. A consumer whose fetch fails with "CORS" and
				// nothing else cannot tell a missing registration from a typo
				// in one, and this is the only place that knows which.
				log.Debug("cross-origin read refused", "origin", origin, "path", r.URL.Path)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// allowed decides an actual (non-preflight) cross-origin request.
//
// The application comes from the bearer token's `client_id`, read WITHOUT
// verifying the signature — and that is deliberate rather than an oversight.
//
// A forged `client_id` buys nothing: the origin still has to appear in that
// application's registered list, so naming somebody else's application only
// makes the check fail. Naming your own is exactly what an honest client does.
// The signature is verified a few microseconds later by the bearer middleware,
// which is what decides whether the request runs at all; duplicating that here
// would cost an RSA verification per request to answer a question whose wrong
// answer is "the browser hides a response the caller was entitled to".
//
// When no usable token is present the request is about to be refused with an
// error envelope containing no personal data, so the preflight rule applies:
// any registered origin may read it. Without that, the console cannot see its
// own 401 and cannot tell an expired token from an unreachable service.
func allowed(checker OriginChecker, r *http.Request, origin string) bool {
	if checker == nil {
		return false
	}
	if clientID := clientIDFromBearer(r.Header.Get("Authorization")); clientID != "" {
		return checker.ApplicationAllows(r, clientID, origin)
	}
	return checker.AnyApplicationAllows(r, origin)
}

// clientIDFromBearer reads the `client_id` claim without verifying anything.
//
// Returns "" for anything malformed. See allowed() for why an unverified read
// is sound here and what is NOT being claimed by it.
func clientIDFromBearer(header string) string {
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.ClientID
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get(headerACRM) != ""
}

// CORS applies whichever policy a path deserves.
//
// One middleware rather than two registrations, because the generated router
// serves both kinds: `/.well-known/*` is public, `/v1/*` is not, and both
// arrive through the same mount (ADR-013 — the served paths are the specified
// paths, so they cannot be split across routers without leaving that
// guarantee).
//
// The probes fall through to the restricted policy and get no headers at all,
// which is right: nothing fetches them from a page.
func CORS(checker OriginChecker, log *slog.Logger) func(http.Handler) http.Handler {
	restricted := RestrictedCORS(checker, log)
	return func(next http.Handler) http.Handler {
		public := PublicCORS(next)
		private := restricted(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicCORSPath(r.URL.Path) {
				public.ServeHTTP(w, r)
				return
			}
			private.ServeHTTP(w, r)
		})
	}
}

// isPublicCORSPath names the endpoints that may be read from any origin.
//
// A closed list, written out rather than derived, so adding an endpoint to it
// is an edit somebody reviews. Every entry has to satisfy both halves of the
// argument in this file's header: its content is public, and it is never
// authenticated by anything the browser attaches on its own.
func isPublicCORSPath(path string) bool {
	return strings.HasPrefix(path, "/.well-known/") || path == "/oauth/token"
}

package management

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What happens before and after every /v1 handler:
//
//	authenticate → authorize → scope → handle
//
// Each step is here rather than in a handler, because docs/PLAN/02 FR-14 means
// there will be dozens of handlers and a control implemented per handler is a
// control that will be missing from one of them.

// Verifier checks a compact JWS of an expected type.
type Verifier interface {
	Verify(compact, wantType string) ([]byte, error)
}

// Grants reads a caller's manager roles.
type Grants interface {
	GrantsFor(ctx context.Context, db *postgres.DB, userID string) ([]Grant, error)
}

// Sessions answers whether the session behind a token is still live.
//
// Optional. A client_credentials token has no session and needs none; a token
// issued from a login does, and checking it is what makes an administrator's
// logout take effect on this API rather than ten minutes later.
type Sessions interface {
	IsLive(ctx context.Context, sessionID string, now time.Time) bool
}

// Middleware carries what every /v1 request needs.
type Middleware struct {
	Issuer   string
	Verifier Verifier
	Grants   Grants
	Sessions Sessions
	DB       *postgres.DB
	Log      *slog.Logger

	Now func() time.Time
}

func (m *Middleware) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// callerKey is the context key for the authenticated caller.
type callerKey struct{}

// CallerFrom returns the caller a Require-wrapped handler is serving.
//
// The bool is not decoration: a handler reached without the middleware has no
// caller, and silently treating that as an anonymous request is how an
// endpoint ends up unauthenticated. Handlers are expected to treat false as a
// programming error.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// scopeKey carries the tenant the request runs in.
type scopeKey struct{}

// ScopeFrom returns the organization a request is scoped to, and whether the
// caller reached it as an INSTANCE_OWNER acting across tenants.
func ScopeFrom(ctx context.Context) (orgID string, instanceScoped bool) {
	if s, ok := ctx.Value(scopeKey{}).(scope); ok {
		return s.orgID, s.instanceScoped
	}
	return "", false
}

type scope struct {
	orgID          string
	instanceScoped bool
}

// Require wraps a handler with the authentication and permission checks it
// declares.
//
// Every /v1 route is registered through this. A route registered without it
// has no caller in its context, which CallerFrom reports — but the real
// protection is that the Requirement's zero value is unsatisfiable, so a route
// wrapped with a forgotten requirement is unreachable rather than open.
func (m *Middleware) Require(req Requirement, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, err := m.authenticate(r)
		if err != nil {
			m.refuse(w, r, err)
			return
		}

		// The organization the REQUEST addresses, which is not necessarily the
		// caller's own: that is what makes an INSTANCE_OWNER useful and what
		// makes forgetting the distinction a cross-tenant hole.
		target := chi.URLParam(r, "org_id")
		if target == "" && req.Scope == ScopeOrganization {
			target = caller.OrgID
		}

		decision := Authorize(caller, req, target)
		if !decision.Allowed {
			if m.Log != nil {
				m.Log.Info("a management request was refused",
					"user_id", caller.UserID, "target_org", target, "reason", decision.Reason)
			}
			// The reason stays in the log. "you are an ORG_ADMIN and this needs
			// ORG_OWNER" told to a caller probing an organization they do not
			// administer confirms it exists.
			//
			// And WHICH refusal depends on whether the caller can see the target
			// at all. A caller holding nothing over it is told 404 — the same
			// answer an organization that does not exist gets — because a 403
			// would answer "is this id real?" for anybody willing to send a
			// request per guess (abuse case A-3, docs/SECURITY/02 §2, §14).
			fault := Fault{
				Class:   Forbidden,
				Message: "You do not have permission to perform this action.",
				Reason:  decision.Reason,
			}
			if decision.Invisible {
				fault.Class = NotFound
				fault.Message = "The requested resource was not found."
			}
			m.refuse(w, r, fault)
			return
		}

		ctx := context.WithValue(r.Context(), callerKey{}, caller)
		ctx = context.WithValue(ctx, scopeKey{}, scope{
			orgID:          target,
			instanceScoped: decision.InstanceScoped,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// InScope runs fn inside the transaction the request is scoped to.
//
// Every handler's database work goes through this, so RLS confines it without
// any handler carrying an org_id predicate.
//
// **A request that names a target organization is scoped to THAT organization,
// whoever the caller is.** An INSTANCE_OWNER acting on somebody else's tenant
// runs under WithTenant(target), not WithInstanceScope — which is both more
// confined and, it turns out, the only thing that works.
//
// P1-16 found the alternative the hard way. Instance scope sets
// current_org_id() to NULL, and every tenant policy is `org_id =
// current_org_id()`, which is false for every row when the value is NULL. An
// INSTANCE_OWNER reading another organization's users through the
// instance-scoped path therefore saw NOTHING — not a refusal, an empty result.
// The capability the role exists for silently did not work, and the P1-15 test
// that covered it passed because its handler never touched a tenant-scoped
// table.
//
// WithInstanceScope remains for the operations that genuinely span
// organizations and cannot name one: listing organizations, and creating the
// first row of a tenant that does not exist yet. Those are the narrow uses its
// own doc comment lists.
func (m *Middleware) InScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, instanceScoped := ScopeFrom(ctx)

	if orgID == "" {
		// No target. Genuinely cross-tenant, and takes the named, logged,
		// audited path — there is no branch where a missing scope silently
		// means "every tenant", because WithTenant("") already returns an
		// error, which P1-14 found the hard way.
		return m.DB.WithInstanceScope(ctx,
			"an INSTANCE_OWNER acting across organizations through the management API", fn)
	}

	if instanceScoped {
		// A cross-tenant action, confined to one tenant. The database scope is
		// the target's, so nothing else is reachable — but it is still one
		// organization acting on another and must not be silent.
		//
		// WithInstanceScope's own audit hook does not fire on this path, which
		// is why the line is written here rather than assumed.
		if m.Log != nil {
			caller, _ := CallerFrom(ctx)
			m.Log.Info("an INSTANCE_OWNER is acting on another organization",
				"user_id", caller.UserID, "caller_org", caller.OrgID, "target_org", orgID)
		}
	}

	return m.DB.WithTenant(ctx, orgID, fn)
}

// --- authentication ------------------------------------------------------------------

// accessToken is what this API reads from a bearer token.
//
// Deliberately a different validation from P1-08's, and the difference is one
// field: userinfo REQUIRES `sid`, because it describes a person and a
// client_credentials token has no person behind it. This API is also used by
// service accounts, so `sid` is optional here — and checked when present,
// which is what makes an administrator's logout take effect immediately
// rather than at the token's expiry.
type accessToken struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	ClientID  string `json:"client_id"`
	OrgID     string `json:"org_id"`
	SessionID string `json:"sid"`
}

// clockSkew tolerates another clock being slightly ahead, on `iat` only.
//
// Never on `exp`: this service issued the token and stamped both with its own
// clock, so leeway there would extend the life of every access token by the
// tolerance.
const clockSkew = 30 * time.Second

func (m *Middleware) authenticate(r *http.Request) (Caller, error) {
	presented, ok := bearer(r.Header.Get("Authorization"))
	if !ok {
		return Caller{}, Fault{
			Class: Unauthenticated, Message: "Authentication is required.",
			Reason: "no bearer token",
		}
	}

	// `at+jwt` (RFC 9068). An ID token presented here is refused before its
	// payload is read — the same substitution abuse case P1-08 covers,
	// arriving at the management surface.
	payload, err := m.Verifier.Verify(presented, signing.TypeAccessToken)
	if err != nil {
		return Caller{}, unusable("signature or type: " + err.Error())
	}

	var token accessToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return Caller{}, unusable("the payload is not JSON")
	}

	switch {
	case token.Issuer != m.Issuer:
		return Caller{}, unusable("another issuer")
	case token.Audience != m.Issuer:
		// A token minted for a consumer's resource server must not administer
		// the platform. This is the check that keeps those two apart.
		return Caller{}, unusable("another audience")
	case token.ExpiresAt == 0 || m.now().Unix() >= token.ExpiresAt:
		return Caller{}, unusable("expired")
	case token.IssuedAt > m.now().Add(clockSkew).Unix():
		return Caller{}, unusable("issued in the future")
	case token.Subject == "" || token.OrgID == "":
		return Caller{}, unusable("no subject or organization")
	}

	// A session, when there is one. A client_credentials token has none.
	if token.SessionID != "" && m.Sessions != nil {
		if !m.Sessions.IsLive(r.Context(), token.SessionID, m.now()) {
			return Caller{}, unusable("the session behind the token has ended")
		}
	}

	// The roles, from the database, on this request. See store.go for why.
	grants, err := m.Grants.GrantsFor(r.Context(), m.DB, token.Subject)
	if err != nil {
		if m.Log != nil {
			m.Log.Error("reading a caller's manager roles failed", "error", err.Error())
		}
		return Caller{}, Fault{Class: Internal, Message: "An unexpected error occurred."}
	}

	return Caller{
		UserID:   token.Subject,
		ClientID: token.ClientID,
		OrgID:    token.OrgID,
		Grants:   grants,
	}, nil
}

// unusable is the one answer for every token that cannot be used.
//
// Expired, forged, wrong audience, wrong issuer, revoked session: all
// UNAUTHENTICATED with the same message. A caller holding a captured token
// must not be able to ask this API whether its owner has logged out.
func unusable(reason string) Fault {
	return Fault{
		Class:   Unauthenticated,
		Message: "The access token is not valid.",
		Reason:  reason,
	}
}

func (m *Middleware) refuse(w http.ResponseWriter, _ *http.Request, err error) {
	var fault Fault
	if ok := asFault(err, &fault); ok && fault.Class == Unauthenticated {
		// RFC 6750's challenge, so an OAuth client library knows to refresh
		// rather than to give up.
		w.Header().Set("WWW-Authenticate",
			`Bearer realm="`+m.Issuer+`", error="invalid_token"`)
		if m.Log != nil && fault.Reason != "" {
			m.Log.Info("a management request presented an unusable token", "reason", fault.Reason)
		}
	}
	WriteError(w, err)
}

func asFault(err error, out *Fault) bool {
	f, ok := err.(Fault)
	if ok {
		*out = f
	}
	return ok
}

// bearer extracts the credential from an Authorization header.
func bearer(header string) (string, bool) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	credential := strings.TrimSpace(parts[1])
	return credential, credential != ""
}

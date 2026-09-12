// Command authservice is the Auth Service: OIDC/OAuth2 provider,
// authentication, authorization, and the Management REST API.
//
// This file does wiring only. Business logic lives in internal/ packages, so
// the modular monolith described in docs/PLAN/07-BACKEND-ARCHITECTURE.md can be
// split later without a rewrite.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/login"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/oauth/userinfo"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/oidc"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// version is set at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)".
var version = "dev"

// partitionMonthsAhead is how much events-partition runway to maintain.
//
// Three months rather than one: a service that is down for a while, or a
// maintenance tick that fails quietly, still has room before the failure
// becomes an outage. The cost of an unused empty partition is nothing.
const partitionMonthsAhead = 3

func main() {
	// -healthcheck exists because the runtime image is distroless: it has no
	// shell and no curl, so a container healthcheck has to be the binary
	// itself (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §17 — a runtime image
	// carrying a shell just to run a healthcheck is attack surface added for
	// operational convenience).
	healthcheck := flag.Bool("healthcheck", false, "probe the local readiness endpoint and exit 0 if ready")
	flag.Parse()

	if *healthcheck {
		os.Exit(probeReadiness())
	}

	if err := run(); err != nil {
		// Written to stderr rather than through the logger, because the most
		// likely reason we are here is that configuration failed to load and
		// there is no configured logger yet.
		fmt.Fprintf(os.Stderr, "authservice: %v\n", err)
		os.Exit(exitCode(err))
	}
}

func run() error {
	// Fail fast and loudly on invalid configuration. A service that boots with
	// a missing signing-key configuration is worse than one that refuses to
	// boot (TASKS P0-04).
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := observability.NewLogger(os.Stdout, observability.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: "authservice",
		Version: version,
	})

	log.Info("starting",
		"environment", string(cfg.Environment),
		"issuer", cfg.Issuer,
	)

	// Every secret is resolved here, before anything is opened or started.
	//
	// The ordering is deliberate. When this ran later, next to the listener
	// that used it, an unreadable secret file produced a confusing cascade: the
	// process returned an error, the deferred pool close ran, and the audit
	// maintenance goroutine — already started — logged "sql: database is
	// closed" on its way out. Three symptoms, one of them wrong, none of them
	// the cause. Resolving up front means a bad secret reference is one error
	// message and nothing else.
	secrets, err := resolveSecrets(cfg)
	if err != nil {
		return err
	}

	// Cancelled on SIGINT or SIGTERM. Kubernetes sends SIGTERM before removing
	// a pod from the load balancer, which is the window graceful shutdown uses
	// to drain in-flight requests (docs/PLAN/14-DEPLOYMENT.md).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metrics := observability.NewMetrics("authservice", version)

	// Tracing is off unless an OTLP endpoint is configured. The shutdown flush
	// matters: an unflushed exporter drops the spans from the last seconds of
	// a process, which are exactly the ones present when it crashed.
	shutdownTracing, err := observability.InitTracing(ctx, observability.TracingConfig{
		Endpoint:       cfg.Tracing.Endpoint,
		Insecure:       cfg.Tracing.Insecure,
		SampleRatio:    cfg.Tracing.SampleRatio,
		ServiceName:    "authservice",
		ServiceVersion: version,
		Environment:    string(cfg.Environment),
	}, log)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if ferr := shutdownTracing(flushCtx); ferr != nil {
			log.Error("flushing traces", "error", ferr.Error())
		}
	}()

	db, err := postgres.Open(ctx, cfg.Postgres, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Error("closing postgres", "error", cerr.Error())
		}
	}()

	// Refuse to start if the connected role can bypass row-level security.
	//
	// Without this the failure is silent and total: point AUTH_POSTGRES_DSN at
	// auth_owner — entirely plausible while debugging a permissions error —
	// and every RLS policy stops applying. Nothing errors, no test fails, and
	// cross-tenant isolation is gone. Refusing to boot is the only response
	// proportionate to that (docs/PLAN/08 Part B, P0-08).
	if err := db.AssertRoleIsNotPrivileged(ctx); err != nil {
		return fmt.Errorf("database role check: %w", err)
	}

	// docs/PLAN/13 names pool utilisation explicitly: exhaustion presents as latency
	// at every endpoint at once, which looks like a dozen unrelated problems
	// until someone thinks to check the pool.
	if err := metrics.RegisterDBStats("postgres", db.SQL()); err != nil {
		return fmt.Errorf("register db metrics: %w", err)
	}

	// Forwarding to an external SIEM is nil until P5-08 supplies one. The seam
	// exists now because docs/PLAN/09 § Audit wants the log forwarded, and adding
	// the seam later would mean touching every call site.
	auditor := audit.NewWriter(db, log, nil)
	auditor.SetObserver(auditObserver{m: metrics})

	// docs/PLAN/08 Part B requires the cross-tenant database path to be auditable.
	// A hook rather than a direct call, because audit already imports postgres
	// and importing back would be a cycle. It runs outside the scoped
	// transaction: failing to record the access should not roll back the
	// access, which is the opposite trade from business events (ADR-012).
	db.SetInstanceScopeHook(func(hookCtx context.Context, reason string) {
		metrics.InstanceScopedAccess.WithLabelValues(reason).Inc()
	})

	// Partition maintenance runs for the life of the process.
	//
	// Without it the service works fine until the last events partition's range
	// ends, at which point every INSERT into events fails — and since every
	// security-sensitive action writes an audit event (docs/PLAN/09 § Audit), every
	// such action fails with it. At midnight on the first of a month, with no
	// deploy to correlate against (P0-12).
	go auditor.Run(ctx, partitionMonthsAhead)

	// Redis: the session lookup cache (P1-11).
	//
	// docs/PLAN/12 gives /oauth/authorize 150ms at p95 for the whole silent-SSO
	// request, and a Redis round trip is how that is met. It is a cache, not a
	// store: PostgreSQL is authoritative (ADR-003), so Redis being down costs
	// latency rather than correctness — which is why it is a readiness check
	// rather than a startup requirement.
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() { _ = rdb.Close() }()

	sessions := session.NewManager(
		db,
		session.NewCache(rdb, sessionObserver{metrics}),
		auditor,
		log,
	)

	// Expired and long-revoked sessions are removed on a schedule rather than
	// accumulating (P1-11 DoD item 4). Bounded per run, on the same pattern as
	// the partition maintenance above: a long-neglected table is caught up over
	// several runs instead of in one enormous transaction.
	go runSessionSweep(ctx, sessions, log)

	health := &httpserver.Health{
		Checks:  []httpserver.Checker{db, redisChecker{rdb}},
		Timeout: 2 * time.Second,
	}

	admin := httpserver.NewAdmin(cfg.Admin, httpserver.AdminDeps{
		Logger:  log,
		Metrics: metrics.Handler(),
		Token:   secrets.adminToken,
	})
	go func() {
		// A failure here is a visibility problem. Taking authentication down
		// over a metrics listener would be a worse outcome than the one it
		// would be reporting.
		if aerr := admin.Run(ctx); aerr != nil {
			log.Error("admin listener stopped", "error", aerr.Error())
		}
	}()

	// Password policy (P1-02).
	//
	// The policy VALUES are not here — they are read per organization from
	// organizations.settings, which is what lets P2-14 make them editable
	// without touching enforcement. What is built here is the breach-corpus
	// client, because it is a network dependency and those belong in main.
	//
	// A nil checker is a first-class state, not an oversight: authn.CheckBreach
	// reports OutcomeDisabled for it, which lands in the same metric as a
	// failure. "Somebody turned it off" and "it has been broken for three
	// weeks" must be visible in the same place (ADR-015).
	//
	// Nothing consumes these yet, and the startup log says so rather than
	// leaving an operator to infer from a quiet metric that the check is
	// working. The first password-set path is P1-12's hosted form; P1-19's
	// user-creation endpoint is the second.
	passwords := newPasswordChecks(cfg, log)
	log.Info("password policy ready",
		"breach_check", passwords.state,
		"enforced_at", "no password-set path exists yet (P1-12, P1-19)")

	// The signing key set, read from the database rather than from a
	// configured path (P1-03).
	//
	// This is what makes an application rollback safe: key state lives in
	// `signing_keys`, so rolling back the binary cannot invalidate tokens
	// signed under a newer key (docs/PLAN/14 § Rollback Strategy). A single key
	// reference in configuration would put that state in the deployment, which
	// is the thing being rolled back.
	keyStore := signing.NewStore(db.SQL(), config.NewSecretResolver(cfg.Environment != config.EnvLocal), signing.PurposeOIDC)

	keys := signing.NewCache(func() (*signing.KeySet, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return keyStore.Load(loadCtx)
	}, signing.DefaultCacheTTL)

	// Load once at startup so a broken key set is a refusal to boot rather
	// than a service that accepts requests and fails every login. The latter
	// is a worse outage and a much harder one to read (P1-03).
	//
	// Absence of ANY key is tolerated here and only here: a fresh deployment
	// has no keys until `keyctl generate && keyctl rotate` runs, and refusing
	// to start would make the service impossible to bootstrap. Discovery then
	// reports an empty key set, which is accurate.
	// The client store. P1-18 will expose it over HTTP; here it exists so the
	// authorization endpoint can resolve a client_id.
	clients := client.NewStore(auditor)

	authorizeHandler := &authorize.Handler{
		Clients:  clientLookup{store: clients, db: db},
		Sessions: sessions,
		Store:    authorize.NewStore(rdb),
		Observer: authorizeObserver{metrics},
		Log:      log,

		// P1-12 serves this. Until then the redirect lands on a 404, which is
		// visible and honest — better than pretending a session exists.
		LoginPath: "/login",
		Policy:    session.DefaultPolicy,
	}

	tokenHandler := &token.Handler{
		Issuer:   cfg.Issuer,
		Clients:  clientLookup{store: clients, db: db},
		Codes:    authorize.NewStore(rdb),
		Sessions: sessionLiveness{sessions: sessions, policy: session.DefaultPolicy},
		Refresh:  token.NewRefreshStore(),
		Signer:   signing.NewSigner(keys),
		DB:       db,
		Audit:    auditor,
		Observer: tokenObserver{metrics},
		Log:      log,
	}

	// Introspection and revocation (P1-09). One handler, two routes: they
	// share client authentication and the ownership rule, and splitting them
	// would be duplicating the rule that says a client may only see or destroy
	// its own tokens.
	lifecycleHandler := &token.LifecycleHandler{
		Issuer:   cfg.Issuer,
		Clients:  clientLookup{store: clients, db: db},
		Verifier: signing.NewVerifier(keys),
		Refresh:  refreshLookup{store: token.NewRefreshStore(), db: db},
		Sessions: sessionLiveness{sessions: sessions, policy: session.DefaultPolicy},
		Tenant:   db,
		Audit:    auditor,
		Observer: lifecycleObserver{metrics},
		Log:      log,
	}

	// The userinfo endpoint (P1-08). The first consumer signing.Verifier has
	// ever had — until now the service could sign tokens and had never once
	// verified one of its own.
	userInfoHandler := &userinfo.Handler{
		Issuer:   cfg.Issuer,
		Verifier: signing.NewVerifier(keys),
		Subjects: userinfo.NewStore(),
		DB:       db,
		Observer: userInfoObserver{metrics},
		Log:      log,
	}

	// The client-IP source, and the rate limiter it feeds (P1-13).
	//
	// Resolved before the login handler is built, because the handler must not
	// be able to fall back to reading RemoteAddr on its own: on this
	// deployment that is the Docker gateway and is the same for every user in
	// the world.
	clientIP, badCIDRs := httpserver.NewClientIP(cfg.HTTP.ClientIPHeader, cfg.HTTP.TrustedProxyCIDRs)
	for _, bad := range badCIDRs {
		// Reported rather than dropped: narrowing the trusted set to nothing
		// looks identical to working and would quietly turn every user into
		// one IP.
		log.Error("AUTH_TRUSTED_PROXY_CIDRS contains an entry that is not a CIDR",
			"entry", bad, "remedy", "correct or remove it; the other entries are still in use")
	}
	if !clientIP.Configured() {
		// Loud, and it does NOT disable the per-IP bound. A limiter that
		// quietly turns itself off is worse than one that is loudly
		// misconfigured — the second gets fixed.
		log.Warn("no client IP source is configured; per-IP rate limiting will treat "+
			"every request as one client if anything is proxying in front of this service",
			"remedy", "set AUTH_CLIENT_IP_HEADER and AUTH_TRUSTED_PROXY_CIDRS")
	}

	limiter := ratelimit.New(rdb, rateLimitObserver{metrics}, log)

	// The Management API's /v1 chain (P1-15).
	//
	// Assembled once here and handed to the router, so that P1-16 onward add
	// ROUTES rather than middleware. docs/PLAN/02 FR-14 means there will be
	// dozens of endpoints, and a chain each of them assembles for itself is a
	// chain one of them will assemble wrong.
	//
	// The ORDER lives in management.Chain.Handle, with the reasoning for each
	// position. It is not a detail: putting the audit guard outside the
	// idempotency middleware, for instance, makes every replay look like an
	// unaudited mutation, and an alarm that fires in normal operation is an
	// alarm somebody turns off.
	// The organization endpoints (P1-16), the first real /v1 surface.
	organizations := &organization.Handler{
		Store:    organization.NewStore(),
		DB:       db,
		Audit:    auditor,
		Log:      log,
		Sessions: sessions,
	}

	projects := &project.Handler{
		Store: project.NewStore(), DB: db, Audit: auditor, Log: log,
	}

	// Its own client.Store, built over an audit recorder that marks P1-15's
	// per-request trail. The `clients` store above writes straight at the
	// audit writer, which is right for the token endpoint — there is no HTTP
	// guard there — and wrong here.
	applications := application.New(db, auditor, log)
	roles := role.New(db, auditor, log)
	grants := grant.New(db, auditor, log)

	// Outbound email (ADR-018). A nil sender is a valid deployment: invitations
	// still create their token and the response says the message was not sent.
	mailer, err := mail.FromURL(cfg.Mail.SMTPURL, cfg.Mail.From, log, mailObserver{metrics})
	if err != nil {
		return fmt.Errorf("configuring outbound mail: %w", err)
	}
	if mailer == nil {
		// Said once, at startup, rather than per request — a deployment that
		// cannot send mail should know before somebody invites their first user.
		log.Warn("no outbound mail is configured; invitations and password resets will not be delivered",
			"remedy", "set AUTH_SMTP_URL and AUTH_MAIL_FROM")
	}
	if cfg.Mail.AllowCleartext && cfg.Environment != config.EnvLocal {
		// Every start, deliberately. This is a claim the DEPLOYMENT makes
		// about its own topology — that the mail hop never leaves the host —
		// and the service cannot check it. A claim nobody is reminded of is a
		// claim that outlives the arrangement that justified it: the sidecar
		// gets replaced by a relay across the network and the variable stays.
		log.Warn("cleartext SMTP is permitted outside local development",
			"smtp_url", redactedSMTPHost(cfg.Mail.SMTPURL),
			"why_this_is_set", "AUTH_SMTP_ALLOW_CLEARTEXT=true",
			"what_it_asserts", "the mail server is a sidecar on this host and the hop leaves no machine",
			"if_that_is_no_longer_true", "unset it and use smtps://")
	}

	// The email-amplification bound (card step 4, docs/SECURITY/02 §10). Its own
	// Quotas instance because the bound is different from /v1's per-client one:
	// five messages an hour to one address is generous for onboarding and
	// useless for flooding somebody.
	mailQuotas := ratelimit.NewQuotas(rdb, rateLimitObserver{metrics}, log).
		WithQuota(ratelimit.Quota{Limit: 5, Window: time.Hour})

	userStore := user.NewStore()
	users := &user.Handler{
		Store:     userStore,
		DB:        db,
		Audit:     auditor,
		Log:       log,
		Sessions:  sessions,
		Refresh:   token.NewRefreshStore(),
		BaseURL:   cfg.Issuer,
		MailLimit: mailQuotas,
	}
	if mailer != nil {
		users.Mailer = mailer
	}

	v1 := &management.Chain{
		Auth: &management.Middleware{
			Issuer:   cfg.Issuer,
			Verifier: signing.NewVerifier(keys),
			Grants:   management.NewRoleStore(),
			Sessions: sessionLiveness{sessions: sessions, policy: session.DefaultPolicy},
			DB:       db,
			Log:      log,
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, rateLimitObserver{metrics}, log),
		},
		Idempotency: &management.Idempotency{
			Claims: management.NewDBClaims(db),
			Log:    log,
		},
		// Every /v1 handler that inspects the keys a caller actually sent needs
		// the raw body, because `additionalProperties: false` is not enforced
		// at runtime by the generated code.
		BufferBody: true,

		Audit: &management.AuditGuard{
			Log:      log,
			Observer: auditGuardObserver{metrics},
			// The address resolved by the trusted-proxy logic, not re-derived
			// per handler. Re-deriving it is how a spoofable header ends up in
			// an audit record (P1-13, BL-05).
			ClientIP: clientIP.Of,
		},
	}

	// RP-initiated logout (P1-10). Shares the login package because the
	// confirmation interstitial is the same kind of browser page.
	logoutHandler := &login.LogoutHandler{
		Issuer:    cfg.Issuer,
		Clients:   clientLookup{store: clients, db: db},
		Sessions:  sessions,
		Refresh:   token.NewRefreshStore(),
		Verifier:  signing.NewVerifier(keys),
		Brandings: login.NewBrandingStore(),
		DB:        db,
		Audit:     auditor,
		Observer:  logoutObserver{metrics},
		Log:       log,
		Policy:    session.DefaultPolicy,
	}

	// The hosted login page (P1-12). It closes the loop: /oauth/authorize
	// sends a browser here when there is no session, and Resume sends it back
	// with a code once there is one.
	loginHandler := &login.Handler{
		Authorization: authorizeHandler,
		Sessions:      sessions,
		Users:         authn.NewUserStore(),
		Policies:      authn.NewPolicyStore(log),
		Brandings:     login.NewBrandingStore(),
		DB:            db,
		Audit:         auditor,
		Observer:      loginObserver{metrics},
		Log:           log,
		Policy:        session.DefaultPolicy,
		Limiter:       limiter,
		IP:            clientIP,

		// P1-19's two hosted pages. The token lookup is passed as a function
		// because resolving a link before a tenant is known needs the
		// SECURITY DEFINER path, and the login package deliberately holds only
		// the narrow Tenant interface.
		Password: &login.PasswordFlow{
			Users:     userStore,
			Policy:    passwordPolicy{store: authn.NewPolicyStore(log)},
			BaseURL:   cfg.Issuer,
			MailLimit: mailQuotas,
			Lookup: func(ctx context.Context, tok string, now time.Time) (user.Claim, error) {
				return userStore.LookupToken(ctx, db, tok, now)
			},
		},
	}
	if mailer != nil {
		loginHandler.Password.Mailer = mailer
	}

	discoveryCapabilities := oidc.Capabilities{
		Issuer:  cfg.Issuer,
		JWKSURI: cfg.Issuer + "/.well-known/jwks.json",

		// Only what this build actually serves.
		//
		// The authorization endpoint became true with P1-06, so it is
		// advertised — and with it PKCE's S256, which the discovery handler
		// only emits once there is an authorization endpoint to apply it to.
		//
		// The token endpoint arrives with P1-07 and is deliberately still
		// absent. A conforming client will fail to configure against a
		// document with one and not the other, which is the correct outcome:
		// it fails at configuration rather than halfway through a login.
		// P1-07 completes the pair. With both endpoints a conforming client
		// can finally configure itself and complete a login from the discovery
		// URL alone — which is P1-04's first Definition-of-Done item, and the
		// first moment it can honestly be ticked.
		//
		// P1-08 adds userinfo. Advertised in the same commit that serves it,
		// which is the only way this document stays true — and the reason the
		// capability struct treats an empty string as "not implemented"
		// rather than publishing a URL that 404s.
		AuthorizationEndpoint: cfg.Issuer + "/oauth/authorize",
		TokenEndpoint:         cfg.Issuer + "/oauth/token",
		UserInfoEndpoint:      cfg.Issuer + "/oauth/userinfo",
		//
		// P1-09. RFC 8414 names both; a resource server that reads this
		// document is exactly the caller introspection exists for, so
		// advertising them is what makes the endpoint discoverable rather
		// than something an integrator has to be told about.
		RevocationEndpoint: cfg.Issuer + "/oauth/revoke",
		// P1-10. RFC 8414 calls it end_session_endpoint; OpenID Connect
		// RP-Initiated Logout 1.0 is the specification it points at. Back-
		// channel logout is a different specification and is NOT advertised,
		// because it is not implemented — see PG-20.
		EndSessionEndpoint:    cfg.Issuer + "/oidc/logout",
		IntrospectionEndpoint: cfg.Issuer + "/oauth/introspect",
		ResponseTypes:         []string{"code"},
		GrantTypes: []string{
			token.GrantAuthorizationCode,
			token.GrantRefreshToken,
			token.GrantClientCredentials,
		},
		Scopes:            authorize.SupportedScopes(),
		SigningAlgorithms: []string{string(signing.RS256), string(signing.ES256)},
	}

	discovery, err := oidc.NewHandler(discoveryCapabilities, keys)
	if err != nil {
		return fmt.Errorf("discovery document: %w", err)
	}

	if _, err := keys.Get(); err != nil {
		log.Warn("no signing keys are available; tokens cannot be issued",
			"error", err.Error(),
			"remedy", "run: keyctl generate && keyctl rotate")
	} else if kid, kerr := signing.NewSigner(keys).CurrentKID(); kerr == nil {
		log.Info("signing key loaded", "kid", kid)
	} else {
		log.Warn("keys are present but none is signing",
			"error", kerr.Error(),
			"remedy", "run: keyctl rotate")
	}

	srv := httpserver.New(cfg.HTTP, httpserver.Deps{
		Logger:         log,
		Health:         health,
		Metrics:        metrics,
		Origins:        originChecker{store: clients, db: db, log: log},
		Discovery:      discovery,
		Authorize:      authorizeHandler,
		Token:          tokenHandler,
		Introspect:     http.HandlerFunc(lifecycleHandler.Introspect),
		Revoke:         http.HandlerFunc(lifecycleHandler.Revoke),
		UserInfo:       userInfoHandler,
		Logout:         logoutHandler,
		Login:          loginHandler,
		Forgot:         http.HandlerFunc(loginHandler.Forgot),
		SetPassword:    http.HandlerFunc(loginHandler.SetPassword),
		V1:             v1,
		Organizations:  organizations,
		ProjectAPI:     projects,
		ApplicationAPI: applications,
		RoleAPI:        roles,
		GrantAPI:       grants,
		UserAPI:        users,
		AuditAPI:       &auditlog.Handler{DB: db, Log: log},
		// Explicit configuration, not inferred from the environment: see the
		// comment on config.HTTPConfig.TrustProxyHeaders. Defaults to false,
		// so a deployment behind a proxy that forwards client headers
		// untouched is safe by default rather than by accident.
		TrustProxyHeaders: cfg.HTTP.TrustProxyHeaders,
	})

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// probeReadiness requests the local readiness endpoint and reports the result
// as a process exit code, for the container healthcheck.
//
// It talks to 127.0.0.1 rather than to the configured address, because the
// probe runs inside the same container as the server it is checking.
func probeReadiness() int {
	addr := os.Getenv("AUTH_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8080"
	}

	// Parse the port to an integer and rebuild the URL from that integer rather
	// than from the environment string.
	//
	// AUTH_HTTP_ADDR is configuration, not user input, so this was never a
	// realistic SSRF vector: anyone who can set it already controls the
	// process. But an environment-derived string concatenated into a request
	// URL is the shape of the bug regardless of today's reachability, and
	// rebuilding from a validated integer means no attacker-influenceable
	// string reaches the URL at all — which is a genuine fix rather than a
	// suppressed warning (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §7).
	portNum, convErr := strconv.Atoi(port)
	if convErr != nil || portNum < 1 || portNum > 65535 {
		fmt.Fprintf(os.Stderr, "healthcheck: AUTH_HTTP_ADDR has an invalid port %q\n", port)
		return 1
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/readyz", portNum)

	client := &http.Client{Timeout: 3 * time.Second}

	// #nosec G704 -- gosec's taint analysis follows portNum back to os.Getenv
	// and cannot see the strconv.Atoi + range check in between. After that
	// validation the URL contains no attacker-influenceable string: the host is
	// the literal 127.0.0.1 and the only variable is an int in [1,65535]
	// formatted with %d. This is a limitation of the analysis, not a reachable
	// SSRF, and it is annotated rather than worked around because contorting
	// the code to satisfy a taint tracker would make it worse to read.
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: readiness returned %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// auditObserver adapts the audit package's Observer to the metric instruments.
// It exists so audit does not import observability, keeping the dependency
// one-directional.
type auditObserver struct{ m *observability.Metrics }

func (o auditObserver) PartitionRunway(months int) {
	o.m.AuditPartitionRunway.Set(float64(months))
}

func (o auditObserver) PartitionMaintenanceFailed() {
	o.m.AuditPartitionErrors.Inc()
}

// exitCode maps an error to a process exit code. Kept for the operational
// distinctions later phases need (a configuration error is not retryable; a
// dependency failure is).
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, config.ErrNotConfigured):
		return 78 // EX_CONFIG, per sysexits.h
	default:
		return 1
	}
}

// serviceSecrets holds every secret value the process needs, resolved once at
// startup.
//
// Values, not references: a reference resolved lazily at the point of use turns
// a configuration error into a runtime failure at an arbitrary later moment,
// which is precisely the failure mode P0-14 exists to remove.
type serviceSecrets struct {
	adminToken string
}

// resolveSecrets reads every configured secret reference.
//
// Permissions are checked strictly outside local development, so a secret file
// that is group- or world-readable is refused rather than used. On the VM
// deployment that means the file must be mode 0400 and owned by the container's
// own uid, since the service is not the operator (deploy/SECRETS.md).
func resolveSecrets(cfg *config.Config) (serviceSecrets, error) {
	var out serviceSecrets

	resolver := config.NewSecretResolver(cfg.Environment != config.EnvLocal)

	if cfg.Admin.TokenRef != "" {
		raw, err := resolver.Resolve(config.SecretRef(cfg.Admin.TokenRef))
		if err != nil {
			return out, fmt.Errorf("resolve admin token: %w", err)
		}
		out.adminToken = string(raw)
	}

	return out, nil
}

// passwordChecks bundles the P1-02 pieces a password-set path needs.
//
// Built here because the breach client is a network dependency and those are
// assembled in main, and returned as one value so the wiring is a single thing
// to pass to P1-12 and P1-19 rather than two that can drift apart.
type passwordChecks struct {
	policies *authn.PolicyStore
	breaches authn.BreachChecker

	// state is what the startup log reports: "enabled" or "disabled".
	state string
}

func newPasswordChecks(cfg *config.Config, log *slog.Logger) passwordChecks {
	checks := passwordChecks{
		policies: authn.NewPolicyStore(log),
		state:    "disabled",
	}

	if !cfg.Password.BreachCheckEnabled {
		log.Warn("breached-password checking is disabled",
			"consequence", "passwords will be accepted without any corpus check",
			"remedy", "remove AUTH_PASSWORD_BREACH_CHECK_ENABLED=false")
		return checks
	}

	client := authn.NewBreachClient()
	if cfg.Password.BreachAPI != "" {
		client.Endpoint = cfg.Password.BreachAPI
	}
	client.HTTP.Timeout = cfg.Password.BreachTimeout

	checks.breaches = client
	checks.state = "enabled"
	return checks
}

// --- session plumbing --------------------------------------------------------

// sessionSweepInterval is how often expired sessions are removed.
//
// Hourly. Expiry is already enforced on every lookup, so this is housekeeping
// rather than a control — its job is to stop the table growing without bound,
// and an hour is far more often than that requires.
const sessionSweepInterval = time.Hour

// sessionRetention is how long an expired or revoked session is kept before
// deletion.
//
// Seven days, because the row is what the sessions screen shows to explain why
// somebody was signed out, and what an incident investigation reads. Deleting
// it the moment it expires would remove the evidence at the moment it becomes
// interesting.
const sessionRetention = 7 * 24 * time.Hour

// sessionSweepBatch bounds one run.
const sessionSweepBatch = 1000

func runSessionSweep(ctx context.Context, sessions *session.Manager, log *slog.Logger) {
	ticker := time.NewTicker(sessionSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := sessions.Sweep(ctx, sessionRetention, sessionSweepBatch, time.Now())
			if err != nil {
				log.Warn("sweeping expired sessions failed", "error", err.Error())
				continue
			}
			if removed > 0 {
				log.Info("swept expired sessions", "removed", removed)
			}
		}
	}
}

// sessionObserver reports session cache behaviour to the metrics registry.
//
// A thin adapter rather than importing the metrics package into
// internal/session: the session package stays usable without it, and the one
// counter that matters — an invalidation that did not reach the cache — is
// wired where the alert can see it.
type sessionObserver struct{ m *observability.Metrics }

func (o sessionObserver) InvalidationFailed() {
	o.m.SessionCacheInvalidationFailures.Inc()
}

func (o sessionObserver) Lookup(source string, d time.Duration) {
	o.m.SessionLookupDuration.WithLabelValues(source).Observe(d.Seconds())
}

// redisChecker reports Redis health for the readiness probe.
//
// Reported, not required. Redis is a cache in front of PostgreSQL, so losing
// it makes the service slower rather than wrong — but a readiness probe that
// stayed green through it would hide the latency cliff until somebody noticed
// the silent-SSO budget being missed.
type redisChecker struct{ client *redis.Client }

func (redisChecker) Name() string { return "redis" }

func (c redisChecker) Check(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// --- authorize plumbing -------------------------------------------------------

// clientLookup adapts the client store to what the authorize handler needs.
//
// The handler wants one method and the store takes a *postgres.DB it does not
// otherwise need to know about, so the seam is here rather than in either
// package.
type clientLookup struct {
	store *client.Store
	db    *postgres.DB
}

func (c clientLookup) ByClientID(ctx context.Context, clientID string) (client.Application, error) {
	return c.store.ByClientID(ctx, c.db, clientID)
}

// CredentialsFor takes the already-resolved application rather than a bare
// client_id: by then the tenant is known, so the secret hash is read on the
// normal tenant-scoped path instead of needing a second bootstrap function.
func (c clientLookup) CredentialsFor(
	ctx context.Context, app client.Application,
) (client.Credentials, error) {
	return c.store.CredentialsFor(ctx, c.db, app)
}

// originChecker answers the CORS questions from the client store (P1-29).
//
// Two lookups because there are two moments. A preflight has no credential, so
// the most that can be asked is whether ANY application registers the origin;
// the actual request names an application, so it is asked about that one. See
// internal/httpserver/cors.go for why the second is the check that matters and
// the first is not a hole.
//
// Every failure answers "not allowed". A database that cannot be reached must
// not become a service that permits every origin — and the request itself is
// unaffected either way, since this decides only whether a browser hands the
// body to the page.
type originChecker struct {
	store *client.Store
	db    *postgres.DB
	log   *slog.Logger
}

func (o originChecker) AnyApplicationAllows(r *http.Request, origin string) bool {
	allowed, err := o.store.OriginIsRegistered(r.Context(), o.db, origin)
	if err != nil {
		o.log.Warn("could not check a preflight origin", "error", err.Error())
		return false
	}
	return allowed
}

func (o originChecker) ApplicationAllows(r *http.Request, clientID, origin string) bool {
	app, err := o.store.ByClientID(r.Context(), o.db, clientID)
	if err != nil {
		// Includes the ordinary case of a client_id that does not exist,
		// which is why this is not a warning.
		return false
	}
	return app.MatchesOrigin(origin)
}

// redactedSMTPHost keeps the host and drops everything else.
//
// An SMTP URL can carry `user:password@`, and this line goes to the log on
// every start. The host is the part an operator needs to recognise; the
// credential is the part that must never be written down.
func redactedSMTPHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "(unparseable)"
	}
	return parsed.Scheme + "://" + parsed.Host
}

// authorizeObserver reports which path an authorization request took.
//
// Separate labels for silent and interactive because docs/PLAN/12 sets a latency
// target for the silent path specifically, and an average across both would
// hide it behind the time a human spends typing a password.
type authorizeObserver struct{ m *observability.Metrics }

func (o authorizeObserver) Authorized(path string, d time.Duration) {
	o.m.AuthorizeTotal.WithLabelValues("granted", path).Inc()
	if path == "silent" {
		o.m.AuthorizeDuration.WithLabelValues(path).Observe(d.Seconds())
	}
}

func (o authorizeObserver) Denied(errorCode string) {
	o.m.AuthorizeTotal.WithLabelValues("denied", errorCode).Inc()
}

// --- token plumbing -------------------------------------------------------------

// sessionLiveness answers whether the session behind a refresh token is still
// usable.
//
// A refresh token outlives the browser session it came from, but not a revoked
// one. P3-09 makes that systematic; Phase 1 does the check here.
type sessionLiveness struct {
	sessions *session.Manager
	policy   session.Policy
}

func (s sessionLiveness) IsLive(ctx context.Context, sessionID string, now time.Time) bool {
	// Looked up by id rather than by token, because a refresh token carries
	// the session's identifier and never its cookie — which is PG-14's
	// separation paying off in a second place.
	return s.sessions.IsLive(ctx, sessionID, now)
}

// refreshLookup binds the database to the refresh store, so the lifecycle
// handler states what it needs (a lookup) rather than how it is done.
type refreshLookup struct {
	store *token.RefreshStore
	db    *postgres.DB
}

func (r refreshLookup) Lookup(ctx context.Context, presented string, now time.Time) (token.Refresh, error) {
	return r.store.Lookup(ctx, r.db, presented, now)
}

func (r refreshLookup) RevokeFamily(ctx context.Context, tx *postgres.Tx, familyID string) (int64, error) {
	return r.store.RevokeFamily(ctx, tx, familyID)
}

func (r refreshLookup) RevokeForSessionAndClient(
	ctx context.Context, tx *postgres.Tx, sessionID, clientID string,
) (int64, error) {
	return r.store.RevokeForSessionAndClient(ctx, tx, sessionID, clientID)
}

// lifecycleObserver counts introspection and revocation outcomes.
//
// Labelled by endpoint and a coarse outcome, never by WHY a token was
// inactive — that would move the disclosure the response body refuses to make
// into /metrics.
type lifecycleObserver struct{ m *observability.Metrics }

func (o lifecycleObserver) Lifecycle(endpoint, outcome string) {
	if o.m != nil {
		o.m.TokenLifecycle.WithLabelValues(endpoint, outcome).Inc()
	}
}

// rateLimitObserver reports what the limiter did.
type rateLimitObserver struct{ m *observability.Metrics }

func (o rateLimitObserver) Refused(bound string) {
	if o.m != nil {
		o.m.RateLimitRefusals.WithLabelValues(bound).Inc()
	}
}

func (o rateLimitObserver) Unavailable() {
	if o.m != nil {
		o.m.RateLimitUnavailable.Inc()
	}
}

// auditGuardObserver reports mutating requests that recorded nothing.
//
// The counter should be permanently zero, so the alert is "greater than zero"
// rather than a rate or a threshold.
type auditGuardObserver struct{ m *observability.Metrics }

func (o auditGuardObserver) MutationNotAudited(route string) {
	if o.m != nil {
		o.m.UnauditedMutations.WithLabelValues(route).Inc()
	}
}

// logoutObserver counts logout outcomes.
//
// "asked" is as interesting as "completed": a sudden rise means relying
// parties have stopped sending a usable id_token_hint, which turns a one-click
// sign-out into a confirmation page for every user.
// mailObserver counts messages that could not be delivered.
//
// ADR-018 makes this the metric that keeps a best-effort send safe: an
// invitation nobody received and an invitation nobody sent look identical from
// outside the service, and without a counter the difference is invisible until
// somebody complains.
type mailObserver struct{ m *observability.Metrics }

func (o mailObserver) MailSendFailed(reason string) {
	if o.m != nil && o.m.MailSendFailures != nil {
		o.m.MailSendFailures.WithLabelValues(reason).Inc()
	}
}

// passwordPolicy applies P1-02's rules to a password chosen through a link.
//
// The same evaluator the login page uses, reading the same per-organization
// settings — a set-password page with rules of its own would be a second
// password policy, and the one nobody remembers to update.
type passwordPolicy struct{ store *authn.PolicyStore }

func (p passwordPolicy) Validate(
	ctx context.Context, tx *postgres.Tx, orgID, _ string, password string,
) error {
	policy, err := p.store.Policy(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if violations := authn.Evaluate(password, policy); len(violations) > 0 {
		// Every rule that failed, not the first: somebody fixing three
		// problems should learn about three (P1-16's reasoning for settings,
		// and it is the same person's afternoon either way).
		reasons := make([]string, 0, len(violations))
		for _, v := range violations {
			reasons = append(reasons, v.Message)
		}
		return errors.New(strings.Join(reasons, " "))
	}
	return nil
}

type logoutObserver struct{ m *observability.Metrics }

func (o logoutObserver) Logout(outcome string) {
	if o.m != nil {
		o.m.LogoutTotal.WithLabelValues(outcome).Inc()
	}
}

// userInfoObserver counts userinfo outcomes.
//
// One label, and coarse: every unusable token is "invalid_token". Separating
// expired from revoked would put SECURITY/02 §12's disclosure into /metrics.
type userInfoObserver struct{ m *observability.Metrics }

func (o userInfoObserver) UserInfo(outcome string, d time.Duration) {
	if o.m != nil {
		o.m.UserInfoTotal.WithLabelValues(outcome).Inc()
		o.m.UserInfoDuration.WithLabelValues(outcome).Observe(d.Seconds())
	}
}

// loginObserver counts login attempts.
//
// The outcome label is coarse on purpose — "failed" covers a wrong password,
// an unknown address and a locked account alike. Splitting them would move
// docs/SECURITY/02 §12's enumeration disclosure from the response body into
// /metrics, which is scraped, retained and usually more widely readable than
// the audit log.
type loginObserver struct{ m *observability.Metrics }

func (o loginObserver) LoginAttempt(outcome string) {
	if o.m != nil {
		o.m.LoginAttempts.WithLabelValues(outcome).Inc()
	}
}

// tokenObserver reports token-endpoint outcomes.
//
// Bucketed and labelled by grant, because docs/PLAN/12's targets are for this
// endpoint as a whole but a client_credentials call and an authorization_code
// call do very different amounts of work — averaging them would hide a
// regression in either.
type tokenObserver struct{ m *observability.Metrics }

func (o tokenObserver) Issued(grant string, d time.Duration) {
	o.m.TokensIssued.WithLabelValues(grant).Inc()
	o.m.TokenDuration.WithLabelValues(grant).Observe(d.Seconds())
}

func (o tokenObserver) Denied(grant, errorCode string) {
	o.m.TokenErrors.WithLabelValues(grant, errorCode).Inc()
}

// Demo application A: a confidential web client (P1-26).
//
// A server-side application with a client secret. The browser never sees a
// token: the code is exchanged on the server and the result is kept in a
// server-side session behind an HttpOnly cookie. That is the profile most
// consumer teams actually have, and it is the one whose token handling is
// safest — which is why it is worth showing beside the SPA.
//
// **It calls the auth service on exactly two occasions**: redirecting a user
// to sign in, and exchanging the code afterwards. Every request after that is
// answered from a locally verified token and a cached key set — no round trip
// (`docs/PLAN/12`, `docs/PLAN/03` § Main Data Flow).
//
// Kept in the repository as living integration documentation: a consumer team
// should be able to copy this file and have something correct.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zed378/zed-auth/demo/internal/verify"
)

type config struct {
	issuer       string
	clientID     string
	clientSecret string
	baseURL      string
	addr         string
}

func main() {
	// Before any configuration is read: a probe must work in a container whose
	// environment is deliberately minimal, and it must not need the client
	// secret in order to ask whether the process is listening.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		healthcheck(env("DEMO_ADDR", ":8090"))
		return
	}

	cfg := config{
		issuer:       required("DEMO_ISSUER"),
		clientID:     required("DEMO_CLIENT_ID"),
		clientSecret: required("DEMO_CLIENT_SECRET"),
		baseURL:      required("DEMO_BASE_URL"),
		addr:         env("DEMO_ADDR", ":8090"),
	}

	app := &webapp{
		cfg:      cfg,
		verifier: verify.New(cfg.issuer, cfg.clientID),
		sessions: map[string]session{},
		pending:  map[string]pending{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.home)
	mux.HandleFunc("/login", app.login)
	mux.HandleFunc("/callback", app.callback)
	mux.HandleFunc("/logout", app.logout)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("demo web application listening on %s as %s", cfg.addr, cfg.clientID)
	server := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

type session struct {
	subject string
	orgID   string
	expires time.Time
}

type pending struct {
	verifier string
	nonce    string
	created  time.Time
}

type webapp struct {
	cfg      config
	verifier *verify.Verifier

	mu       sync.Mutex
	sessions map[string]session
	pending  map[string]pending
}

const (
	cookieName      = "demo_webapp_session"
	sessionLifetime = time.Hour
)

func (a *webapp) home(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(r)
	if !ok {
		render(w, homeTemplate, map[string]any{"SignedIn": false, "App": "Demo Web Application"})
		return
	}

	render(w, homeTemplate, map[string]any{
		"SignedIn": true,
		"App":      "Demo Web Application",
		"Subject":  current.subject,
		"OrgID":    current.orgID,
		"Expires":  current.expires.UTC().Format(time.RFC3339),
	})
}

func (a *webapp) login(w http.ResponseWriter, r *http.Request) {
	verifierValue := randomString()
	state := randomString()
	nonce := randomString()

	a.mu.Lock()
	a.pending[state] = pending{verifier: verifierValue, nonce: nonce, created: time.Now()}
	a.mu.Unlock()

	challenge := sha256.Sum256([]byte(verifierValue))

	// PKCE even though this is a confidential client. RFC 9700 recommends it
	// for every client, and it costs nothing: it removes code interception as
	// a concern regardless of how well the secret is kept.
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {a.cfg.clientID},
		"redirect_uri":  {a.cfg.baseURL + "/callback"},
		"scope":         {"openid"},
		"state":         {state},
		// A nonce, checked against the ID token below. `state` protects the
		// callback; `nonce` protects the token — without it, an ID token
		// captured from an earlier sign-in can be replayed into a new one.
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, a.cfg.issuer+"/oauth/authorize?"+params.Encode(), http.StatusFound)
}

func (a *webapp) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	a.mu.Lock()
	waiting, ok := a.pending[state]
	delete(a.pending, state)
	a.mu.Unlock()

	// Compared to what THIS application issued, and consumed on use. A
	// callback carrying somebody else's code and a state we never issued is
	// the login-CSRF attack.
	if !ok {
		http.Error(w, "no sign-in is in progress", http.StatusBadRequest)
		return
	}
	if time.Since(waiting.created) > 10*time.Minute {
		http.Error(w, "that sign-in took too long; start again", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "no authorization code", http.StatusBadRequest)
		return
	}

	token, err := a.exchange(r.Context(), code, waiting.verifier)
	if err != nil {
		log.Printf("exchange failed: %v", err)
		http.Error(w, "sign-in failed", http.StatusBadGateway)
		return
	}

	claims, err := a.verifier.Verify(r.Context(), token)
	if err != nil {
		// The token came straight from the issuer over TLS, and it is verified
		// anyway. A consumer that trusts a token because of where it arrived
		// from has no defence the day something else can reach that path.
		log.Printf("the issuer returned a token we could not verify: %v", err)
		http.Error(w, "sign-in failed", http.StatusBadGateway)
		return
	}

	// The nonce closes the replay: this ID token has to be the one minted for
	// the sign-in that started here, not one captured from a previous session.
	if claims.Nonce != waiting.nonce {
		log.Printf("nonce mismatch on callback")
		http.Error(w, "sign-in failed", http.StatusBadGateway)
		return
	}

	id := randomString()
	a.mu.Lock()
	a.sessions[id] = session{
		subject: claims.Subject,
		orgID:   claims.OrgID,
		// The session outlives the ID token deliberately. An ID token is
		// consumed once to establish this session (it lives five minutes);
		// the session it establishes is this application's own, and tying its
		// length to the assertion's would sign everybody out after five
		// minutes for no security gain.
		expires: time.Now().Add(sessionLifetime),
	}
	a.mu.Unlock()

	// HttpOnly, so no script in this application's pages can read it — the
	// browser never holds a token here, only a reference to one.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.cfg.baseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *webapp) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})

	// Ends the SSO session too, not just this application's. A logout that
	// leaves the identity provider's session alive means the next click on
	// "sign in" signs straight back in, which looks like the logout failed.
	params := url.Values{
		"post_logout_redirect_uri": {a.cfg.baseURL + "/"},
		"client_id":                {a.cfg.clientID},
	}
	http.Redirect(w, r, a.cfg.issuer+"/oidc/logout?"+params.Encode(), http.StatusFound)
}

// current reads the session, if there is a live one.
//
// **No call to the auth service.** The claims were verified once at sign-in
// and the expiry is checked here; that is the whole request path.
func (a *webapp) current(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return session{}, false
	}

	a.mu.Lock()
	found, ok := a.sessions[cookie.Value]
	a.mu.Unlock()

	if !ok || time.Now().After(found.expires) {
		return session{}, false
	}
	return found, true
}

// exchange trades the code for tokens and returns the ID TOKEN.
//
// Not the access token. The access token's `aud` is the auth service itself —
// it is a capability at that API, the same for every application — so it
// cannot tell this application's users from another's. The ID token is the
// assertion addressed to THIS client, and its `aud` is this client_id, which
// is what makes "reject a token meant for the other application" mean
// something.
func (a *webapp) exchange(ctx context.Context, code, verifierValue string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.cfg.baseURL + "/callback"},
		"code_verifier": {verifierValue},
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, a.cfg.issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Client authentication in the Authorization header rather than the body.
	// docs/PLAN/05 Part A prefers it, and a secret in a form body is a secret
	// in more logs.
	request.SetBasicAuth(url.QueryEscape(a.cfg.clientID), url.QueryEscape(a.cfg.clientSecret))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the token endpoint answered %d", response.StatusCode)
	}

	var body struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.IDToken == "" {
		return "", fmt.Errorf("no id_token in the response")
	}
	return body.IDToken, nil
}

func randomString() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("demo: no entropy available: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func render(w http.ResponseWriter, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("rendering: %v", err)
	}
}

var homeTemplate = template.Must(template.New("home").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.App}}</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto">
<h1>{{.App}}</h1>
{{if .SignedIn}}
<p>You are signed in.</p>
<dl>
<dt>Subject</dt><dd><code id="subject">{{.Subject}}</code></dd>
<dt>Organization</dt><dd><code id="org">{{.OrgID}}</code></dd>
<dt>This session expires</dt><dd><code>{{.Expires}}</code></dd>
</dl>
<p>This page was rendered without contacting the auth service. The ID token was
verified locally against its published key set, once, at sign-in.</p>
<p><a href="/logout">Sign out</a></p>
{{else}}
<p>You are not signed in.</p>
<p><a href="/login" id="signin">Sign in</a></p>
{{end}}
</body>
</html>
`))

// healthcheck lets the container probe itself.
//
// The runtime image is distroless: no shell, no curl, no wget. The service's
// own image solved this the same way (ADR-008) — the binary is the probe. A
// compose healthcheck that has to be `disable: true` because the image is
// minimal is a container whose failures are invisible until someone looks.
func healthcheck(addr string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatalf("cannot probe %q: %v", addr, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		log.Fatalf("unhealthy: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		log.Fatalf("unhealthy: /healthz answered %d", response.StatusCode)
	}
}

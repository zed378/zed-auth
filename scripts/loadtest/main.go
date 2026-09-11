// Command loadgen is the first load test against docs/PLAN/12's latency
// targets (P1-28, docs/PLAN/17 § Phase 1).
//
// It runs ON the VM against 127.0.0.1:10800 — the service's own port. Driving
// it through the Cloudflare tunnel from a laptop would measure Cloudflare, the
// internet, and a residential uplink, and would report all three as the
// service's latency. The Host header and X-Forwarded-Proto are set to what the
// tunnel sends, so the service builds the same URLs it would in production.
//
// Standard library only, for the same reason `demo/` is: a load result is
// evidence, and evidence that needs a dependency tree to reproduce is weaker.
//
// Phases, in the order docs/PLAN/12's table lists them:
//
//	authorize-silent  the SSO path a user feels — session exists, no prompt
//	token-refresh     the highest-volume endpoint, driven by rotating refresh
//	userinfo          what a consumer SPA calls on every page load
//	management        deliberately paced under the 600/min per-client quota
//	mixed             all four at once, because production never sees one
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// ---------------------------------------------------------------------------
// A client that talks to the loopback port while looking like the tunnel.
// ---------------------------------------------------------------------------

type client struct {
	base   string
	host   string
	http   *http.Client
	mu     sync.Mutex
	cookie map[string]string
}

func newClient(base, host string) *client {
	return &client{
		base: base,
		host: host,
		http: &http.Client{
			// Never follow. Every redirect in this flow carries something the
			// test needs to read — a code, an error, a login request id — and
			// a followed redirect is a measurement of two requests reported as
			// one.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				MaxIdleConns:        512,
				MaxIdleConnsPerHost: 512,
				MaxConnsPerHost:     0,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 20 * time.Second,
		},
		cookie: map[string]string{},
	}
}

// cookies returns the jar as a Cookie header. A real jar would need the URLs
// to be https for Secure cookies to be stored, and they cannot be: TLS is
// terminated by the tunnel, above the port under test.
func (c *client) cookies() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := make([]string, 0, len(c.cookie))
	for k, v := range c.cookie {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func (c *client) absorb(resp *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sc := range resp.Header.Values("Set-Cookie") {
		head := strings.SplitN(sc, ";", 2)[0]
		if k, v, ok := strings.Cut(head, "="); ok {
			if v == "" || strings.Contains(strings.ToLower(sc), "max-age=0") {
				delete(c.cookie, k)
				continue
			}
			c.cookie[k] = v
		}
	}
}

type result struct {
	status   int
	body     string
	location string
	took     time.Duration
	err      error
}

func (c *client) do(method, path string, form url.Values, bearer string, sendCookies bool) result {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return result{err: err}
	}
	req.Host = c.host
	req.Header.Set("X-Forwarded-Proto", "https")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if sendCookies {
		if ck := c.cookies(); ck != "" {
			req.Header.Set("Cookie", ck)
		}
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return result{took: time.Since(start), err: err}
	}
	raw, _ := io.ReadAll(resp.Body)
	took := time.Since(start) // after the body, which is part of the response
	resp.Body.Close()
	if sendCookies {
		c.absorb(resp)
	}
	return result{
		status:   resp.StatusCode,
		body:     string(raw),
		location: resp.Header.Get("Location"),
		took:     took,
	}
}

// ---------------------------------------------------------------------------
// PKCE and the sign-in dance
// ---------------------------------------------------------------------------

func pkce() (verifier, challenge string) {
	b := make([]byte, 32)
	crand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

var (
	reCSRF    = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)
	reRequest = regexp.MustCompile(`name="request" value="([^"]*)"`)
)

func codeFrom(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return u.Query().Get("code")
}

// authorizePath builds the authorize request. `silent` adds prompt=none, which
// is the path docs/PLAN/12 names "silent SSO" and gives it its own target.
func (e *env) authorizePath(challenge string, silent bool) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {e.clientID},
		"redirect_uri":          {e.redirect},
		"scope":                 {"openid offline_access"},
		"state":                 {fmt.Sprint(rand.Int63())},
		"nonce":                 {fmt.Sprint(rand.Int63())},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if silent {
		q.Set("prompt", "none")
	}
	return "/oauth/authorize?" + q.Encode()
}

type tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	ID      string `json:"id_token"`
	Error   string `json:"error"`
	Desc    string `json:"error_description"`
}

func (e *env) exchange(c *client, code, verifier string) (tokens, error) {
	r := c.do("POST", "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {e.clientID},
		"redirect_uri":  {e.redirect},
		"code_verifier": {verifier},
	}, "", false)
	if r.err != nil {
		return tokens{}, r.err
	}
	var t tokens
	json.Unmarshal([]byte(r.body), &t)
	if t.Access == "" {
		return t, fmt.Errorf("exchange failed (%d): %s %s", r.status, t.Error, t.Desc)
	}
	return t, nil
}

// signIn posts the hosted login form once, leaving the session cookie in the
// jar. Everything afterwards is silent.
func (e *env) signIn(c *client) error {
	verifier, challenge := pkce()
	r := c.do("GET", e.authorizePath(challenge, false), nil, "", true)
	if r.status != http.StatusFound || !strings.Contains(r.location, "/login?request=") {
		return fmt.Errorf("authorize did not offer a login page: %d %s", r.status, r.location)
	}
	loginPath := strings.TrimPrefix(r.location, "https://"+c.host)

	page := c.do("GET", loginPath, nil, "", true)
	if page.status != http.StatusOK {
		return fmt.Errorf("login page: %d", page.status)
	}
	csrf := reCSRF.FindStringSubmatch(page.body)
	reqID := reRequest.FindStringSubmatch(page.body)
	if csrf == nil || reqID == nil {
		return fmt.Errorf("login page has no csrf_token or request field")
	}

	post := c.do("POST", "/login", url.Values{
		"csrf_token": {csrf[1]},
		"request":    {reqID[1]},
		"email":      {e.email},
		"password":   {e.password},
	}, "", true)
	code := codeFrom(post.location)
	if code == "" {
		return fmt.Errorf("sign-in did not produce a code: %d %s", post.status, post.location)
	}
	if _, err := e.exchange(c, code, verifier); err != nil {
		return err
	}
	return nil
}

// silentToken gets one more refresh token over the existing session, so each
// worker in the token phase owns its own rotating chain rather than racing the
// others for one.
func (e *env) silentToken(c *client) (tokens, error) {
	verifier, challenge := pkce()
	r := c.do("GET", e.authorizePath(challenge, true), nil, "", true)
	code := codeFrom(r.location)
	if code == "" {
		return tokens{}, fmt.Errorf("silent authorize gave no code: %d %s %s", r.status, r.location, trim(r.body))
	}
	return e.exchange(c, code, verifier)
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Measurement
// ---------------------------------------------------------------------------

type phase struct {
	name   string
	target [3]time.Duration // p50, p95, p99 from docs/PLAN/12
	mu     sync.Mutex
	took   []time.Duration
	ok     int64
	bad    int64
	firstF string
}

func (p *phase) record(d time.Duration, good bool, detail string) {
	p.mu.Lock()
	p.took = append(p.took, d)
	if good {
		p.ok++
	} else {
		p.bad++
		if p.firstF == "" {
			p.firstF = detail
		}
	}
	p.mu.Unlock()
}

func (p *phase) pct(q float64) time.Duration {
	if len(p.took) == 0 {
		return 0
	}
	i := int(q * float64(len(p.took)-1))
	return p.took[i]
}

func (p *phase) sortTimes() { sort.Slice(p.took, func(i, j int) bool { return p.took[i] < p.took[j] }) }

// ---------------------------------------------------------------------------

type env struct {
	base     string
	host     string
	clientID string
	redirect string
	email    string
	password string
	orgID    string
	workers  int
	dur      time.Duration
}

func main() {
	e := &env{
		base:     getenv("LOAD_BASE", "http://127.0.0.1:10800"),
		host:     getenv("LOAD_HOST", "auth.zedth.my.id"),
		clientID: os.Getenv("LOAD_CLIENT_ID"),
		redirect: getenv("LOAD_REDIRECT", "http://localhost:9998/callback"),
		email:    os.Getenv("LOAD_EMAIL"),
		password: os.Getenv("LOAD_PASSWORD"),
		orgID:    os.Getenv("LOAD_ORG"),
		workers:  atoi(getenv("LOAD_WORKERS", "20")),
		dur:      time.Duration(atoi(getenv("LOAD_SECONDS", "30"))) * time.Second,
	}
	// A load test that has only ever printed "within targets" has not been
	// shown to be capable of printing anything else. LOAD_STRICT divides every
	// target by N, so the same run against the same service must report OVER
	// and exit non-zero — the harness's own mutation test.
	strict := atoi(getenv("LOAD_STRICT", "1"))
	if strict < 1 {
		strict = 1
	}

	if e.clientID == "" || e.email == "" || e.orgID == "" {
		fmt.Fprintln(os.Stderr, "LOAD_CLIENT_ID, LOAD_EMAIL and LOAD_ORG are required")
		os.Exit(2)
	}

	fmt.Printf("target      %s (Host: %s)\n", e.base, e.host)
	fmt.Printf("shape       %d concurrent workers, %s per phase\n\n", e.workers, e.dur)

	// --- bootstrap ---------------------------------------------------------
	session := newClient(e.base, e.host)
	if err := e.signIn(session); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap sign-in:", err)
		os.Exit(1)
	}
	bearer, err := e.silentToken(session)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap token:", err)
		os.Exit(1)
	}
	chains := make([]tokens, e.workers)
	for i := range chains {
		t, err := e.silentToken(session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap chain %d: %v\n", i, err)
			os.Exit(1)
		}
		if t.Refresh == "" {
			fmt.Fprintln(os.Stderr, "bootstrap: no refresh token issued — is offline_access granted?")
			os.Exit(1)
		}
		chains[i] = t
	}
	fmt.Printf("bootstrap   1 session, %d refresh chains, 1 access token\n\n", len(chains))

	// --- the four workloads ------------------------------------------------
	target := func(p50, p95, p99 time.Duration) [3]time.Duration {
		return [3]time.Duration{p50 / time.Duration(strict), p95 / time.Duration(strict), p99 / time.Duration(strict)}
	}
	authorize := &phase{name: "/oauth/authorize (silent)", target: target(50*time.Millisecond, 150*time.Millisecond, 300*time.Millisecond)}
	token := &phase{name: "/oauth/token (refresh)", target: target(50*time.Millisecond, 200*time.Millisecond, 400*time.Millisecond)}
	userinfo := &phase{name: "/oauth/userinfo", target: target(30*time.Millisecond, 100*time.Millisecond, 200*time.Millisecond)}
	mgmt := &phase{name: "/v1 management (CRUD read)", target: target(100*time.Millisecond, 300*time.Millisecond, 600*time.Millisecond)}
	if strict > 1 {
		fmt.Printf("strict      targets divided by %d (harness self-test)\n\n", strict)
	}

	authorizeWork := func(c *client, p *phase) {
		_, challenge := pkce()
		r := c.do("GET", e.authorizePath(challenge, true), nil, "", true)
		good := r.err == nil && codeFrom(r.location) != ""
		p.record(r.took, good, fmt.Sprintf("%d %s %s", r.status, r.location, trim(r.body)))
	}

	// Each worker rotates its OWN chain. Refresh tokens are single-use
	// (P1-16 reuse detection), so two workers sharing one would spend the
	// test tripping the reuse alarm and measuring the error path.
	tokenWork := func(idx int) func(*client, *phase) {
		return func(c *client, p *phase) {
			r := c.do("POST", "/oauth/token", url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {chains[idx].Refresh},
				"client_id":     {e.clientID},
			}, "", false)
			var t tokens
			json.Unmarshal([]byte(r.body), &t)
			good := r.err == nil && t.Access != ""
			if t.Refresh != "" {
				chains[idx].Refresh = t.Refresh
			}
			p.record(r.took, good, fmt.Sprintf("%d %s %s", r.status, t.Error, t.Desc))
		}
	}

	userinfoWork := func(c *client, p *phase) {
		r := c.do("GET", "/oauth/userinfo", nil, bearer.Access, false)
		p.record(r.took, r.err == nil && r.status == 200, fmt.Sprintf("%d %s", r.status, trim(r.body)))
	}

	mgmtWork := func(c *client, p *phase) {
		r := c.do("GET", "/v1/organizations/"+e.orgID+"/projects?page_size=20", nil, bearer.Access, false)
		p.record(r.took, r.err == nil && r.status == 200, fmt.Sprintf("%d %s", r.status, trim(r.body)))
	}

	run := func(p *phase, work func(*client, *phase), workers int, pace time.Duration) {
		deadline := time.Now().Add(e.dur)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				c := session
				if pace == 0 {
					// Own connection pool per worker, or the pool's own
					// contention becomes the thing being measured.
					c = newClient(e.base, e.host)
					c.mu.Lock()
					for k, v := range session.cookie {
						c.cookie[k] = v
					}
					c.mu.Unlock()
				}
				for time.Now().Before(deadline) {
					work(c, p)
					if pace > 0 {
						time.Sleep(pace)
					}
				}
			}(w)
		}
		wg.Wait()
		p.sortTimes()
	}

	// LOAD_ONLY narrows the run to one phase, for sweeping concurrency against
	// a single endpoint without paying for the other three each time.
	only := os.Getenv("LOAD_ONLY")
	want := func(name string) bool { return only == "" || only == name }

	started := time.Now()
	if want("authorize") {
		run(authorize, authorizeWork, e.workers, 0)
	}

	if want("token") {
		var wg sync.WaitGroup
		deadline := time.Now().Add(e.dur)
		for i := 0; i < e.workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c := newClient(e.base, e.host)
				work := tokenWork(i)
				for time.Now().Before(deadline) {
					work(c, token)
				}
			}(i)
		}
		wg.Wait()
		token.sortTimes()
	}

	if want("userinfo") {
		run(userinfo, userinfoWork, e.workers, 0)
	}
	// 8 requests a second against a 600/minute per-client quota. Measuring an
	// endpoint past its own documented bound measures the limiter.
	if want("management") {
		run(mgmt, mgmtWork, 4, 500*time.Millisecond)
	}

	// docs/PLAN/12: "Include a mixed workload test: simultaneous token
	// issuance + management API traffic + authz checks, since production will
	// never see just one traffic type in isolation." Authz checks are Phase 2,
	// so this is the three shipped workloads at once — the same endpoints, now
	// competing for the same connections, pool and CPU.
	mixAuthorize := &phase{name: "mixed: authorize (silent)", target: authorize.target}
	mixToken := &phase{name: "mixed: token (refresh)", target: token.target}
	mixUserinfo := &phase{name: "mixed: userinfo", target: userinfo.target}
	mixMgmt := &phase{name: "mixed: management", target: mgmt.target}
	mixDeadline := time.Now().Add(e.dur)
	if only != "" {
		mixDeadline = time.Now()
	}
	var mwg sync.WaitGroup
	spawn := func(n int, fn func()) {
		for i := 0; i < n; i++ {
			mwg.Add(1)
			go func() { defer mwg.Done(); fn() }()
		}
	}
	for i := 0; i < e.workers/2; i++ {
		idx := i
		mwg.Add(1)
		go func() {
			defer mwg.Done()
			c := newClient(e.base, e.host)
			work := tokenWork(idx)
			for time.Now().Before(mixDeadline) {
				work(c, mixToken)
			}
		}()
	}
	spawn(e.workers/4, func() {
		c := sessionCopy(session, e)
		for time.Now().Before(mixDeadline) {
			authorizeWork(c, mixAuthorize)
		}
	})
	spawn(e.workers/4, func() {
		c := newClient(e.base, e.host)
		for time.Now().Before(mixDeadline) {
			userinfoWork(c, mixUserinfo)
		}
	})
	spawn(2, func() {
		c := newClient(e.base, e.host)
		for time.Now().Before(mixDeadline) {
			mgmtWork(c, mixMgmt)
			time.Sleep(500 * time.Millisecond)
		}
	})
	mwg.Wait()
	for _, p := range []*phase{mixAuthorize, mixToken, mixUserinfo, mixMgmt} {
		p.sortTimes()
	}
	elapsed := time.Since(started)

	// --- report ------------------------------------------------------------
	fmt.Printf("%-28s %8s %8s %8s %8s %9s %7s  %s\n", "phase", "n", "p50", "p95", "p99", "max", "rps", "vs docs/PLAN/12")
	failures := 0
	for _, p := range []*phase{authorize, token, userinfo, mgmt, mixAuthorize, mixToken, mixUserinfo, mixMgmt} {
		if len(p.took) == 0 {
			fmt.Printf("%-28s  no samples\n", p.name)
			failures++
			continue
		}
		p50, p95, p99 := p.pct(0.50), p.pct(0.95), p.pct(0.99)
		verdict := "within targets"
		if p50 > p.target[0] || p95 > p.target[1] || p99 > p.target[2] {
			verdict = "OVER"
			failures++
		}
		if p.bad > 0 {
			verdict += fmt.Sprintf(" — %d failed (%s)", p.bad, p.firstF)
			failures++
		}
		fmt.Printf("%-28s %8d %8s %8s %8s %8s %8.0f  %s\n",
			p.name, len(p.took), ms(p50), ms(p95), ms(p99), ms(p.took[len(p.took)-1]),
			float64(len(p.took))/e.dur.Seconds(), verdict)
		fmt.Printf("%-28s %8s %8s %8s %8s\n", "  target", "",
			ms(p.target[0]), ms(p.target[1]), ms(p.target[2]))
		// One machine-readable line per phase. A sweep that has to find the
		// columns of a human-formatted table by counting whitespace breaks the
		// first time a phase name contains a space, and "/v1 management (CRUD
		// read)" already does.
		fmt.Printf("CSV,%s,%d,%.2f,%.2f,%.2f,%.2f,%.1f,%d\n", strings.ReplaceAll(p.name, ",", ";"),
			len(p.took), msf(p50), msf(p95), msf(p99), msf(p.took[len(p.took)-1]),
			float64(len(p.took))/e.dur.Seconds(), p.bad)
	}

	fmt.Printf("\ntotal %s wall clock, %d requests\n", elapsed.Round(time.Second),
		len(authorize.took)+len(token.took)+len(userinfo.took)+len(mgmt.took))
	if failures > 0 {
		fmt.Printf("\n%d phase(s) missed a target or saw failures\n", failures)
		os.Exit(1)
	}
	fmt.Println("\nevery phase within its docs/PLAN/12 target")
}

// sessionCopy hands a worker its own connection pool while keeping the one
// signed-in session: the silent path needs that cookie, and a shared pool
// would make connection contention look like server latency.
func sessionCopy(session *client, e *env) *client {
	c := newClient(e.base, e.host)
	session.mu.Lock()
	for k, v := range session.cookie {
		c.cookie[k] = v
	}
	session.mu.Unlock()
	return c
}

func msf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func ms(d time.Duration) string {
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

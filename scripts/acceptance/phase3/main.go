// Command phase3 checks docs/PLAN/17's three Phase 3 criteria against a running
// deployment and prints the evidence (P3-15).
//
//	TOTP enrollment and verification work end-to-end, including recovery from
//	a lost device.
//	A rotated refresh token cannot be reused.
//	A user can view and revoke their own active sessions, and a revoked session
//	is immediately unusable.
//
// It runs ON the staging VM against the service's own port, looking like the
// tunnel, for the load test's reason: through the tunnel it would be testing
// Cloudflare as well. `scripts/acceptance-phase3.sh` creates a throwaway account
// and application and deletes both afterwards.
//
// Every negative is credited only beside its positive. "A wrong code is
// refused" is also true of a challenge that refuses everything, and "a revoked
// session cannot be used" is also true of a session that never worked; each
// refusal here is preceded by the same operation succeeding.
//
// Standard library only, for the reason `demo/` and the load test give:
// evidence that needs a dependency tree to reproduce is weaker evidence.
package main

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is HMAC-SHA1; this computes what an authenticator app computes.
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type client struct {
	base, host string
	http       *http.Client
	cookie     map[string]string
}

func newClient(base, host string) *client {
	return &client{
		base: base, host: host,
		http: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       20 * time.Second,
		},
		cookie: map[string]string{},
	}
}

type result struct {
	status   int
	body     string
	location string
}

func (c *client) send(method, path, contentType string, body io.Reader, bearer string) result {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		die("building a request: %v", err)
	}
	req.Host = c.host
	req.Header.Set("X-Forwarded-Proto", "https")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	parts := make([]string, 0, len(c.cookie))
	for k, v := range c.cookie {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		die("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, sc := range resp.Header.Values("Set-Cookie") {
		head := strings.SplitN(sc, ";", 2)[0]
		if k, v, ok := strings.Cut(head, "="); ok {
			if v == "" || strings.Contains(strings.ToLower(sc), "max-age=0") {
				delete(c.cookie, k)
			} else {
				c.cookie[k] = v
			}
		}
	}
	return result{status: resp.StatusCode, body: string(raw), location: resp.Header.Get("Location")}
}

func (c *client) form(path string, v url.Values) result {
	return c.send("POST", path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()), "")
}

func (c *client) get(path, bearer string) result { return c.send("GET", path, "", nil, bearer) }

func (c *client) json(method, path, body, bearer string) result {
	return c.send(method, path, "application/json", strings.NewReader(body), bearer)
}

// --- output ------------------------------------------------------------------

var passed, failed int

func section(s string) { fmt.Printf("\n\033[1;36m%s\033[0m\n", s) }
func pass(format string, a ...any) {
	passed++
	fmt.Printf("  \033[32m✓\033[0m %s\n", fmt.Sprintf(format, a...))
}
func fail(format string, a ...any) {
	failed++
	fmt.Printf("  \033[31m✗\033[0m %s\n", fmt.Sprintf(format, a...))
}
func note(format string, a ...any) { fmt.Printf("      \033[2m%s\033[0m\n", fmt.Sprintf(format, a...)) }
func die(format string, a ...any) {
	fmt.Printf("\n\033[31m✗ %s\033[0m\n", fmt.Sprintf(format, a...))
	os.Exit(2)
}

// --- the flow ----------------------------------------------------------------

type env struct {
	clientID, redirect, email, password string
	base, host                          string
}

var (
	reCSRF    = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)
	reRequest = regexp.MustCompile(`name="request" value="([^"]*)"`)
)

type tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	ID      string `json:"id_token"`
	Error   string `json:"error"`
}

func pkce() (string, string) {
	b := make([]byte, 32)
	_, _ = crand.Read(b)
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (e *env) authorize(c *client, challenge string, prompt string) result {
	q := url.Values{
		"response_type": {"code"}, "client_id": {e.clientID}, "redirect_uri": {e.redirect},
		"scope": {"openid offline_access"}, "state": {"s"}, "nonce": {"n"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	if prompt != "" {
		q.Set("prompt", prompt)
	}
	return c.get("/oauth/authorize?"+q.Encode(), "")
}

func codeFrom(location string) (code, errCode string) {
	u, err := url.Parse(location)
	if err != nil {
		return "", ""
	}
	return u.Query().Get("code"), u.Query().Get("error")
}

func (e *env) exchange(c *client, code, verifier string) tokens {
	r := c.form("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {e.clientID},
		"redirect_uri": {e.redirect}, "code_verifier": {verifier},
	})
	var t tokens
	_ = json.Unmarshal([]byte(r.body), &t)
	return t
}

func (e *env) refresh(c *client, refresh string) (tokens, int) {
	r := c.form("/oauth/token", url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {e.clientID},
	})
	var t tokens
	_ = json.Unmarshal([]byte(r.body), &t)
	return t, r.status
}

// passwordStep runs authorize and the password form. It returns the response
// to the password POST — a redirect carrying a code, or a challenge page.
func (e *env) passwordStep(c *client, prompt string) (result, string) {
	verifier, challenge := pkce()
	r := e.authorize(c, challenge, prompt)
	if r.status != http.StatusFound || !strings.Contains(r.location, "/login?request=") {
		die("authorize did not offer the login page: %d %s", r.status, r.location)
	}
	page := c.get(strings.TrimPrefix(r.location, "https://"+c.host), "")
	csrf, req := reCSRF.FindStringSubmatch(page.body), reRequest.FindStringSubmatch(page.body)
	if csrf == nil || req == nil {
		die("the login page has no csrf_token or request field")
	}
	return c.form("/login", url.Values{
		"csrf_token": {csrf[1]}, "request": {req[1]}, "email": {e.email}, "password": {e.password},
	}), verifier
}

// answer submits the second step on a challenge page.
func answer(c *client, page result, factor, code string) result {
	csrf, req := reCSRF.FindStringSubmatch(page.body), reRequest.FindStringSubmatch(page.body)
	if csrf == nil || req == nil {
		die("the challenge page has no csrf_token or request field")
	}
	return c.form("/login/mfa", url.Values{
		"csrf_token": {csrf[1]}, "request": {req[1]}, "factor": {factor}, "code": {code},
	})
}

func claims(idToken string) map[string]any {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func amrOf(idToken string) string {
	c := claims(idToken)
	if c == nil {
		return "(no id_token)"
	}
	b, _ := json.Marshal(c["amr"])
	return string(b)
}

func totp(secret string, at time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		die("decoding the TOTP secret: %v", err)
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func main() {
	e := &env{
		clientID: must("ACCEPT_CLIENT_ID"), email: must("ACCEPT_EMAIL"), password: must("ACCEPT_PASSWORD"),
		redirect: "http://localhost:9998/callback",
		base:     getenv("ACCEPT_BASE", "http://127.0.0.1:10800"),
		host:     getenv("ACCEPT_HOST", "auth.zedth.my.id"),
	}
	fmt.Printf("\033[1mPhase 3 acceptance\033[0m — %s (via %s)\n", e.host, e.base)

	// --- criterion 1: TOTP end to end ---------------------------------------
	section("1. TOTP enrolment and verification, end to end, and recovery from a lost device")

	first := newClient(e.base, e.host)
	signedIn, verifier := e.passwordStep(first, "")
	code, _ := codeFrom(signedIn.location)
	if code == "" {
		die("the fixture account could not sign in with its password: %d", signedIn.status)
	}
	start := e.exchange(first, code, verifier)
	if start.Access == "" {
		die("the code exchange failed: %s", start.Error)
	}
	pass("signed in with a password alone — amr %s", amrOf(start.ID))

	begin := first.json("POST", "/v1/me/mfa/totp", `{"label":"acceptance"}`, start.Access)
	var enrolment struct {
		FactorID string `json:"factor_id"`
		Secret   string `json:"secret"`
		URI      string `json:"provisioning_uri"`
	}
	_ = json.Unmarshal([]byte(begin.body), &enrolment)
	if begin.status != http.StatusCreated && begin.status != http.StatusOK || enrolment.Secret == "" {
		die("beginning TOTP enrolment answered %d: %s", begin.status, begin.body)
	}
	pass("enrolment began: factor %s, provisioning URI %s…", enrolment.FactorID, enrolment.URI[:min(len(enrolment.URI), 40)])

	enrolledAt := time.Now()
	confirm := first.json("POST", "/v1/me/mfa/totp/"+enrolment.FactorID+"/confirm",
		`{"code":"`+totp(enrolment.Secret, enrolledAt)+`"}`, start.Access)
	var confirmed struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.Unmarshal([]byte(confirm.body), &confirmed)
	if confirm.status != http.StatusOK || len(confirmed.RecoveryCodes) == 0 {
		die("confirming the factor answered %d: %s", confirm.status, confirm.body)
	}
	pass("confirmed with a computed code; %d recovery codes issued", len(confirmed.RecoveryCodes))

	// A new browser: the password is no longer enough.
	second := newClient(e.base, e.host)
	challenge, verifier := e.passwordStep(second, "")
	if c, _ := codeFrom(challenge.location); c != "" {
		fail("a password alone still signed in after enrolment")
	} else if !strings.Contains(challenge.body, `name="factor" value="totp"`) {
		fail("the password step did not present a TOTP challenge (status %d)", challenge.status)
	} else {
		pass("the password alone is no longer enough — the challenge asks for the authenticator app")
	}

	// Positive and negative on the same challenge: wrong first, then right.
	wrong := answer(second, challenge, "totp", "000000")
	if c, _ := codeFrom(wrong.location); c != "" {
		fail("a wrong TOTP code completed the sign-in")
	} else if strings.Contains(wrong.body, "not correct") {
		pass("a wrong code is refused on the challenge, and the person can try again")
	} else {
		fail("a wrong code produced neither a sign-in nor the refusal message (status %d)", wrong.status)
	}
	right := answer(second, wrong, "totp", totp(enrolment.Secret, enrolledAt.Add(30*time.Second)))
	rc, _ := codeFrom(right.location)
	if rc == "" {
		fail("the right TOTP code did not complete the sign-in (status %d)", right.status)
	} else {
		t := e.exchange(second, rc, verifier)
		amr := amrOf(t.ID)
		if amr == `["mfa","otp","pwd"]` {
			pass("the right code signs in — amr %s", amr)
		} else {
			fail("signed in, but amr is %s, want [\"mfa\",\"otp\",\"pwd\"]", amr)
		}
	}

	// The lost device: a recovery code, once.
	lost := newClient(e.base, e.host)
	page, verifier := e.passwordStep(lost, "")
	recoveryCode := strings.ToLower(confirmed.RecoveryCodes[0])
	used := answer(lost, page, "recovery", recoveryCode)
	uc, _ := codeFrom(used.location)
	if uc == "" {
		fail("a recovery code did not sign in (status %d)", used.status)
	} else {
		t := e.exchange(lost, uc, verifier)
		pass("device lost: a recovery code, typed in lower case, signs in — amr %s (mfa, and not otp)", amrOf(t.ID))
	}
	again := newClient(e.base, e.host)
	page2, _ := e.passwordStep(again, "")
	reused := answer(again, page2, "recovery", recoveryCode)
	if c, _ := codeFrom(reused.location); c != "" {
		fail("the same recovery code signed in twice")
	} else {
		pass("the same recovery code is refused the second time")
		note("the documented process for a user with no codes is deploy/RUNBOOK-mfa-recovery.md § C")
	}

	// --- criterion 2: rotated refresh token ---------------------------------
	section("2. A rotated refresh token cannot be reused")

	rotator := newClient(e.base, e.host)
	r1, _ := e.refresh(rotator, start.Refresh)
	if r1.Refresh == "" || r1.Refresh == start.Refresh {
		fail("a refresh did not rotate the token")
	} else {
		pass("a refresh returns a new refresh token")
	}
	r2, _ := e.refresh(rotator, r1.Refresh)
	if r2.Refresh == "" {
		fail("the new token did not refresh (positive control)")
	} else {
		pass("the new token refreshes (it is used, so the old one can no longer be a lost-response retry)")
	}
	if _, status := e.refresh(rotator, start.Refresh); status != http.StatusBadRequest {
		fail("the rotated token was accepted again (status %d)", status)
	} else {
		pass("the rotated token is refused: invalid_grant")
	}
	if _, status := e.refresh(rotator, r2.Refresh); status == http.StatusOK {
		fail("the family survived the reuse — the newest token still works")
	} else {
		pass("and the whole family is revoked — the newest token no longer works either")
	}

	// --- criterion 3: sessions -----------------------------------------------
	section("3. A user views and revokes their own sessions; a revoked one is unusable at once")

	// Two sessions: `second` (TOTP) and `lost` (recovery). `lost` does the
	// revoking; `second` is revoked. Tokens for each from a silent authorize, so
	// each holds a fresh refresh token bound to its own session.
	silent := func(c *client) tokens {
		v, ch := pkce()
		r := e.authorize(c, ch, "none")
		code, errCode := codeFrom(r.location)
		if code == "" {
			die("silent authorize gave no code (%s)", errCode)
		}
		return e.exchange(c, code, v)
	}
	victim := silent(second)
	actor := silent(lost)
	pass("two live sessions, each silently issuing tokens (positive control for what follows)")

	list := lost.get("/v1/me/sessions", actor.Access)
	var sessions struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal([]byte(list.body), &sessions)
	victimSID, _ := claims(victim.ID)["sid"].(string)
	var sawVictim, sawCurrent bool
	for _, s := range sessions.Sessions {
		sawVictim = sawVictim || s.ID == victimSID
		sawCurrent = sawCurrent || s.Current
	}
	if list.status == http.StatusOK && sawVictim && sawCurrent {
		pass("the user lists their sessions: %d, including this one (marked current) and the other device", len(sessions.Sessions))
	} else {
		fail("the session list (status %d) did not show both sessions", list.status)
	}

	del := lost.send("DELETE", "/v1/me/sessions/"+victimSID, "", nil, actor.Access)
	if del.status == http.StatusNoContent {
		pass("the user revokes the other session: 204")
	} else {
		fail("revoking the other session answered %d: %s", del.status, del.body)
	}

	_, ch := pkce()
	after := e.authorize(second, ch, "none")
	if code, errCode := codeFrom(after.location); code == "" && errCode == "login_required" {
		pass("the revoked session's cookie no longer signs anything in — login_required, immediately")
	} else {
		fail("the revoked session still issued a code (error=%q)", errCode)
	}
	if _, status := e.refresh(second, victim.Refresh); status != http.StatusBadRequest {
		fail("the revoked session's refresh token still works (status %d)", status)
	} else {
		pass("and its refresh token is refused")
	}
	if _, status := e.refresh(lost, actor.Refresh); status != http.StatusOK {
		fail("the user's own session was affected by revoking the other (status %d)", status)
	} else {
		pass("while the user's own session carries on")
	}

	fmt.Printf("\n\033[1m%d passed, %d failed\033[0m\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		die("%s is required", k)
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

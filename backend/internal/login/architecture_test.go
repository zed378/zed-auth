package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claims about the wiring, asserted by reading it (P3-03).
//
// Two of this task's controls are optional fields with a nil default —
// `Handler.MFA` and `Framework.Attempts`. That shape is deliberate and it is
// the shape the rest of this package already uses for `Limiter`: a unit test
// about something else should not have to build a factor framework.
//
// The cost of that shape is that a deployment could simply not set them, and
// nothing would say so — the login would work, no factor would ever be asked
// for, and every user who enrolled one would believe they were protected. That
// is precisely the failure `P3-03` exists to prevent, arriving through the
// wiring instead of through the logic.
//
// A comment saying "production always sets it" does not make it true. These do.

// mainSource reads the service's entry point.
func mainSource(t *testing.T) string {
	t.Helper()

	path := filepath.Join("..", "..", "cmd", "authservice", "main.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	// Without this, a truncated or moved file would make every assertion below
	// hold trivially — the failure mode this project keeps finding.
	if len(body) < 10_000 {
		t.Fatalf("%s is only %d bytes; these assertions would prove nothing", path, len(body))
	}
	return string(body)
}

// The login handler is given a factor framework.
//
// Without this, `Handler.MFA` stays nil, `challengeFor` returns no challenge,
// and every enrolled factor in the estate is decorative.
func TestTheLoginHandlerIsGivenAFactorFramework(t *testing.T) {
	src := mainSource(t)

	if !strings.Contains(src, "MFA:           factorFramework") {
		t.Error("the login handler is constructed without its MFA field; no login would ever be challenged")
	}
	if !strings.Contains(src, "buildMFA(") {
		t.Error("nothing builds the factor framework")
	}
}

// The challenge route is registered.
//
// A framework with no route is a challenge page issued and then unreachable:
// the password step would set a handle cookie, render a form posting to
// /login/mfa, and the browser would get a 404 — a login nobody can finish.
func TestTheChallengeRouteIsRegistered(t *testing.T) {
	path := filepath.Join("..", "httpserver", "server.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	src := string(body)

	if !strings.Contains(src, `mux.Method(http.MethodPost, "/login/mfa"`) {
		t.Error("POST /login/mfa is not registered, so no code could ever be submitted")
	}
	if !strings.Contains(src, `mux.Method(http.MethodGet, "/login/mfa"`) {
		t.Error("GET /login/mfa is not registered, so a refresh would lose the challenge")
	}

	if !strings.Contains(mainSource(t), "mfaRoute(factorFramework, loginHandler)") {
		t.Error("the challenge route is not wired to the login handler")
	}
}

// The factor framework is given its attempt bound.
//
// `Framework.Attempts` nil means unbounded guessing across challenges, which
// leaves only `MaxAttempts`' five-per-challenge — and an attacker who holds the
// password restarts the login for free, so five per challenge is not a bound at
// all. This is the control the whole step leans on.
func TestTheFactorFrameworkIsGivenItsAttemptBound(t *testing.T) {
	src := mainSource(t)

	if !strings.Contains(src, "Attempts:   &mfa.RedisAttempts{") {
		t.Error("the factor framework is built without an attempt bound; codes could be guessed without limit")
	}
}

// MFA is all-or-nothing: no key means no framework, rather than a framework
// that cannot open the secrets it is asked to verify against.
func TestMFAIsNotHalfEnabled(t *testing.T) {
	src := mainSource(t)

	if !strings.Contains(src, "if !cfg.MFA.Enabled() {") {
		t.Error("buildMFA does not check whether MFA is configured before building it")
	}

	// A resolve failure must be fatal. A deployment that ASKED for MFA and
	// could not read its key starting anyway is every enrolled user silently
	// signing in on a password alone.
	if !strings.Contains(src, "resolve MFA seal key: %w") {
		t.Error("a failure to resolve the seal key is not reported as a startup failure")
	}
}

// The handle never reaches a URL.
//
// Asserted against the source rather than against one response, because the
// claim is about every path: a handle in a query string reaches the Referer
// header, the browser history, and every access log in between. The cookie is
// the only carrier.
func TestTheChallengeHandleIsOnlyEverReadFromACookie(t *testing.T) {
	body, err := os.ReadFile("mfa.go")
	if err != nil {
		t.Fatalf("reading mfa.go: %v", err)
	}
	src := string(body)

	for _, forbidden := range []string{
		`URL.Query().Get("handle")`,
		`PostForm.Get("handle")`,
		`Query().Get("challenge")`,
		`PostForm.Get("challenge")`,
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("the challenge handle is read from %s; it must come from the cookie only", forbidden)
		}
	}

	if !strings.Contains(src, "r.Cookie(ChallengeCookieName)") {
		t.Fatal("nothing reads the challenge cookie; this test is asserting against the wrong file")
	}
}

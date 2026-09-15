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

	// Through `challenger`, since P3-15: assigning the *mfa.Framework directly
	// put a nil pointer inside the interface on a deployment with no seal key,
	// and every sign-in answered 500. Either wiring without the helper is wrong
	// now, so the direct form is refused as well as the missing one.
	if !strings.Contains(src, "MFA:           challenger(factorFramework)") {
		t.Error("the login handler is not given its MFA field through challenger(); no login would ever be challenged, or a nil framework would take sign-in down")
	}
	if strings.Contains(src, "MFA:           factorFramework,") {
		t.Error("the login handler is given the *mfa.Framework directly — a nil one inside the interface passes its nil check")
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

// P3-08: the report page and the detector are wired, and the route exists. A
// notice carrying a link to an unregistered route would tell somebody who
// believes a stranger is in their account to click a 404.
func TestTheReportPageAndDetectorAreWired(t *testing.T) {
	src := mainSource(t)

	for _, want := range []string{
		"loginHandler.Reports = &login.NotMeFlow{",
		"loginHandler.Anomalies = anomalies",
		"buildAnomalyDetection(cfg,",
		"NotMe:          http.HandlerFunc(loginHandler.NotMe)",
		"detector.Notifier = &login.AnomalyMail{",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go does not contain %q", want)
		}
	}

	body, err := os.ReadFile(filepath.Join("..", "httpserver", "server.go"))
	if err != nil {
		t.Fatalf("reading server.go: %v", err)
	}
	for _, want := range []string{
		`mux.Method(http.MethodGet, "/account/not-me"`,
		`mux.Method(http.MethodPost, "/account/not-me"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("server.go does not register %s", want)
		}
	}
	if NotMePath != "/account/not-me" {
		t.Errorf("NotMePath is %q but the registered route is /account/not-me", NotMePath)
	}
}

// P3-12: every password-set path validates through the one PasswordValidator,
// and the breach corpus is handed to it.
//
// The gap this closes was a wiring gap and nothing else: the breach client was
// built in main and passed to nothing, so no test of the validator or the page
// could have seen it. This reads the wiring.
func TestEveryPasswordSetPathUsesTheValidatorWithTheCorpus(t *testing.T) {
	src := mainSource(t)

	for _, want := range []string{
		"passwordValidator := &authn.PasswordValidator{",
		"Breaches: passwords.breaches,",
		"Policy:    passwordValidator,",
		"Validator:     passwordValidator,",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go does not contain %q", want)
		}
	}
	if strings.Contains(src, "passwordPolicy{") {
		t.Error("main.go still builds the policy-only adapter that skipped the breach corpus")
	}
}

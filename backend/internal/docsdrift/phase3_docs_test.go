// Package docsdrift holds tests that read the published documentation and fail
// when it disagrees with the code.
//
// It has no non-test code. It exists as its own package because the facts a
// guide states come from several packages at once — `mfa`, `oauth/token`,
// `authn`, `audit`, `management` — and no one of them can import all the
// others.
package docsdrift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
)

// The Phase 3 guides (P3-13) against the values the service actually uses.
//
// The card's Definition of Done is three sentences about agreement: the
// rotation guide describes reuse detection and the grace window accurately, the
// `amr` guide matches what is emitted, and the lost-device process matches what
// was built. Each is a claim about a page and a constant agreeing, and a page
// read carefully once is agreement on the day it was read. A consumer builds
// against the number on the page; the day the constant moves, this fails.

func docsRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "public-site", "docs")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the published docs are not where this test expects them (%s): %v", root, err)
	}
	return root
}

func page(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(docsRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("reading %s: %v — a guide that moved makes every assertion here pass against nothing", name, err)
	}
	// Prose is wrapped at arbitrary points, so a phrase can span a line break.
	return strings.Join(strings.Fields(string(body)), " ")
}

func mustContain(t *testing.T, name, body, want, why string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("%s does not say %q — %s", name, want, why)
	}
}

// days renders a duration the way the guides write it.
func days(d time.Duration) string { return fmt.Sprintf("%d days", int(d/(24*time.Hour))) }

func minutes(d time.Duration) string { return fmt.Sprintf("%d minutes", int(d/time.Minute)) }

func seconds(d time.Duration) string { return fmt.Sprintf("%d seconds", int(d/time.Second)) }

var numberWords = map[int]string{3: "three", 5: "five", 10: "ten", 14: "fourteen", 15: "fifteen"}

func word(t *testing.T, n int) string {
	t.Helper()
	w, ok := numberWords[n]
	if !ok {
		t.Fatalf("no word for %d: the constant changed, and the guides that spell it out need rewriting", n)
	}
	return w
}

// amrArray renders an `amr` value as the guide's table prints it.
func amrArray(methods []string) string {
	quoted := make([]string, len(methods))
	for i, m := range methods {
		b, _ := json.Marshal(m)
		quoted[i] = string(b)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// --- the `amr` guide ----------------------------------------------------------

func TestTheStepUpGuideListsTheAmrValuesActuallyEmitted(t *testing.T) {
	const name = "guides/step-up-with-amr.md"
	body := page(t, name)

	rows := map[string][]string{
		"Password only":                    mfa.AuthMethods(true),
		"Password, then authenticator app": mfa.AuthMethods(true, mfa.TypeTOTP),
		"Password, then passkey":           mfa.AuthMethods(true, mfa.TypeWebAuthn),
		"Password, then a recovery code":   mfa.AuthMethodsWithRecovery(true, true),
	}
	for label, methods := range rows {
		want := fmt.Sprintf("| %s | `%s` |", label, amrArray(methods))
		mustContain(t, name, body, want,
			"a consumer copies this array into a comparison, and a wrong one is a step-up check that never passes or always does")
	}

	// The vocabulary table: exactly the values the service can emit, no more.
	emitted := map[string]bool{
		mfa.MethodPassword: true, mfa.TypeTOTP.AMR(): true,
		mfa.TypeWebAuthn.AMR(): true, mfa.MethodMultiFactor: true,
	}
	start := strings.Index(body, "### The values `amr` can hold")
	end := strings.Index(body, "No other values are emitted.")
	if start < 0 || end < start {
		t.Fatalf("%s no longer has its amr vocabulary table where this test reads it", name)
	}
	vocabulary := regexp.MustCompile("\\| `([a-z]+)` \\| [A-Z]")
	listed := map[string]bool{}
	for _, m := range vocabulary.FindAllStringSubmatch(body[start:end], -1) {
		listed[m[1]] = true
	}
	for value := range emitted {
		if !listed[value] {
			t.Errorf("%s does not list the amr value %q, which the service emits", name, value)
		}
	}
	for value := range listed {
		if !emitted[value] {
			t.Errorf("%s lists the amr value %q, which the service never emits", name, value)
		}
	}

	// The recovery-code row is the one a careless edit gets wrong in the
	// dangerous direction: `otp` for a user who has lost their device.
	if strings.Contains(amrArray(mfa.AuthMethodsWithRecovery(true, true)), `"otp"`) {
		t.Fatal("a recovery-code sign-in emits otp; the guide's warning is now false")
	}
}

func TestTheStepUpGuideUsesTheServedAuthorizePath(t *testing.T) {
	server, err := os.ReadFile(filepath.Join("..", "httpserver", "server.go"))
	if err != nil {
		t.Fatalf("reading the router: %v", err)
	}
	if !strings.Contains(string(server), `"/oauth/authorize"`) {
		t.Fatal("the router no longer serves /oauth/authorize; the guides send people there")
	}
	for _, name := range []string{"guides/step-up-with-amr.md", "guides/refresh-token-rotation.md", "quickstart.md"} {
		if strings.Contains(page(t, name), "/oauth2/") {
			t.Errorf("%s points at /oauth2/, which this service does not serve", name)
		}
	}
}

// --- the rotation guide -------------------------------------------------------

func TestTheRotationGuideStatesTheRealWindowAndLifetimes(t *testing.T) {
	const name = "guides/refresh-token-rotation.md"
	body := page(t, name)

	mustContain(t, name, body, "within "+seconds(token.RotationGrace)+"**",
		"the retry window is the number an integrator designs a retry policy around")
	mustContain(t, name, body, "the token that replaced it has never been used",
		"the window alone is not the rule — a guide that omits the second condition describes a thirty-second hole")
	mustContain(t, name, body, "| Access token | "+minutes(token.AccessTokenLifetime)+" |", "access token lifetime")
	mustContain(t, name, body, "| ID token | "+minutes(token.IDTokenLifetime)+" |", "ID token lifetime")
	mustContain(t, name, body, "| Refresh token | "+days(token.RefreshTokenLifetime), "refresh token lifetime")
	mustContain(t, name, body, "| Refresh token family | "+days(token.FamilyLifetime), "family lifetime")
	mustContain(t, name, body, "`"+string(audit.EventRefreshReuseDetected)+"`",
		"an operator searches the audit log for exactly this string")

	// The refusal the guide quotes is the one the handler sends.
	handler, err := os.ReadFile(filepath.Join("..", "oauth", "token", "handler.go"))
	if err != nil {
		t.Fatalf("reading the token handler: %v", err)
	}
	const refusal = "the refresh token is not valid"
	if !strings.Contains(string(handler), `"`+refusal+`"`) {
		t.Fatalf("the handler no longer answers %q; the guide quotes it", refusal)
	}
	mustContain(t, name, body, refusal, "the quoted refusal")
}

// --- the end-user guide: the lost-device process ------------------------------

func TestTheUserGuideMatchesTheRecoveryAndAttemptRules(t *testing.T) {
	const name = "guides/two-step-verification.md"
	body := page(t, name)

	mustContain(t, name, body, "**"+word(t, mfa.RecoveryCodeCount)+" recovery codes**", "the number of codes a user is told to expect")
	mustContain(t, name, body, word(t, mfa.RecoveryLowWaterMark)+" or fewer", "when the low-code warning appears")
	mustContain(t, name, body, "the last "+minutes(mfa.RecentAuthentication), "the recent-sign-in requirement")
	mustContain(t, name, body, seconds(time.Duration(mfa.TOTPSkew)*mfa.TOTPPeriod)+" either side", "the clock tolerance")
	mustContain(t, name, body,
		fmt.Sprintf("%s wrong codes in %s minutes", word(t, mfa.MaxAttemptsPerWindow), word(t, int(mfa.AttemptWindow/time.Minute))),
		"the per-user attempt bound")
	mustContain(t, name, body, "Use a recovery code", "the button the user presses on the challenge page")
}

// --- the administrator guide --------------------------------------------------

func TestTheAdminGuideMatchesTheMandate(t *testing.T) {
	const name = "guides/require-mfa.md"
	body := page(t, name)

	grace := int(authn.MFAGracePeriod / (24 * time.Hour))
	mustContain(t, name, body, fmt.Sprintf("**%s days**", strings.ToUpper(word(t, grace)[:1])+word(t, grace)[1:]), "the grace period")
	mustContain(t, name, body, fmt.Sprintf(`"grace_period_days": %d`, grace), "the example response")
	mustContain(t, name, body, word(t, mfa.MaxAttemptsPerWindow)+" wrong codes in "+word(t, int(mfa.AttemptWindow/time.Minute))+" minutes", "the attempt bound")

	for _, event := range []audit.EventType{
		audit.EventMFAMandateEnabled, audit.EventMFAMandateDisabled, audit.EventMFAEnrolled,
		audit.EventMFARemoved, audit.EventMFARecoveryUsed, audit.EventMFAResetByAdmin,
	} {
		mustContain(t, name, body, "`"+string(event)+"`", "an administrator filters the audit log by this exact type")
	}

	// The role each call needs, from the table the router enforces.
	for route, sentence := range map[string]string{
		"GET /v1/organizations/{org_id}/mfa-impact":                 "Requires `%s`. `without_factor`",
		"PATCH /v1/organizations/{org_id}":                          "requires `%s`.",
		"POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset": "which requires `%s`",
	} {
		requirement, ok := management.Policy[route]
		if !ok {
			t.Fatalf("%s is not in the policy table", route)
		}
		mustContain(t, name, body, fmt.Sprintf(sentence, requirement.Role), "the role the guide names for "+route)
	}
}

// --- the audit for unshipped claims (card step 6) -----------------------------

// SAML, social sign-in and Project Grants are Phase 4. A page may name them only
// in a section that also names the phase — sections, because a concept page
// states the phase once under its heading rather than in every paragraph.
func TestNoPageDescribesAPhase4CapabilityAsAvailable(t *testing.T) {
	root := docsRoot(t)
	// "Google or" / "Google," rather than "Google": Google Authenticator is a
	// TOTP app, and a guide naming it claims nothing about social sign-in.
	mentions := regexp.MustCompile(`(?i)\bSAML\b|social (sign-in|login|provider)|Google (or|,)|Project Grant`)
	phase := regexp.MustCompile(`Phases? 4\b`)

	pages := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "api-reference" {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		pages++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, section := range strings.Split("\n"+string(raw), "\n#") {
			flat := strings.Join(strings.Fields(section), " ")
			if mentions.MatchString(flat) && !phase.MatchString(flat) {
				t.Errorf("%s: a section mentions a Phase 4 capability without naming the phase:\n  %.200s", path, flat)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the docs: %v", err)
	}
	if pages < 10 {
		t.Fatalf("read only %d pages — the docs moved and this audit checked nothing", pages)
	}
}

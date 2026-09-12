package login

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// The page itself.
//
// There is no JavaScript. Not "it degrades gracefully" — none at all, which is
// what makes `default-src 'none'` an achievable policy rather than an
// aspirational one, and what leaves no DOM sink for an injected value to reach.
// FR-1 asks for a page that works without script; this goes further because
// the stronger version is also the simpler one.
//
// The stylesheet is inline and whitelisted by its SHA-256 hash rather than by
// a nonce. A nonce is the usual choice and would be wrong here: it changes
// every response, and one of this page's acceptance criteria is that two
// failed logins produce byte-identical responses. A hash is a function of the
// stylesheet, so it is stable for a given organization's branding and varies
// only when the styling does — which is also a stricter policy, since a hash
// authorises one exact block of CSS and a nonce authorises whatever the server
// happened to put a nonce on.

// Page is everything the template renders.
//
// Nothing here comes from the query string. That is abuse case A-7's control
// and it is a property of this struct: there is no field an attacker could
// steer, because the only attacker-influenced value is Email, which the person
// typing it supplied themselves.
type Page struct {
	// AppName is the registered name of the application the user is signing
	// in to. It comes from the applications table, not from the request.
	AppName string

	Branding Branding

	// Style is the rendered stylesheet, and StyleHash is its CSP hash. Kept
	// as fields so the handler computes both once and the template cannot get
	// them out of step.
	Style     template.CSS
	StyleHash string

	CSRFToken string
	RequestID string

	// Email is re-rendered so a typo need not retype the address. The
	// password never is, on any path.
	Email string

	// Error is the form-level message. One of the constants below, never a
	// value assembled from the request.
	Error string

	// EmailError and PasswordError are field-level, and only ever set for a
	// field left empty — a lookup that happened cannot produce a field error,
	// because a field error is per-field and the credential answer must not be.
	EmailError    string
	PasswordError string

	ForgotPath string

	// RedirectOrigin is the origin of the redirect URI this sign-in will end
	// at — `scheme://host[:port]`, nothing more.
	//
	// It exists for the Content-Security-Policy, and it is not cosmetic.
	// Chrome applies `form-action` to the REDIRECT CHAIN a submission
	// produces, not only to the action URL. With `form-action 'self'` the
	// POST to /login is allowed, the service answers 302 to the client's
	// registered redirect URI, and the browser refuses to follow it — so no
	// sign-in can complete in any Chromium browser. The page simply sits
	// there with the fields still filled and one console message.
	//
	// Empty when the URI cannot be parsed, which leaves the policy at 'self'
	// and the flow broken — but broken closed, and only in a case that cannot
	// arise from a registration the service accepted.
	RedirectOrigin string
}

// The messages. Every credential failure uses the same one.
//
// A named constant rather than a literal at each call site, so the uniformity
// is visible in one place and a future edit that wants to be more helpful about
// one case has to change the constant that every case shares.
const (
	// MsgCredentials answers a wrong password, an unknown address, a locked
	// account, a deactivated account, and an account with no password set.
	// All five, deliberately: any distinction here is an answer to "does this
	// address have an account", which is docs/SECURITY/02 §12's enumeration
	// disclosure and the reason credential-stuffing lists are worth money.
	MsgCredentials = "Your email or password is incorrect."

	// MsgPasswordExpired is different, and may be: by the time it is shown the
	// password was correct, so the person reading it has already proved they
	// are the account holder. Telling them discloses nothing they did not just
	// demonstrate.
	MsgPasswordExpired = "Your password has expired and must be reset before you can sign in. " +
		"Please ask your organization's administrator to reset it."

	// MsgMethodNotAllowed is shown when an organization's
	// `allowed_login_methods` excludes passwords (P2-10).
	//
	// It says what is actually wrong, unlike MsgCredentials, and may: this is a
	// fact about the ORGANIZATION, which the person already knows they belong
	// to — they arrived through one of its applications. Hiding it behind
	// "incorrect email or password" would send somebody to reset a password
	// that was never going to work, which is a worse outcome than the
	// disclosure it avoids.
	MsgMethodNotAllowed = "Password sign-in is not available for this organization. " +
		"Please use the sign-in method your administrator has enabled."

	// MsgSessionProblem covers a missing or mismatched CSRF token. Usually a
	// tab left open overnight rather than an attack, and worded for the person
	// who will actually see it.
	MsgSessionProblem = "This page has been open for a while. Please try again."

	MsgEmailRequired    = "Enter your email address."
	MsgPasswordRequired = "Enter your password."

	// MsgRateLimited is shown when the attempt was refused before the password
	// was even looked at.
	//
	// It says something TRUE and specific, unlike every other refusal on this
	// page, and that is safe precisely because the counter is keyed on the
	// SUBMITTED ADDRESS rather than on a resolved user (P1-13). The counter
	// exists for an address with no account exactly as it does for a real one,
	// so this message tells an attacker only about their own behaviour - which
	// they already know, because they produced it.
	//
	// Being vague here would be worse than useless: a user told "your email or
	// password is incorrect" while actually in a cooldown will keep retrying,
	// which is both a worse experience and more load.
	MsgRateLimited = "Too many sign-in attempts. Please wait a few minutes and try again."

	// MsgWrongCode answers a wrong code AND a factor type the challenge has no
	// factor for (P3-03).
	//
	// Both, for the reason MsgCredentials covers five cases: the second is a
	// fact about what this user has ENROLLED, and a form that answered it
	// differently would let somebody probe the shape of an account they have a
	// password for but no code.
	//
	// It is more specific than MsgCredentials, and may be. By the time it is
	// read a password has been proven, so the person seeing it has already
	// demonstrated they are the account holder — telling them their code was
	// wrong discloses nothing they did not just establish, and telling them
	// anything vaguer would leave them retrying the password that was correct.
	MsgWrongCode = "That code is not correct. Check your authenticator app and try again."

	// MsgCodeRateLimited is the PER-USER bound (P3-03 step 2).
	//
	// Truthful and specific, on MsgRateLimited's reasoning: the counter is
	// keyed on a user whose password the reader has already proven, so it
	// describes only their own behaviour. A vague message here would be
	// actively harmful — somebody in a cooldown who is told "wrong code" will
	// keep typing correct codes and watching them fail.
	MsgCodeRateLimited = "Too many incorrect codes. Please wait a few minutes and try again."

	// MsgChallengeGone covers expired, spent, missing and never-was.
	//
	// One message for all four: the difference between them is a fact about a
	// login the reader may not own, and there is one way onward from every one
	// of them.
	MsgChallengeGone = "This sign-in step has expired or is no longer valid. Please sign in again."
)

// styleTemplate is the page's CSS.
//
// The token VALUES from docs/UI-UX/05 rather than an import of the console's
// stylesheet, for the reason DefaultAccent gives. `--color-accent` is the one
// value an organization may change, so it is the one substitution.
const styleTemplate = `
:root{
--color-bg-base:#f6f7f9;
--color-bg-surface:#ffffff;
--color-text-primary:#15181d;
--color-text-secondary:#586170;
--color-border:#828d9c;
--color-danger:#b42318;
--color-accent:%s;
}
*{box-sizing:border-box}
body{margin:0;background:var(--color-bg-base);color:var(--color-text-primary);
font:16px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{min-height:100vh;display:grid;place-items:center;padding:24px}
.card{background:var(--color-bg-surface);border:1px solid var(--color-border);
border-radius:12px;padding:32px;width:100%%;max-width:24rem}
.mark{display:block;margin:0 0 24px;max-height:40px;max-width:100%%}
.org{margin:0 0 24px;font-weight:600;color:var(--color-text-secondary)}
h1{font-size:1.375rem;line-height:1.3;margin:0 0 24px}
.alert{border:1px solid var(--color-danger);border-left-width:4px;border-radius:6px;
padding:12px 16px;margin:0 0 24px;color:var(--color-danger)}
.alert p{margin:0}
.alert:focus{outline:2px solid var(--color-accent);outline-offset:2px}
.field{margin:0 0 20px}
label{display:block;margin:0 0 6px;font-weight:600;font-size:0.9375rem}
input[type=email],input[type=password]{display:block;width:100%%;padding:10px 12px;
font:inherit;color:inherit;background:var(--color-bg-surface);
border:1px solid var(--color-border);border-radius:6px}
input:focus-visible,button:focus-visible,a:focus-visible{outline:2px solid var(--color-accent);
outline-offset:2px}
.err{margin:6px 0 0;font-size:0.875rem;color:var(--color-danger)}
button{display:block;width:100%%;padding:11px 16px;font:inherit;font-weight:600;
color:#ffffff;background:var(--color-accent);border:1px solid var(--color-accent);
border-radius:6px;cursor:pointer}
.check{display:flex;align-items:center;gap:8px;font-weight:400;font-size:0.9375rem}
.check input{width:1rem;height:1rem;margin:0;accent-color:var(--color-accent)}
.foot{margin:20px 0 0;font-size:0.875rem;text-align:center}
a{color:var(--color-accent)}
.note{margin:0 0 24px;color:var(--color-text-secondary)}
`

// renderStyle substitutes the accent and returns the stylesheet with its hash.
//
// The accent has already been through ValidateAccent, which admits only
// #rrggbb — so what is interpolated here cannot close the declaration and
// start another. That is stated as a comment because the safety lives in the
// validator rather than in this function, and a future edit that widened the
// validator would break this without touching it.
func renderStyle(accent string) (template.CSS, string) {
	if _, reason := ValidateAccent(accent); reason != "" {
		accent = DefaultAccent
	}

	css := fmt.Sprintf(styleTemplate, accent)
	sum := sha256.Sum256([]byte(css))
	return template.CSS(css), "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// ContentSecurityPolicy is the policy for one rendering of the page.
//
// Built per page rather than set as a constant in middleware, because two of
// its directives depend on what the page actually contains: the style hash,
// and whether there is a logo to permit at all. An organization with no logo
// gets `img-src 'none'`, which is not a detail — it means the default page
// cannot be made to emit a request to anywhere.
func (p Page) ContentSecurityPolicy() string {
	img := "'none'"
	if p.Branding.LogoURL != "" {
		// https: rather than the logo's own origin. Narrower would be better,
		// but the value is per organization and a Content-Security-Policy
		// header that varies by tenant is one an intermediary can cache
		// against the wrong tenant.
		img = "https:"
	}

	return strings.Join([]string{
		// Nothing loads unless a directive below says so. Script is not
		// mentioned anywhere, so there is no script, ever.
		"default-src 'none'",
		"style-src '" + p.StyleHash + "'",
		"img-src " + img,
		// The form may post back to this service, and the browser may follow
		// the redirect that produces — to this application's REGISTERED
		// redirect origin and nowhere else.
		//
		// Without the second source, nothing works: Chrome enforces
		// `form-action` against the redirect chain, so `'self'` alone blocks
		// the 302 that ends every successful sign-in. Every test before
		// `P1-27` drove this page with curl, which has no CSP, so the page
		// passed every one of them and could not log anyone in.
		//
		// Without the FIRST source, an injected form action would exfiltrate
		// the password to another origin, and this is the one page where that
		// is the whole prize. The origin added here is the one the client
		// registered and that `P1-06` matched by exact string comparison — not
		// anything from this request.
		"form-action " + formAction(p.RedirectOrigin),
		// Clickjacking, in the modern spelling. X-Frame-Options is sent too:
		// they overlap, and the older header is the one some proxies and
		// embedded browsers still act on.
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// formAction builds the `form-action` source list.
//
// `'self'` plus one registered origin, or `'self'` alone when there is none to
// add. Kept as a function so the login page and the logout interstitial cannot
// drift apart on the one directive whose failure mode is "nothing works, and
// only in a real browser".
func formAction(redirectOrigin string) string {
	if redirectOrigin == "" {
		return "'self'"
	}
	return "'self' " + redirectOrigin
}

// originOf reduces a URL to scheme://host[:port].
//
// Anything else in the URL — path, query, fragment — is meaningless in a CSP
// source and including it would silently widen or break the directive. A URL
// this cannot parse, or one that is not http(s), yields the empty string.
func originOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// pageTemplate is the login form.
//
// Read it for what is absent: no <script>, no event-handler attribute, no
// style attribute, and no value that came from the query string. The only
// interpolations are the application's registered name, the organization's
// validated logo, the two opaque tokens, and the address the user typed.
var pageTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign in</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
{{if .Branding.LogoURL}}<img class="mark" src="{{.Branding.LogoURL}}" alt="">{{end}}
<h1>Sign in to {{.AppName}}</h1>
{{if .Error}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Error}}</p></div>
{{end}}
<form method="post" action="/login">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="request" value="{{.RequestID}}">
<div class="field">
<label for="email">Email address</label>
<input id="email" name="email" type="email" inputmode="email" autocomplete="username"
 autocapitalize="none" spellcheck="false" required value="{{.Email}}"
 {{if .EmailError}}aria-invalid="true" aria-describedby="email-err"{{end}}
 {{if not .Error}}autofocus{{end}}>
{{if .EmailError}}<p class="err" id="email-err">{{.EmailError}}</p>{{end}}
</div>
<div class="field">
<label for="password">Password</label>
<input id="password" name="password" type="password" autocomplete="current-password" required
 {{if .PasswordError}}aria-invalid="true" aria-describedby="password-err"{{end}}>
{{if .PasswordError}}<p class="err" id="password-err">{{.PasswordError}}</p>{{end}}
</div>
<button type="submit">Sign in</button>
</form>
<p class="foot"><a href="{{.ForgotPath}}">Forgot your password?</a></p>
</div>
</main>
</body>
</html>
`))

// noticeTemplate is every page that is not the form: an expired request, a
// login page reached with no request at all, and the forgotten-password entry
// point.
//
// One template for all three because they are the same shape — a heading, an
// explanation, and no way onward — and because a page with no form has no
// tokens to get wrong.
var noticeTemplate = template.Must(template.New("notice").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>{{.Title}}</h1>
<p class="note">{{.Body}}</p>
{{if .BackPath}}<p class="foot"><a href="{{.BackPath}}">Back to sign in</a></p>{{end}}
</div>
</main>
</body>
</html>
`))

// ChallengePage is the second step (P3-03).
//
// It EMBEDS Page rather than copying its fields, so the stylesheet, the CSP,
// the branding and the CSRF token cannot drift between the two screens — the
// failure that would produce is a challenge page with a weaker policy than the
// page that asks for the password, which is the wrong way round.
type ChallengePage struct {
	Page

	// Offered are the factor kinds this user may answer with, already reduced
	// to what a person should read. Never a factor id and never a count of
	// anything: a page that said "you have 2 factors" would be answering a
	// question about the account to whoever reached it.
	Offered []OfferedFactor

	// Problem is the form-level message, one of the constants above.
	Problem string
}

// OfferedFactor is one kind of factor, as the page shows it.
type OfferedFactor struct {
	// Type is the value posted back — the mfa.Type string.
	Type string

	// Label and Hint are what a person reads.
	Label string
	Hint  string
}

// offeredLabels turns factor types into what the page renders.
//
// A type this does not recognise is DROPPED rather than shown with its raw
// name. A build that offered "webauthn" as a bare string before P3-05 has a
// verifier for it would be offering something nobody can complete, and a dead
// option on this page is a user who cannot sign in.
func offeredLabels(types []mfa.Type) []OfferedFactor {
	out := make([]OfferedFactor, 0, len(types))
	for _, t := range types {
		switch t {
		case mfa.TypeTOTP:
			out = append(out, OfferedFactor{
				Type:  string(mfa.TypeTOTP),
				Label: "Authenticator app",
				Hint:  "Enter the 6-digit code from your authenticator app.",
			})
		case mfa.TypeWebAuthn:
			// P3-05. Listed so the switch is exhaustive and the omission is
			// visible, not so it renders: WebAuthn needs script, and this page
			// has none. It will need its own step.
			continue
		}
	}
	return out
}

// challengeTemplate is the code form.
//
// The same shape as the login form and the same absences: no script, no event
// handler, no style attribute, and no value from the query string. The only
// interpolations are the application's registered name, the validated logo, the
// two opaque tokens, and the factor labels above — all of which are constants
// in this file.
//
// `autocomplete="one-time-code"` is what lets a phone offer the code from a
// notification. `inputmode="numeric"` gets the numeric keypad. Neither is
// decoration: a six-digit code typed on a full keyboard on a phone is the step
// people abandon.
var challengeTemplate = template.Must(template.New("challenge").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Two-step verification</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
{{if .Branding.LogoURL}}<img class="mark" src="{{.Branding.LogoURL}}" alt="">{{end}}
<h1>Two-step verification</h1>
{{if .Problem}}
<div class="alert" role="alert" tabindex="-1" autofocus><p>{{.Problem}}</p></div>
{{end}}
{{range $index, $factor := .Offered}}
<form method="post" action="/login/mfa">
<input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
<input type="hidden" name="request" value="{{$.RequestID}}">
<input type="hidden" name="factor" value="{{$factor.Type}}">
<div class="field">
<label for="code-{{$index}}">{{$factor.Label}}</label>
<p class="note">{{$factor.Hint}}</p>
<input id="code-{{$index}}" name="code" type="text" inputmode="numeric"
 autocomplete="one-time-code" autocapitalize="none" spellcheck="false" required
 {{if and (eq $index 0) (not $.Problem)}}autofocus{{end}}>
</div>
<button type="submit">Verify</button>
</form>
{{end}}
<p class="foot"><a href="{{.ForgotPath}}">Having trouble?</a></p>
</div>
</main>
</body>
</html>
`))

// Notice is a page with no form.
type Notice struct {
	Title    string
	Body     string
	BackPath string

	Style     template.CSS
	StyleHash string
}

// ContentSecurityPolicy for a notice: no images at all, since there is no
// branding on a page that could not resolve an organization.
func (n Notice) ContentSecurityPolicy() string {
	return strings.Join([]string{
		"default-src 'none'",
		"style-src '" + n.StyleHash + "'",
		"img-src 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
}

// render executes a template into a buffer.
//
// Into a buffer rather than straight to the ResponseWriter, so a template
// error cannot produce a half-written page with a 200 already committed.
func render(t *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("login: rendering: %w", err)
	}
	return buf.Bytes(), nil
}

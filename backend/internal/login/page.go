package login

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"strings"
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

	// MsgSessionProblem covers a missing or mismatched CSRF token. Usually a
	// tab left open overnight rather than an attack, and worded for the person
	// who will actually see it.
	MsgSessionProblem = "This page has been open for a while. Please try again."

	MsgEmailRequired    = "Enter your email address."
	MsgPasswordRequired = "Enter your password."
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
		// The form may only post back to this service. Without it, an injected
		// form action would exfiltrate the password to another origin — and
		// this is the one page where that is the whole prize.
		"form-action 'self'",
		// Clickjacking, in the modern spelling. X-Frame-Options is sent too:
		// they overlap, and the older header is the one some proxies and
		// embedded browsers still act on.
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; ")
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

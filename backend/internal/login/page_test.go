package login

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

func renderedPage(t *testing.T, page Page) string {
	t.Helper()

	if page.Style == "" {
		page.Style, page.StyleHash = renderStyle(page.Branding.AccentColor)
	}
	body, err := render(pageTemplate, page)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return string(body)
}

func samplePage() Page {
	return Page{
		AppName:    "Billing",
		Branding:   DefaultBranding,
		CSRFToken:  "csrf-token-value",
		RequestID:  "pending-request-id",
		ForgotPath: ForgotPath,
	}
}

// DoD item 1: the page functions without client-side JavaScript. Not
// "degrades gracefully" — there is none, which is what makes
// `default-src 'none'` achievable rather than aspirational.
func TestThePageContainsNoScript(t *testing.T) {
	html := renderedPage(t, samplePage())

	if strings.Contains(strings.ToLower(html), "<script") {
		t.Error("the login page contains a script element")
	}
	if strings.Contains(strings.ToLower(html), "javascript:") {
		t.Error("the login page contains a javascript: URL")
	}

	// Inline event handlers are script by another name, and they are the ones
	// a CSP without 'unsafe-inline' would block at runtime rather than here.
	handler := regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	if match := handler.FindString(html); match != "" {
		t.Errorf("the page carries an inline event handler: %q", strings.TrimSpace(match))
	}

	// A style attribute would need 'unsafe-inline' in style-src, which would
	// undo the hash. The stylesheet is one hashed block or it is nothing.
	if regexp.MustCompile(`(?i)\sstyle\s*=`).MatchString(html) {
		t.Error("the page carries a style attribute, which the hashed CSP cannot permit")
	}
}

// The hash in the policy must be the hash of the stylesheet actually served.
// If these drift, the browser blocks the CSS and the page renders unstyled —
// which is a visible failure, but only if somebody looks.
func TestTheStyleHashMatchesTheStyleServed(t *testing.T) {
	page := samplePage()
	page.Style, page.StyleHash = renderStyle(page.Branding.AccentColor)

	sum := sha256.Sum256([]byte(page.Style))
	want := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])

	if page.StyleHash != want {
		t.Fatalf("StyleHash = %q, want %q", page.StyleHash, want)
	}
	if !strings.Contains(page.ContentSecurityPolicy(), "style-src '"+want+"'") {
		t.Errorf("the policy does not whitelist the served stylesheet:\n%s",
			page.ContentSecurityPolicy())
	}
}

// A nonce would change every response; a hash changes only when the styling
// does. That is what lets two failed logins be byte-identical responses.
func TestTheStyleHashIsStableAcrossRenders(t *testing.T) {
	_, first := renderStyle(DefaultAccent)
	_, second := renderStyle(DefaultAccent)

	if first != second {
		t.Error("the style hash differs between two renders of the same branding; " +
			"the uniform-response requirement cannot hold")
	}

	_, branded := renderStyle("#046c4e")
	if branded == first {
		t.Error("a different accent produced the same hash, so the policy would " +
			"block the stylesheet it is meant to permit")
	}
}

func TestContentSecurityPolicy(t *testing.T) {
	page := samplePage()
	page.Style, page.StyleHash = renderStyle(DefaultAccent)
	policy := page.ContentSecurityPolicy()

	for _, directive := range []string{
		"default-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	} {
		if !strings.Contains(policy, directive) {
			t.Errorf("the policy is missing %q:\n%s", directive, policy)
		}
	}

	// script-src is absent because default-src 'none' already covers it. What
	// must never appear is a permission for one.
	if strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "unsafe-eval") {
		t.Errorf("the policy permits inline or eval:\n%s", policy)
	}
}

// An organization with no logo gets a page that cannot emit a request
// anywhere. That is not a detail: img-src is the only network directive the
// page would otherwise have.
func TestImagesArePermittedOnlyWhenThereIsALogo(t *testing.T) {
	bare := samplePage()
	bare.Style, bare.StyleHash = renderStyle(DefaultAccent)
	if !strings.Contains(bare.ContentSecurityPolicy(), "img-src 'none'") {
		t.Errorf("an unbranded page permits images:\n%s", bare.ContentSecurityPolicy())
	}

	branded := samplePage()
	branded.Branding.LogoURL = "https://cdn.example/logo.svg"
	branded.Style, branded.StyleHash = renderStyle(DefaultAccent)
	if !strings.Contains(branded.ContentSecurityPolicy(), "img-src https:") {
		t.Errorf("a branded page cannot load its logo:\n%s", branded.ContentSecurityPolicy())
	}
}

// Abuse case A-7. Nothing from the query string is rendered, so the strongest
// version of the control is that there is no field to carry one — but the
// values that DO get rendered still go through html/template, and this is the
// test that says so.
func TestRenderedValuesAreEscaped(t *testing.T) {
	page := samplePage()
	page.AppName = `<script>alert(1)</script>`
	page.Email = `"><script>alert(2)</script>`
	page.Error = `<img src=x onerror=alert(3)>`

	html := renderedPage(t, page)

	// The escaped text still CONTAINS "onerror=" as characters, so the
	// assertion has to be about the markup rather than about the substring.
	// An earlier version of this test checked for the substring and failed on
	// a page that was escaping perfectly well.
	if strings.Contains(html, "<script>") {
		t.Error("a rendered value produced a live script element")
	}
	if strings.Contains(html, "<img src=x") {
		t.Error("a rendered value produced a live element with an event handler")
	}
	for _, want := range []string{"&lt;script&gt;", "&lt;img src=x onerror="} {
		if !strings.Contains(html, want) {
			t.Errorf("%q is absent, so the value was not rendered at all and this "+
				"test proves nothing", want)
		}
	}
}

// The password is never re-rendered, on any path. An HTML page that echoes it
// is a password in the browser cache, in the back button, and in any proxy
// that ignores no-store.
func TestThePasswordIsNeverRendered(t *testing.T) {
	page := samplePage()
	page.Email = "someone@example.test"
	page.Error = MsgCredentials

	html := renderedPage(t, page)

	if strings.Contains(html, `name="password"`) && strings.Contains(html, `value=`) {
		// The field exists; what must not exist is a value on it.
		field := html[strings.Index(html, `id="password"`):]
		field = field[:strings.Index(field, ">")]
		if strings.Contains(field, "value=") {
			t.Errorf("the password input carries a value attribute: %s", field)
		}
	}
	if !strings.Contains(html, `value="someone@example.test"`) {
		t.Error("the email was not re-rendered, so this test would pass on a page " +
			"that renders nothing at all")
	}
}

// UI-UX/13: every input has a visible, associated label, and an error is tied
// to its field. Without JavaScript, focus is moved by the autofocus attribute
// on the error summary — which is why the summary carries tabindex.
func TestFormAccessibility(t *testing.T) {
	page := samplePage()
	page.EmailError = MsgEmailRequired
	page.Error = "Please check the fields below."

	html := renderedPage(t, page)

	for _, want := range []string{
		`<label for="email">`,
		`<label for="password">`,
		`id="email"`,
		`id="password"`,
		`aria-describedby="email-err"`,
		`aria-invalid="true"`,
		`role="alert"`,
		`tabindex="-1"`,
		`autofocus`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the rendered form is missing %s", want)
		}
	}

	if strings.Count(html, "autofocus") != 1 {
		t.Errorf("autofocus appears %d times; more than one is undefined behaviour "+
			"and none leaves a keyboard user hunting", strings.Count(html, "autofocus"))
	}
	if !strings.Contains(html, `autocomplete="current-password"`) ||
		!strings.Contains(html, `autocomplete="username"`) {
		t.Error("the autocomplete attributes a password manager needs are absent")
	}
}

// With no error the focus lands in the first field, which is where somebody
// arriving to type a password wants it.
func TestFocusStartsInTheFirstFieldWhenThereIsNoError(t *testing.T) {
	html := renderedPage(t, samplePage())

	email := html[strings.Index(html, `id="email"`):]
	email = email[:strings.Index(email, ">")]
	if !strings.Contains(email, "autofocus") {
		t.Error("the email field is not focused on a clean render")
	}
}

// FR-9's entry point exists and points somewhere real.
func TestTheForgottenPasswordLinkIsPresent(t *testing.T) {
	html := renderedPage(t, samplePage())

	if !strings.Contains(html, `href="`+ForgotPath+`"`) {
		t.Error("there is no forgotten-password entry point")
	}
}

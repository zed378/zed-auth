// Command saml runs the P4-07/P4-08 acceptance checks against a deployment.
//
// It is a SAML service provider, badly behaved on purpose. It sends requests
// that name their own Assertion Consumer Service URL, replays request ids,
// asks for an assertion it never requested, and presents an entity ID nobody
// registered — and then asserts that every one of those was refused.
//
// # Why it verifies the signature with goxmldsig directly
//
// The service verifies signatures through `internal/saml.Verify`, which picks
// the element to validate and fails closed. A check that called the same
// function would agree with it by construction: a `Verify` that validated the
// wrong element would still say yes here, and the one property this protocol
// rests on would go unchecked by the only test that runs against a real
// deployment.
//
// So this builds its own validation context from the certificate published in
// `/saml/metadata` and validates the assertion it was actually delivered. It
// shares the underlying library with the service and nothing above it.
//
// Seeding and cleanup are the wrapper's job; see scripts/acceptance-saml.sh.
package main

import (
	"bytes"
	"compress/flate"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// --- transport ---------------------------------------------------------------

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
	status      int
	body        string
	location    string
	contentType string
}

func (c *client) send(method, path, contentType string, body io.Reader) result {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		die("building a request: %v", err)
	}
	req.Host = c.host
	req.Header.Set("X-Forwarded-Proto", "https")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
	return result{
		status: resp.StatusCode, body: string(raw),
		location: resp.Header.Get("Location"), contentType: resp.Header.Get("Content-Type"),
	}
}

func (c *client) get(path string) result { return c.send("GET", path, "", nil) }

func (c *client) form(path string, v url.Values) result {
	return c.send("POST", path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
}

// --- output ------------------------------------------------------------------

var passed, failed int

func pass(format string, a ...any) {
	passed++
	fmt.Printf("  \033[32mPASS\033[0m %s\n", fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	failed++
	fmt.Printf("  \033[31mFAIL\033[0m %s\n", fmt.Sprintf(format, a...))
}

func note(format string, a ...any) {
	fmt.Printf("       \033[2m%s\033[0m\n", fmt.Sprintf(format, a...))
}

// abandoned is what die raises: this section cannot continue.
type abandoned struct{ reason string }

// die abandons the CURRENT section, not the run.
//
// It used to call os.Exit, and that cost three deployments to learn from. The
// first SP-initiated failure aborted every section after it, so each run
// surfaced exactly one defect and the next was only visible once the first was
// fixed and redeployed. Three bugs, three round trips.
//
// The sections are independent — the POST binding does not depend on the
// Redirect binding having worked, and IdP-initiated depends on neither — so a
// section that cannot continue should say so and let the others run. The exit
// code still reflects any failure; only the reach of one changes.
func die(format string, a ...any) {
	panic(abandoned{reason: fmt.Sprintf(format, a...)})
}

// section runs one group of checks, surviving a die inside it.
//
// A panic that is not an `abandoned` is a bug in this program rather than a
// finding about the service, so it is re-raised rather than reported as one.
func section(title string, checks func()) {
	fmt.Printf("\n\033[1;36m%s\033[0m\n", title)
	defer func() {
		switch p := recover().(type) {
		case nil:
		case abandoned:
			failed++
			fmt.Printf("  \033[31mFAIL\033[0m this section could not continue\n")
			for _, line := range strings.Split(p.reason, "\n") {
				note("%s", line)
			}
		default:
			panic(p)
		}
	}()
	checks()
}

func check(ok bool, subject string, detail ...any) {
	if ok {
		pass("%s", subject)
		return
	}
	fail("%s", subject)
	for _, d := range detail {
		note("%v", d)
	}
}

// --- what a service provider sends -------------------------------------------

// attackerACS is the address every request below asks to be answered at.
//
// It is somewhere this service never approved. Every check that follows a
// successful login also asserts the assertion went to the REGISTERED address
// instead, which is the only way to tell "the request was ignored" from "the
// request happened to agree".
const attackerACS = "https://attacker.invalid/steal"

func authnRequest(id, issuer, destination string) string {
	return fmt.Sprintf(`<samlp:AuthnRequest`+
		` xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"`+
		` xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`+
		` ID="%s" Version="2.0" IssueInstant="%s"`+
		` Destination="%s"`+
		` AssertionConsumerServiceURL="%s"`+
		` ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST">`+
		`<saml:Issuer>%s</saml:Issuer>`+
		`</samlp:AuthnRequest>`,
		id, time.Now().UTC().Format(time.RFC3339), destination, attackerACS, issuer)
}

// deflateBase64 encodes a request for the HTTP-Redirect binding.
func deflateBase64(doc string) string {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		die("compressing the request: %v", err)
	}
	if _, err := w.Write([]byte(doc)); err != nil {
		die("compressing the request: %v", err)
	}
	if err := w.Close(); err != nil {
		die("compressing the request: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func requestID(prefix string) string {
	return fmt.Sprintf("_%s%d", prefix, time.Now().UnixNano())
}

// --- what comes back ----------------------------------------------------------

var (
	reAction   = regexp.MustCompile(`(?s)<form[^>]*action="([^"]+)"`)
	reResponse = regexp.MustCompile(`name="SAMLResponse"\s+value="([^"]*)"`)
	reRelay    = regexp.MustCompile(`name="RelayState"\s+value="([^"]*)"`)
	reCSRF     = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)
	reRequest  = regexp.MustCompile(`name="request"\s+value="([^"]+)"`)
)

type delivered struct {
	action     string
	relayState string
	raw        []byte
	doc        *etree.Document
}

// readForm pulls the self-posting form apart.
//
// The values are HTML-escaped in the page — `html/template` is what makes that
// form safe — so they are unescaped before decoding. A `+` arriving as `&#43;`
// and being base64-decoded as it stands is a corrupted assertion that presents
// as a signature failure, which is a confusing way to learn this.
func readForm(body string) (delivered, error) {
	action := reAction.FindStringSubmatch(body)
	response := reResponse.FindStringSubmatch(body)
	if action == nil || response == nil {
		return delivered{}, fmt.Errorf("no self-posting form in the response")
	}
	raw, err := base64.StdEncoding.DecodeString(html.UnescapeString(response[1]))
	if err != nil {
		return delivered{}, fmt.Errorf("decoding SAMLResponse: %w", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return delivered{}, fmt.Errorf("parsing SAMLResponse: %w", err)
	}
	out := delivered{action: html.UnescapeString(action[1]), raw: raw, doc: doc}
	if relay := reRelay.FindStringSubmatch(body); relay != nil {
		out.relayState = html.UnescapeString(relay[1])
	}
	return out, nil
}

func (d delivered) attr(path, name string) string {
	el := d.doc.FindElement(path)
	if el == nil {
		return ""
	}
	return el.SelectAttrValue(name, "")
}

func (d delivered) text(path string) string {
	el := d.doc.FindElement(path)
	if el == nil {
		return ""
	}
	return strings.TrimSpace(el.Text())
}

// verifyAssertion validates the delivered assertion against a certificate.
//
// The certificate comes from `/saml/metadata`, which is where a service
// provider would get it, and the element validated is the Assertion — the
// element a consumer acts on. Validating the envelope instead is the signature
// wrapping bug this check exists to notice.
func verifyAssertion(d delivered, cert *x509.Certificate) error {
	assertion := d.doc.FindElement("//Assertion")
	if assertion == nil {
		return fmt.Errorf("the response carries no Assertion")
	}
	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{
		Roots: []*x509.Certificate{cert},
	})
	if _, err := ctx.Validate(assertion); err != nil {
		return err
	}
	return nil
}

// --- signing in ---------------------------------------------------------------

// signIn drives the hosted login page for a SAML request and returns whatever
// the service answers with — which, for SAML, is the self-posting form itself
// rather than a redirect.
func (c *client) signIn(loginPath, email, password string) result {
	page := c.get(loginPath)
	if page.status != http.StatusOK {
		die("the login page answered %d: %s", page.status, truncate(page.body))
	}
	csrf := reCSRF.FindStringSubmatch(page.body)
	req := reRequest.FindStringSubmatch(page.body)
	if csrf == nil || req == nil {
		die("the login page has no csrf_token or request field")
	}
	return c.form("/login", url.Values{
		"csrf_token": {csrf[1]}, "request": {req[1]},
		"email": {email}, "password": {password},
	})
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// --- the checks ---------------------------------------------------------------

func main() {
	// Shared across sections, because the sections are independent of each
	// other's SUCCESS but not of each other's discoveries: every assertion is
	// verified against the certificate the metadata publishes, and the
	// IdP-initiated section compares its NameID against the SP-initiated one.
	//
	// A section that could not run leaves these at their zero values, and the
	// sections that need them say so rather than reporting a confusing failure.
	var (
		cert       *x509.Certificate
		c          *client
		redirectID string
		nameID     string
	)

	// Echoed back unchanged by every flow that carries it. A constant rather
	// than something the first section sets, so a later section can still
	// check it when an earlier one could not run.
	const relay = "/portal/after-login"

	var (
		base        = getenv("ACCEPT_BASE", "http://127.0.0.1:10800")
		host        = must("ACCEPT_HOST")
		issuer      = must("ACCEPT_ISSUER")
		entityID    = must("SP_ENTITY_ID")
		acsURL      = must("SP_ACS_URL")
		idpEntityID = must("SP_IDP_ENTITY_ID")
		idpACSURL   = must("SP_IDP_ACS_URL")
		email       = must("SP_USER_EMAIL")
		password    = must("SP_USER_PASSWORD")
	)

	// --- metadata -------------------------------------------------------------

	section("Metadata — what a service provider configures itself from", func() {
		meta := newClient(base, host).get("/saml/metadata")
		if meta.status != http.StatusOK {
			die("/saml/metadata answered %d: %s", meta.status, truncate(meta.body))
		}
		check(strings.HasPrefix(meta.contentType, "application/samlmetadata+xml"),
			"metadata is served as application/samlmetadata+xml", meta.contentType)

		md := etree.NewDocument()
		if err := md.ReadFromString(meta.body); err != nil {
			die("the metadata is not XML: %v", err)
		}

		check(md.FindElement("//EntityDescriptor") != nil &&
			md.FindElement("//EntityDescriptor").SelectAttrValue("entityID", "") == issuer,
			"the metadata names this instance as the entity", issuer)

		certEl := md.FindElement("//X509Certificate")
		if certEl == nil {
			die("the metadata publishes no certificate; a service provider has nothing to pin")
		}
		cert = parseCertificate(strings.TrimSpace(certEl.Text()))
		check(cert != nil, "the published certificate parses as X.509")
		check(cert != nil && time.Now().Before(cert.NotAfter),
			"the published certificate has not expired", certificateWindow(cert))

		bindings := map[string]bool{}
		for _, sso := range md.FindElements("//SingleSignOnService") {
			bindings[sso.SelectAttrValue("Binding", "")] = true
		}
		check(bindings["urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"],
			"HTTP-Redirect is advertised")
		check(bindings["urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"],
			"HTTP-POST is advertised")

		// The absence is the decision (P4-08). Front-channel Single Logout leaves a
		// partial logout — a user told they are signed out of applications they are
		// not — so the endpoint is not advertised and a service provider cannot
		// build a logout button on it.
		check(len(md.FindElements("//SingleLogoutService")) == 0,
			"no SingleLogoutService is advertised, so nothing is built on a partial logout")

		formats := []string{}
		for _, f := range md.FindElements("//NameIDFormat") {
			formats = append(formats, strings.TrimSpace(f.Text()))
		}
		check(contains(formats, "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"),
			"the persistent NameID format is advertised")
		check(!containsSubstring(formats, "emailAddress"),
			"no email NameID format is advertised", strings.Join(formats, ", "))

		// --- single logout --------------------------------------------------------
	})

	section("Single Logout — refused in the protocol's own vocabulary", func() {
		slo := newClient(base, host).get("/saml/slo")
		check(slo.status != http.StatusNotFound,
			"/saml/slo is not a 404 — a 404 reads as a misconfiguration to retry", slo.status)
		check(strings.Contains(slo.body, "RequestDenied"),
			"/saml/slo answers RequestDenied, which reads as a decision", truncate(slo.body))

		// --- SP-initiated, HTTP-Redirect -----------------------------------------
	})

	section("SP-initiated on HTTP-Redirect", func() {
		if cert == nil {
			die("the metadata section could not publish a certificate, so an assertion here could not be verified against anything")
		}

		redirectID = requestID("redir")
		c = newClient(base, host)

		started := c.get("/saml/sso?" + url.Values{
			"SAMLRequest": {deflateBase64(authnRequest(redirectID, entityID, issuer+"/saml/sso"))},
			"RelayState":  {relay},
		}.Encode())

		check(started.status == http.StatusSeeOther || started.status == http.StatusFound,
			"an AuthnRequest with no session is sent to the hosted login", started.status)
		if !strings.Contains(started.location, "/login?request=saml") {
			die("the login URL does not carry a SAML request: %q", started.location)
		}
		pass("the login URL namespaces the request as SAML")

		answered := c.signIn(loginPath(started.location, host), email, password)
		if answered.status != http.StatusOK {
			die("signing in did not produce an assertion: %d %s", answered.status, truncate(answered.body))
		}

		got, err := readForm(answered.body)
		if err != nil {
			die("reading the delivered form: %v — %s", err, truncate(answered.body))
		}

		check(got.action == acsURL,
			"the assertion is posted to the REGISTERED ACS URL, not the one the request named",
			fmt.Sprintf("posted to %s; the request asked for %s", got.action, attackerACS))
		check(got.action != attackerACS,
			"the request's own AssertionConsumerServiceURL was ignored")

		if err := verifyAssertion(got, cert); err != nil {
			fail("the assertion verifies against the certificate in metadata")
			note("%v", err)
		} else {
			pass("the assertion verifies against the certificate in metadata")
		}

		check(got.attr("//Response", "InResponseTo") == redirectID,
			"the response answers the request it was sent",
			got.attr("//Response", "InResponseTo"))
		check(got.attr("//Response", "Destination") == acsURL,
			"the response is addressed to the registered ACS URL")
		check(got.relayState == relay, "RelayState came back unchanged", got.relayState)

		nameID = got.text("//NameID")
		check(nameID != "" && nameID != email,
			"the NameID is not the user's email address", nameID)
		check(got.attr("//NameID", "Format") == "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
			"the NameID is persistent", got.attr("//NameID", "Format"))
		check(got.text("//Audience") == entityID,
			"the assertion is restricted to this service provider", got.text("//Audience"))

		// --- one answer per request -----------------------------------------------
	})

	section("One AuthnRequest, one answer", func() {
		if c == nil || redirectID == "" {
			die("no SP-initiated login completed, so there is nothing to repeat")
		}

		replayed := c.get("/saml/sso?" + url.Values{
			"SAMLRequest": {deflateBase64(authnRequest(redirectID, entityID, issuer+"/saml/sso"))},
			"RelayState":  {relay},
		}.Encode())

		// The session is live now, so this takes the fast path — which records no
		// pending row and answers immediately. Single-use is a property of the
		// login path, not of this one, and the record says why: replaying against
		// a live session needs the cookie that would have let you start a fresh
		// sign-on anyway, and an unsigned AuthnRequest can be forged outright, so
		// bounding replays of one id bounds nothing. The correlation defence is
		// the service provider checking InResponseTo, where it belongs.
		//
		// So the property worth asserting here is narrower and still real: a
		// second request never produces an assertion addressed anywhere but the
		// registration, and the user is the same subject both times.
		if replayed.status == http.StatusOK {
			again, err := readForm(replayed.body)
			check(err == nil && again.action == acsURL,
				"a repeated request is still answered only at the registered ACS URL")
			check(err == nil && again.text("//NameID") == nameID,
				"the same user is the same subject to this service provider across logins",
				"a NameID that changes per login is a new account at the service provider")
		} else {
			pass("a repeated request id is refused outright (%d)", replayed.status)
		}

		// --- SP-initiated, HTTP-POST ----------------------------------------------
	})

	section("SP-initiated on HTTP-POST", func() {
		if cert == nil {
			die("the metadata section could not publish a certificate, so an assertion here could not be verified against anything")
		}

		postID := requestID("post")
		p := newClient(base, host)
		postStarted := p.form("/saml/sso", url.Values{
			"SAMLRequest": {base64.StdEncoding.EncodeToString(
				[]byte(authnRequest(postID, entityID, issuer+"/saml/sso")))},
			"RelayState": {relay},
		})
		check(postStarted.status == http.StatusSeeOther || postStarted.status == http.StatusFound,
			"an AuthnRequest on HTTP-POST is sent to the hosted login", postStarted.status)

		postAnswered := p.signIn(loginPath(postStarted.location, host), email, password)
		postGot, err := readForm(postAnswered.body)
		if err != nil {
			die("HTTP-POST did not produce an assertion: %v — %s", err, truncate(postAnswered.body))
		}
		if err := verifyAssertion(postGot, cert); err != nil {
			fail("the HTTP-POST assertion verifies against the published certificate")
			note("%v", err)
		} else {
			pass("the HTTP-POST assertion verifies against the published certificate")
		}
		check(postGot.attr("//Response", "InResponseTo") == postID,
			"the HTTP-POST response answers its own request")
		check(postGot.action == acsURL,
			"HTTP-POST is held to the same registered ACS URL")

		// --- an entity nobody registered ------------------------------------------
	})

	section("An entity ID nobody registered", func() {
		stranger := newClient(base, host).get("/saml/sso?" + url.Values{
			"SAMLRequest": {deflateBase64(authnRequest(
				requestID("strange"), "https://stranger.invalid/sp", issuer+"/saml/sso"))},
		}.Encode())
		check(stranger.status >= 400,
			"an unregistered service provider is refused", stranger.status)
		check(!strings.Contains(stranger.body, "SAMLResponse"),
			"nothing is delivered to an unregistered service provider — there is no approved address to send it to")

		// --- IdP-initiated --------------------------------------------------------
	})

	section("IdP-initiated, which is off until somebody turns it on", func() {
		if cert == nil {
			die("the metadata section could not publish a certificate, so an assertion here could not be verified against anything")
		}

		// Two registrations, identical but for `allow_idp_initiated`, exercised in
		// the same run against the same key and the same user. A single
		// registration toggled between passes would show the same two outcomes and
		// prove less: a refusal followed by a success is also what a fixed
		// unrelated problem looks like.

		i := newClient(base, host)
		refused := i.get("/saml/init?" + url.Values{"entity_id": {entityID}}.Encode())
		check(refused.status >= 400,
			"a registration that has not opted in is refused", refused.status)
		check(!strings.Contains(refused.body, "SAMLResponse"),
			"not even a SAML failure is delivered to a service provider that did not ask for the capability")

		unsolicited := i.get("/saml/init?" + url.Values{
			"entity_id": {idpEntityID}, "RelayState": {relay},
		}.Encode())
		if unsolicited.status == http.StatusSeeOther || unsolicited.status == http.StatusFound {
			unsolicited = i.signIn(loginPath(unsolicited.location, host), email, password)
		}

		idp, err := readForm(unsolicited.body)
		if err != nil {
			fail("an opted-in service provider receives an unsolicited assertion")
			note("%v — %s", err, truncate(unsolicited.body))
		} else {
			pass("an opted-in service provider receives an unsolicited assertion")
			pass("the opt-in is the only difference between the two registrations")

			if err := verifyAssertion(idp, cert); err != nil {
				fail("the unsolicited assertion verifies against the published certificate")
				note("%v", err)
			} else {
				pass("the unsolicited assertion verifies against the published certificate")
			}

			check(!strings.Contains(string(idp.raw), "InResponseTo"),
				"the unsolicited assertion claims to answer no request",
				"an InResponseTo here invents a correlation the service provider would rely on")
			check(idp.action == idpACSURL,
				"the unsolicited assertion goes to that registration's own ACS URL", idp.action)
			check(idp.text("//Audience") == idpEntityID,
				"the unsolicited assertion is restricted to the service provider that opted in",
				idp.text("//Audience"))
			check(idp.text("//NameID") != nameID,
				"the same user is a DIFFERENT subject to a different service provider",
				"a NameID shared across service providers is a correlation handle they can join on")
		}
	})

	// --- verdict ---------------------------------------------------------------

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// --- helpers ------------------------------------------------------------------

// loginPath turns the Location header into a path this client can request.
//
// The service builds absolute URLs from its issuer, and this runs against the
// loopback port behind the proxy, so the host has to come off.
func loginPath(location, host string) string {
	if location == "" {
		die("no Location header to follow")
	}
	if u, err := url.Parse(location); err == nil && u.Host != "" {
		if u.Host != host {
			die("the login URL points somewhere unexpected: %q", location)
		}
		if u.RawQuery != "" {
			return u.Path + "?" + u.RawQuery
		}
		return u.Path
	}
	return location
}

func parseCertificate(b64 string) *x509.Certificate {
	// Metadata carries the DER base64-encoded without PEM armour, and it is
	// wrapped across lines. Both have to come off before decoding.
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			return -1
		}
		return r
	}, b64)
	der, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		// Some producers do publish PEM. Accept it rather than failing on a
		// difference that changes nothing.
		if block, _ := pem.Decode([]byte(b64)); block != nil {
			der = block.Bytes
		} else {
			return nil
		}
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil
	}
	return cert
}

func certificateWindow(cert *x509.Certificate) string {
	if cert == nil {
		return "no certificate"
	}
	return fmt.Sprintf("valid %s … %s",
		cert.NotBefore.Format(time.RFC3339), cert.NotAfter.Format(time.RFC3339))
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func containsSubstring(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		die("%s is not set", k)
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

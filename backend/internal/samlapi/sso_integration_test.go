//go:build integration

// A full SP-initiated login, through the real endpoints (P4-08, docs/PLAN/17
// Phase 4).
//
// The criterion is "a SAML-only legacy application completes a full
// SP-initiated login", and this is that sentence as a test: an AuthnRequest
// arrives on a binding, a session answers it, and what comes back is an
// assertion the service provider can verify — checked by verifying it.
//
// The assertions worth having here are the refusals. A flow that works is one
// screenshot; a flow that refuses the right things is the product.
package samlapi

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"html"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const idpIssuer = "https://auth.example.test"

// sessionTokenLength is what session.ValidToken requires. Stated here rather
// than exported from that package: a test that needed it to change would be a
// test noticing a deliberate change to the token format.
const sessionTokenLength = 43

// staticKeys hands out one SAML key, or refuses.
type staticKeys struct{ key *saml.SigningKey }

func (s staticKeys) SAML(context.Context) (*saml.SigningKey, error) {
	if s.key == nil {
		return nil, ErrNotConfigured
	}
	return s.key, nil
}

// staticSessions answers with one session, or none.
type staticSessions struct {
	found session.Session
	ok    bool
}

func (s staticSessions) Lookup(context.Context, string, session.Policy, time.Time) (session.Session, error) {
	if !s.ok {
		return session.Session{}, session.ErrNotFound
	}
	return s.found, nil
}

type ssoFixture struct {
	*subjectFixture
	handler *Handler
	key     *saml.SigningKey
	cert    *x509.Certificate
}

func setupSSO(t *testing.T, release []string, methods []string, signedIn bool) *ssoFixture {
	t.Helper()
	f := setupSubjects(t, release)

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	certPEM, err := saml.SelfSignedCertificate(pair, idpIssuer, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("certifying: %v", err)
	}
	key, err := saml.NewSigningKey(pair, certPEM)
	if err != nil {
		t.Fatalf("adapting the key: %v", err)
	}
	cert, err := key.Certificate()
	if err != nil {
		t.Fatalf("reading the certificate: %v", err)
	}

	return &ssoFixture{
		subjectFixture: f,
		key:            key,
		cert:           cert,
		handler: &Handler{
			Issuer:    idpIssuer,
			Endpoints: saml.Endpoints{SSORedirect: idpIssuer + "/saml/sso", SSOPost: idpIssuer + "/saml/sso"},
			LoginPath: "/login",
			Policy:    session.DefaultPolicy,
			DB:        f.db,
			Keys:      staticKeys{key: key},
			Providers: saml.NewProviderStore(),
			Sessions: staticSessions{
				ok: signedIn,
				found: session.Session{
					ID: "sess-1", UserID: f.userID, OrgID: f.orgID,
					AuthMethods: methods, CreatedAt: time.Now().Add(-5 * time.Minute),
				},
			},
			Subjects: NewSubjectStore(),
			Requests: saml.NewRequests(),
		},
	}
}

// authnRequestFor builds a request from the service provider.
func authnRequestFor(id, issuer, requestedContext string) string {
	context := ""
	if requestedContext != "" {
		context = `<RequestedAuthnContext><AuthnContextClassRef>` +
			requestedContext + `</AuthnContextClassRef></RequestedAuthnContext>`
	}
	return `<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="` + id + `" Version="2.0"` +
		` IssueInstant="2026-09-21T00:00:00Z"` +
		// An ACS URL of the attacker's choosing, present in every request here
		// so that every test also asserts it is ignored.
		` AssertionConsumerServiceURL="https://attacker.example.test/acs">` +
		`<Issuer>` + issuer + `</Issuer>` + context + `</AuthnRequest>`
}

func deflateEncode(t *testing.T, raw string) string {
	t.Helper()
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	_ = w.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// withSession attaches a well-formed session cookie.
//
// The handler reads the token from the request before it asks the session
// store anything, so a test with no cookie exercises the signed-OUT path
// however the store is stubbed — which is correct behaviour and cost a
// confusing round of failures to notice.
func withSession(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{
		Name:  session.CookieName,
		Value: strings.Repeat("a", sessionTokenLength),
	})
	return r
}

func (f *ssoFixture) redirect(t *testing.T, encoded, relayState string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"SAMLRequest": {encoded}}
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	r := withSession(httptest.NewRequest(http.MethodGet, "/saml/sso?"+q.Encode(), nil))
	w := httptest.NewRecorder()
	f.handler.SSORedirect(w, r)
	return w
}

func (f *ssoFixture) post(t *testing.T, encoded, relayState string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"SAMLRequest": {encoded}}
	if relayState != "" {
		form.Set("RelayState", relayState)
	}
	r := withSession(httptest.NewRequest(http.MethodPost, "/saml/sso", strings.NewReader(form.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.handler.SSOPost(w, r)
	return w
}

var reSAMLResponse = regexp.MustCompile(`name="SAMLResponse" value="([^"]*)"`)

// assertionFrom pulls the assertion out of a delivered form and verifies it.
func (f *ssoFixture) assertionFrom(t *testing.T, body string) saml.Verified {
	t.Helper()
	field := reSAMLResponse.FindStringSubmatch(body)
	if field == nil {
		t.Fatalf("no SAMLResponse in the delivered form: %s", truncate(body))
	}
	raw, err := base64.StdEncoding.DecodeString(html.UnescapeString(field[1]))
	if err != nil {
		t.Fatalf("the SAMLResponse is not base64: %v", err)
	}
	v, err := saml.Verify(raw, "Assertion", []*x509.Certificate{f.cert})
	if err != nil {
		t.Fatalf("the delivered assertion did not verify: %v", err)
	}
	return v
}

func truncate(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// docs/PLAN/17's Phase 4 criterion, on both bindings.
func TestASPInitiatedLoginCompletesOnBothBindings(t *testing.T) {
	request := authnRequestFor("_req1", "https://sp.example.test", "")

	for name, deliver := range map[string]func(*ssoFixture) *httptest.ResponseRecorder{
		"HTTP-Redirect": func(f *ssoFixture) *httptest.ResponseRecorder {
			return f.redirect(t, deflateEncode(t, request), "/dashboard")
		},
		"HTTP-POST": func(f *ssoFixture) *httptest.ResponseRecorder {
			return f.post(t, base64.StdEncoding.EncodeToString([]byte(request)), "/dashboard")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
			w := deliver(f)

			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, truncate(w.Body.String()))
			}
			body := w.Body.String()

			// It goes to the REGISTERED ACS URL, not the one the request named.
			if !strings.Contains(body, "https://sp.example.test/acs") {
				t.Error("the form does not post to the registered ACS URL")
			}
			if strings.Contains(body, "attacker.example.test") {
				t.Error("the form posts to the ACS URL the REQUEST named — an open redirect with a signature on it")
			}

			v := f.assertionFrom(t, body)
			if err := saml.CheckConditions(v, "https://sp.example.test", time.Now()); err != nil {
				t.Errorf("the assertion fails its own conditions: %v", err)
			}

			// The subject is the persistent identifier, and the released
			// attribute is the registered one.
			nameID := v.Element().FindElement("./Subject/NameID")
			if nameID == nil || strings.Contains(nameID.Text(), "@") {
				t.Errorf("NameID is %v, want a persistent identifier", nameID)
			}
			if !strings.Contains(string(serialiseElement(t, v)), "budi@company.test") {
				t.Error("the registered attribute was not released")
			}

			// RelayState survives unchanged.
			if !strings.Contains(body, `name="RelayState" value="/dashboard"`) {
				t.Error("RelayState was not echoed")
			}
		})
	}
}

func serialiseElement(t *testing.T, v saml.Verified) []byte {
	t.Helper()
	raw, err := saml.Serialise(v.Element())
	if err != nil {
		t.Fatalf("serialising: %v", err)
	}
	return raw
}

// No session: the browser goes to the hosted login, and the request is stored
// so it can be answered afterwards.
func TestWithoutASessionTheBrowserGoesToTheLogin(t *testing.T) {
	f := setupSSO(t, []string{"email"}, nil, false)

	w := f.redirect(t, deflateEncode(t, authnRequestFor("_req2", "https://sp.example.test", "")), "/dash")

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303: %s", w.Code, truncate(w.Body.String()))
	}
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") {
		t.Fatalf("redirected to %q, want the hosted login", location)
	}

	// The namespaced id and nothing else — no document, no ACS URL, nothing a
	// user could edit in the address bar.
	u, _ := url.Parse(location)
	id := u.Query().Get("request")
	if !IsSAMLRequest(id) {
		t.Errorf("the login URL carries %q, which is not a SAML request id", id)
	}
	if strings.Contains(location, "attacker") || strings.Contains(location, "AuthnRequest") {
		t.Errorf("the login URL carries request content: %q", location)
	}

	// And the request is waiting to be answered.
	var pending int
	f.factory.QueryRow(&pending,
		`SELECT count(*) FROM saml_authn_requests WHERE id = '_req2' AND consumed_at IS NULL`)
	if pending != 1 {
		t.Errorf("%d pending requests recorded, want 1", pending)
	}
}

// C-6, through the endpoint: a service provider asking for more than the
// session earned is told so, at its own registered address, rather than handed
// an assertion that claims it.
func TestARequestForMoreThanTheSessionEarnedIsRefusedToTheServiceProvider(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	request := authnRequestFor("_req3", "https://sp.example.test", saml.ClassMultiFactor)
	w := f.redirect(t, deflateEncode(t, request), "")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, truncate(w.Body.String()))
	}
	body := w.Body.String()

	if !strings.Contains(body, "https://sp.example.test/acs") {
		t.Error("the refusal did not go to the registered ACS URL")
	}
	field := reSAMLResponse.FindStringSubmatch(body)
	if field == nil {
		t.Fatal("no SAMLResponse in the refusal")
	}
	raw, _ := base64.StdEncoding.DecodeString(html.UnescapeString(field[1]))
	doc := string(raw)

	if !strings.Contains(doc, saml.StatusNoAuthnContext) {
		t.Errorf("the refusal does not carry NoAuthnContext: %s", truncate(doc))
	}
	if strings.Contains(doc, "<Assertion") {
		t.Error("an assertion was issued for a session that did not meet the request")
	}
}

// An unregistered issuer is refused HERE, with nothing delivered anywhere: the
// only address available is one the request supplied.
func TestAnUnregisteredServiceProviderIsRefusedWithoutADelivery(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.redirect(t, deflateEncode(t, authnRequestFor("_req4", "https://stranger.example.test", "")), "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "SAMLResponse") {
		t.Error("a response was delivered to an unverified address")
	}
}

// A-2 through the endpoint.
func TestADecompressionBombIsRefusedByTheEndpoint(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	bomb := deflateEncode(t, strings.Repeat("A", 32*1024*1024))
	w := f.redirect(t, bomb, "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for a decompression bomb", w.Code)
	}
	// The reason is not told to the caller: "your bomb was too large" is useful
	// to somebody tuning one and useless to a service provider.
	if strings.Contains(strings.ToLower(w.Body.String()), "bomb") ||
		strings.Contains(strings.ToLower(w.Body.String()), "expand") {
		t.Errorf("the refusal explains the defence: %q", w.Body.String())
	}
}

// The metadata an SP configures itself from, served.
func TestMetadataIsServedAndNamesTheSigningCertificate(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	r := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	w := httptest.NewRecorder()
	f.handler.Metadata(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/samlmetadata+xml" {
		t.Errorf("content type %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, base64.StdEncoding.EncodeToString(f.cert.Raw)) {
		t.Error("the metadata does not advertise the signing certificate")
	}
	if strings.Contains(body, "SingleLogoutService") {
		t.Error("the metadata advertises a logout endpoint that does not exist")
	}
}

// Without a key there is no SAML, and the endpoints say so rather than serving
// something nothing can verify.
func TestWithoutASigningKeyTheEndpointsAreUnavailable(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	f.handler.Keys = staticKeys{}

	r := httptest.NewRequest(http.MethodGet, "/saml/metadata", nil)
	w := httptest.NewRecorder()
	f.handler.Metadata(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("metadata answered %d with no key, want 503", w.Code)
	}

	if got := f.redirect(t, deflateEncode(t, authnRequestFor("_req5", "https://sp.example.test", "")), ""); got.Code != http.StatusServiceUnavailable {
		t.Errorf("sso answered %d with no key, want 503", got.Code)
	}
}

// Single Logout refuses in the protocol's vocabulary rather than 404.
func TestSingleLogoutRefusesWithAReason(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	r := httptest.NewRequest(http.MethodPost, "/saml/slo", nil)
	w := httptest.NewRecorder()
	f.handler.SingleLogout(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status %d — a 404 reads as a misconfiguration to retry rather than a decision", w.Code)
	}
	if !strings.Contains(w.Body.String(), saml.StatusRequestDenied) {
		t.Error("the refusal does not name a SAML status")
	}
}

// The form posts to a partner's origin, so the policy must allow that and
// nothing wider (T4-7).
func TestTheDeliveredFormRestrictsItsFormAction(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.redirect(t, deflateEncode(t, authnRequestFor("_req6", "https://sp.example.test", "")), "")
	policy := w.Header().Get("Content-Security-Policy")

	if !strings.Contains(policy, "form-action https://sp.example.test") {
		t.Errorf("policy %q does not allow the registered ACS origin", policy)
	}
	if strings.Contains(policy, "attacker") {
		t.Errorf("policy %q allows an origin from the request", policy)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("the delivered assertion is cacheable")
	}
}

// One request, one answer, through the endpoint and the resume seam.
func TestAnAnsweredRequestCannotBeAnsweredAgain(t *testing.T) {
	f := setupSSO(t, []string{"email"}, nil, false)

	// Start it: no session, so it is stored.
	if w := f.redirect(t, deflateEncode(t, authnRequestFor("_req7", "https://sp.example.test", "")), ""); w.Code != http.StatusSeeOther {
		t.Fatalf("expected a redirect to the login, got %d", w.Code)
	}

	current := session.Session{
		ID: "sess-2", UserID: f.userID, OrgID: f.orgID,
		AuthMethods: []string{"pwd"}, CreatedAt: time.Now(),
	}

	first := httptest.NewRecorder()
	f.handler.Resume(first, httptest.NewRequest(http.MethodGet, "/login", nil), RequestPrefix+"_req7", current)
	if first.Code != http.StatusOK {
		t.Fatalf("the first answer failed: %d %s", first.Code, truncate(first.Body.String()))
	}
	f.assertionFrom(t, first.Body.String())

	second := httptest.NewRecorder()
	f.handler.Resume(second, httptest.NewRequest(http.MethodGet, "/login", nil), RequestPrefix+"_req7", current)
	if second.Code == http.StatusOK && strings.Contains(second.Body.String(), "SAMLResponse") {
		t.Error("the same AuthnRequest was answered twice")
	}
}

// A session from another organization cannot complete a login for this service
// provider, whatever the request says.
func TestASessionFromAnotherOrganizationCannotCompleteTheLogin(t *testing.T) {
	f := setupSSO(t, []string{"email"}, nil, false)

	if w := f.redirect(t, deflateEncode(t, authnRequestFor("_req8", "https://sp.example.test", "")), ""); w.Code != http.StatusSeeOther {
		t.Fatalf("expected a redirect, got %d", w.Code)
	}

	stranger := testsupport.NewFactory(t, testsupport.Start(t))
	otherOrg := stranger.Organization(stranger.Instance())

	w := httptest.NewRecorder()
	f.handler.Resume(w, httptest.NewRequest(http.MethodGet, "/login", nil), RequestPrefix+"_req8",
		session.Session{ID: "sess-3", UserID: f.userID, OrgID: otherOrg, AuthMethods: []string{"pwd"}})

	if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "SAMLResponse") {
		t.Error("an assertion was issued across an organization boundary")
	}
}

// The audit trail: an issued assertion is a token.issued event, so a query that
// does not know SAML exists still finds it (F-8).
func TestAnIssuedAssertionIsAuditedAsAnIssuedToken(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	f.handler.Audit = NewAuditWriter(audit.NewWriter(f.db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))

	if w := f.redirect(t, deflateEncode(t, authnRequestFor("_req9", "https://sp.example.test", "")), ""); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, truncate(w.Body.String()))
	}

	var count int
	f.factory.QueryRow(&count, `
		SELECT count(*) FROM events
		 WHERE event_type = 'token.issued'
		   AND payload->>'protocol' = 'saml'
		   AND payload->>'entity_id' = 'https://sp.example.test'`)
	if count != 1 {
		t.Errorf("%d SAML issuance events, want 1 — a SAML login must be as visible as an OIDC one", count)
	}
}

// A registration that asks for signed requests gets them verified — or refused
// (P4-08, closing the gap where the flag was stored and not enforced).

// signedRequest builds an AuthnRequest signed by the service provider.
func signedRequest(t *testing.T, id, issuer string, ks dsig.X509KeyStore) string {
	t.Helper()
	el := etree.NewElement("AuthnRequest")
	el.CreateAttr("xmlns", "urn:oasis:names:tc:SAML:2.0:protocol")
	el.CreateAttr("ID", id)
	el.CreateAttr("Version", "2.0")
	el.CreateAttr("IssueInstant", time.Now().UTC().Format(time.RFC3339))
	el.CreateAttr("AssertionConsumerServiceURL", "https://attacker.example.test/acs")
	el.CreateElement("Issuer").SetText(issuer)

	signed, err := dsig.NewDefaultSigningContext(ks).SignEnveloped(el)
	if err != nil {
		t.Fatalf("signing the request: %v", err)
	}
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	raw, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialising: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// spKeyStore is a service provider's own signing key.
type spKeyStore struct {
	key *rsa.PrivateKey
	der []byte
}

func (k spKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return k.key, k.der, nil }

func serviceProviderKey(t *testing.T) (dsig.X509KeyStore, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certifying: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return spKeyStore{key: key, der: der}, string(pemBytes)
}

func (f *ssoFixture) requireSignedRequests(t *testing.T, certPEM string) {
	t.Helper()
	f.factory.Exec(
		`UPDATE saml_service_providers SET want_signed_requests = true, certificate = $2 WHERE id = $1`,
		f.reg.ID, certPEM)
}

func TestASignedRequestIsVerifiedOnThePostBinding(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	ks, certPEM := serviceProviderKey(t)
	f.requireSignedRequests(t, certPEM)

	w := f.post(t, signedRequest(t, "_signed1", "https://sp.example.test", ks), "")
	if w.Code != http.StatusOK {
		t.Fatalf("a correctly signed request was refused: %d %s", w.Code, truncate(w.Body.String()))
	}
	f.assertionFrom(t, w.Body.String())
}

// The flag doing its job: an unsigned request to a registration that requires
// signing is refused, in the protocol's vocabulary, at the registered address.
func TestAnUnsignedRequestIsRefusedWhenTheRegistrationRequiresSigning(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	_, certPEM := serviceProviderKey(t)
	f.requireSignedRequests(t, certPEM)

	unsigned := base64.StdEncoding.EncodeToString(
		[]byte(authnRequestFor("_unsigned", "https://sp.example.test", "")))
	w := f.post(t, unsigned, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<Assertion") {
		t.Error("an unsigned request was answered with an assertion")
	}
	field := reSAMLResponse.FindStringSubmatch(body)
	if field == nil {
		t.Fatal("no SAMLResponse in the refusal")
	}
	raw, _ := base64.StdEncoding.DecodeString(html.UnescapeString(field[1]))
	if !strings.Contains(string(raw), saml.StatusRequester) {
		t.Errorf("the refusal does not carry a Requester status: %s", truncate(string(raw)))
	}
}

// Signed by somebody else's key.
func TestARequestSignedByAnotherKeyIsRefused(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	ks, _ := serviceProviderKey(t)
	_, otherPEM := serviceProviderKey(t) // the registration trusts a different key
	f.requireSignedRequests(t, otherPEM)

	w := f.post(t, signedRequest(t, "_wrongkey", "https://sp.example.test", ks), "")
	if strings.Contains(w.Body.String(), "<Assertion") {
		t.Error("a request signed by an unknown key was answered with an assertion")
	}
}

// The Redirect binding signs a query string rather than the XML, and that
// construction is not implemented. A registration that requires signing is
// REFUSED there rather than accepted unverified — the visible failure an
// administrator can act on, instead of a flag that silently verifies nothing.
func TestTheRedirectBindingRefusesWhenSigningIsRequired(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	_, certPEM := serviceProviderKey(t)
	f.requireSignedRequests(t, certPEM)

	w := f.redirect(t, deflateEncode(t, authnRequestFor("_redir", "https://sp.example.test", "")), "")
	if strings.Contains(w.Body.String(), "<Assertion") {
		t.Error("the Redirect binding issued an assertion for a registration that requires signed requests")
	}
}

// And a registration that does NOT require signing is unaffected, so the check
// is the flag's rather than a refusal of everything.
func TestAnUnsignedRequestIsFineWhenSigningIsNotRequired(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.post(t, base64.StdEncoding.EncodeToString(
		[]byte(authnRequestFor("_plain", "https://sp.example.test", ""))), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	f.assertionFrom(t, w.Body.String())
}

// IdP-initiated sign-on, and the opt-in that guards it (P4-08 F-6).

func (f *ssoFixture) allowIdPInitiated(t *testing.T) {
	t.Helper()
	f.factory.Exec(`UPDATE saml_service_providers SET allow_idp_initiated = true WHERE id = $1`, f.reg.ID)
}

func (f *ssoFixture) initiate(t *testing.T, entityID, relayState string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"entity_id": {entityID}}
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	r := httptest.NewRequest(http.MethodGet, "/saml/init?"+q.Encode(), nil)
	if signedIn {
		r = withSession(r)
	}
	w := httptest.NewRecorder()
	f.handler.Initiate(w, r)
	return w
}

// The opt-in doing its job. A registration that has not enabled unsolicited
// sign-on receives nothing at all — not even a SAML failure, which would still
// be delivering something it never asked for.
func TestIdPInitiatedIsRefusedUnlessTheRegistrationOptsIn(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.initiate(t, "https://sp.example.test", "", true)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for a service provider that has not opted in", w.Code)
	}
	if strings.Contains(w.Body.String(), "SAMLResponse") {
		t.Error("an unsolicited assertion was delivered to a service provider that did not ask for the capability")
	}
}

func TestIdPInitiatedDeliversAnAssertionWithNoInResponseTo(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	f.allowIdPInitiated(t)

	w := f.initiate(t, "https://sp.example.test", "/portal", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, truncate(w.Body.String()))
	}

	body := w.Body.String()
	if !strings.Contains(body, "https://sp.example.test/acs") {
		t.Error("the form does not post to the registered ACS URL")
	}
	if !strings.Contains(body, `name="RelayState" value="/portal"`) {
		t.Error("RelayState was not carried")
	}

	f.assertionFrom(t, body)

	// The response must not claim to answer a request.
	field := reSAMLResponse.FindStringSubmatch(body)
	raw, _ := base64.StdEncoding.DecodeString(html.UnescapeString(field[1]))
	if strings.Contains(string(raw), "InResponseTo") {
		t.Error("an IdP-initiated response carries InResponseTo — it answers no request, " +
			"and saying otherwise invents a correlation the service provider would rely on")
	}
}

// With no session the browser signs in and comes back — and the assertion it
// eventually receives still answers no request, even though this service had
// to invent an id to find the browser again.
func TestIdPInitiatedSignsInAndStillAnswersNoRequest(t *testing.T) {
	f := setupSSO(t, []string{"email"}, nil, false)
	f.allowIdPInitiated(t)

	w := f.initiate(t, "https://sp.example.test", "/portal", false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want a redirect to the login: %s", w.Code, truncate(w.Body.String()))
	}
	location := w.Header().Get("Location")
	u, _ := url.Parse(location)
	id := u.Query().Get("request")
	if !IsSAMLRequest(id) {
		t.Fatalf("the login URL carries %q, which is not a SAML request id", id)
	}

	resumed := httptest.NewRecorder()
	f.handler.Resume(resumed, httptest.NewRequest(http.MethodGet, "/login", nil), id,
		session.Session{
			ID: "sess-idp", UserID: f.userID, OrgID: f.orgID,
			AuthMethods: []string{"pwd"}, CreatedAt: time.Now(),
		})

	if resumed.Code != http.StatusOK {
		t.Fatalf("the resume failed: %d %s", resumed.Code, truncate(resumed.Body.String()))
	}
	f.assertionFrom(t, resumed.Body.String())

	field := reSAMLResponse.FindStringSubmatch(resumed.Body.String())
	raw, _ := base64.StdEncoding.DecodeString(html.UnescapeString(field[1]))
	if strings.Contains(string(raw), "InResponseTo") {
		t.Error("the bookkeeping id was echoed as InResponseTo")
	}
	if !strings.Contains(resumed.Body.String(), `name="RelayState" value="/portal"`) {
		t.Error("RelayState did not survive the login")
	}
}

func TestIdPInitiatedRefusesAnUnregisteredServiceProvider(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.initiate(t, "https://stranger.example.test", "", true)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "SAMLResponse") {
		t.Error("something was delivered for an unregistered service provider")
	}
}

// Asserting on the status alone would prove nothing here: with the guard
// removed, the lookup of the empty string fails and answers 400 anyway. The
// reason is the only thing that separates "you named nobody" from "you named
// somebody unknown", so the reason is what this checks.
func TestIdPInitiatedNeedsAServiceProvider(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)

	w := f.initiate(t, "", "", true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d with no entity_id, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No service provider was named") {
		t.Errorf("the refusal reads %q — an empty entity_id must be refused as a missing "+
			"parameter, not as an unregistered service provider", truncate(w.Body.String()))
	}
}

// The bound is refused, never truncated — a RelayState the service provider
// cannot match against what it stored is worse than no sign-on at all, and the
// registration never sees a value this service quietly shortened.
func TestIdPInitiatedRefusesAnOversizedRelayState(t *testing.T) {
	f := setupSSO(t, []string{"email"}, []string{"pwd"}, true)
	f.allowIdPInitiated(t)

	w := f.initiate(t, "https://sp.example.test", strings.Repeat("a", saml.MaxRelayStateBytes+1), true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d for an oversized RelayState, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "SAMLResponse") {
		t.Error("an assertion was delivered despite a RelayState over the bound")
	}
}

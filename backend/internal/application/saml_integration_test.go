//go:build integration

package application

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/saml"
)

// A SAML application and the service provider it represents (P4-09).
//
// These drive the Management API rather than the store, because the defects
// this task is guarding against live in the join: a registration written
// without its application, a type check that reads the body instead of the
// stored row, a metadata document parsed somewhere other than the hardened
// parser. Every one of those passes a unit test of either half.

func spCertificate(t *testing.T, notAfter time.Time) (der []byte, pemText string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "sp.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err = x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}
	return der, "-----BEGIN CERTIFICATE-----\n" +
		wrap(base64.StdEncoding.EncodeToString(der)) + "-----END CERTIFICATE-----\n"
}

func wrap(s string) string {
	var out strings.Builder
	for len(s) > 64 {
		out.WriteString(s[:64])
		out.WriteString("\n")
		s = s[64:]
	}
	out.WriteString(s)
	out.WriteString("\n")
	return out.String()
}

// spMetadata is a service provider's own document, as one would send it.
func spMetadata(entityID, acsURL string, der []byte, signed bool) string {
	key := ""
	if der != nil {
		key = `<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>` +
			base64.StdEncoding.EncodeToString(der) +
			`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>`
	}
	return `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"` +
		` xmlns:ds="http://www.w3.org/2000/09/xmldsig#" entityID="` + entityID + `">` +
		fmt.Sprintf(`<md:SPSSODescriptor AuthnRequestsSigned="%t" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">`, signed) +
		key +
		`<md:AssertionConsumerService Binding="` + saml.BindingHTTPPost +
		`" Location="` + acsURL + `" index="0" isDefault="true"/>` +
		`</md:SPSSODescriptor></md:EntityDescriptor>`
}

// samlBody returns a create request for a SAML application.
func samlBody(name string, registration string) string {
	return fmt.Sprintf(`{"name":%q,"type":"saml","saml":%s}`, name, registration)
}

func decodeAppJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decoding the application: %v — %s", err, body)
	}
	return out
}

func samlOf(t *testing.T, body string) map[string]any {
	t.Helper()
	app := decodeAppJSON(t, body)
	registration, ok := app["saml"].(map[string]any)
	if !ok {
		t.Fatalf("the response carries no saml object: %s", body)
	}
	return registration
}

// --- creation ---------------------------------------------------------------

func TestASamlApplicationIsCreatedFromItsMetadata(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	der, _ := spCertificate(t, time.Now().Add(365*24*time.Hour))
	entity := "https://sp-metadata.example.test"
	metadata := spMetadata(entity, "https://sp-metadata.example.test/acs", der, true)

	w := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Legacy payroll", fmt.Sprintf(`{"metadata_xml":%q}`, metadata))),
		http.StatusCreated)

	registration := samlOf(t, w.Body.String())
	if registration["entity_id"] != entity {
		t.Errorf("entity_id %v, want %q", registration["entity_id"], entity)
	}
	if registration["acs_url"] != "https://sp-metadata.example.test/acs" {
		t.Errorf("acs_url %v", registration["acs_url"])
	}
	if registration["want_signed_requests"] != true {
		t.Error("AuthnRequestsSigned from the document was not carried")
	}
	// Off unless somebody turned it on, whatever the document said.
	if registration["allow_idp_initiated"] != false {
		t.Error("allow_idp_initiated defaulted to true")
	}
	if registration["certificate_expires_at"] == nil {
		t.Error("no certificate expiry was reported")
	}
	if registration["certificate_expires_soon"] != false {
		t.Errorf("a certificate a year out is reported as expiring soon")
	}
}

func TestASamlApplicationIsCreatedFromManualFields(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	body := samlBody("Manual", `{"entity_id":"https://manual.example.test",`+
		`"acs_url":"https://manual.example.test/acs","attribute_release":["email"]}`)
	w := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), body),
		http.StatusCreated)

	registration := samlOf(t, w.Body.String())
	if registration["entity_id"] != "https://manual.example.test" {
		t.Errorf("entity_id %v", registration["entity_id"])
	}
	// A service provider that has not asked for anything receives the NameID
	// and nothing else, so what it DID ask for must survive exactly.
	release, _ := registration["attribute_release"].([]any)
	if len(release) != 1 || release[0] != "email" {
		t.Errorf("attribute_release %v", registration["attribute_release"])
	}
	// No certificate, so nothing to expire and nothing to warn about.
	if registration["certificate_expires_at"] != nil {
		t.Error("an expiry was reported for a registration with no certificate")
	}
}

// The registration and the application are written together or not at all.
//
// An entity ID already taken must not leave an application behind: one that
// exists and matches no AuthnRequest looks configured in the console and
// cannot complete a login.
func TestADuplicateEntityIdLeavesNoApplicationBehind(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	entity := "https://contested.example.test"
	first := samlBody("First", fmt.Sprintf(
		`{"entity_id":%q,"acs_url":"https://contested.example.test/acs"}`, entity))
	mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), first), http.StatusCreated)

	second := samlBody("Second", fmt.Sprintf(
		`{"entity_id":%q,"acs_url":"https://other.example.test/acs"}`, entity))
	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), second)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d for a duplicate entity id, want 409: %s", w.Code, w.Body.String())
	}

	listed := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA), ""), http.StatusOK)
	if strings.Contains(listed.Body.String(), `"Second"`) {
		t.Error("the refused application was created anyway — the registration and the " +
			"application are not written in one transaction")
	}
}

// Both directions of the type check, because both are a caller believing
// something untrue.
func TestTheRegistrationAndTheTypeMustAgree(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	t.Run("a saml application without a registration", func(t *testing.T) {
		w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
			`{"name":"Naked","type":"saml"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
		}
		// The reason, not only the status. Without the type check, an empty
		// registration reaches the store and is refused there for a missing
		// entity_id — also a 400, and a misleading one: the caller left out
		// the whole registration, not one field of it. A mutation removing the
		// check stayed green until this asserted which field was named.
		if !strings.Contains(w.Body.String(), `"field":"saml"`) {
			t.Errorf("the refusal does not name the missing registration: %s", w.Body.String())
		}
	})

	t.Run("a registration on a web application", func(t *testing.T) {
		w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
			`{"name":"Confused","type":"web","redirect_uris":["https://x.example.test/cb"],`+
				`"saml":{"entity_id":"https://x.example.test","acs_url":"https://x.example.test/acs"}}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
		}
	})
}

// Metadata or fields, never both, because answering by precedence would make
// the choice a guess the caller never sees.
func TestMetadataAndFieldsTogetherAreRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	der, _ := spCertificate(t, time.Now().Add(time.Hour))
	metadata := spMetadata("https://both.example.test", "https://both.example.test/acs", der, false)

	body := samlBody("Both", fmt.Sprintf(
		`{"metadata_xml":%q,"entity_id":"https://other.example.test"}`, metadata))
	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

// The registration goes through the hardened parser, asserted at the API
// rather than at the parser — the handler is one call away from a different
// one that is not hardened.
func TestUploadedMetadataIsRefusedByTheXmlGate(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	der, _ := spCertificate(t, time.Now().Add(time.Hour))
	hostile := `<?xml version="1.0"?><!DOCTYPE EntityDescriptor [<!ENTITY x "y">]>` +
		spMetadata("https://hostile.example.test", "https://hostile.example.test/acs", der, false)

	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Hostile", fmt.Sprintf(`{"metadata_xml":%q}`, hostile)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d for a document carrying a DTD, want 400: %s", w.Code, w.Body.String())
	}
	// And the refusal does not report what the gate found. These fire on
	// documents malformed in ways an attacker chooses, and a detailed answer
	// is a probe result.
	//
	// Asserted as the exact generic issue rather than as the absence of one
	// word. The first version checked for "doctype", and the gate's own
	// message says "DTD" — so a mutation echoing the full reason passed it.
	if !strings.Contains(w.Body.String(), `"issue":"is not a document this service will parse"`) {
		t.Errorf("the refusal is not the generic one: %s", w.Body.String())
	}
	for _, leaked := range []string{"DTD", "entity", "doctype", "declares"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Errorf("the refusal echoes %q, which says what the hardening detected", leaked)
		}
	}
}

// A flag with nothing to verify against is the shape P4-08 spent a task
// removing. Refused rather than stored and quietly ineffective.
func TestSignedRequestsWithoutACertificateIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Unverifiable", `{"entity_id":"https://unverifiable.example.test",`+
			`"acs_url":"https://unverifiable.example.test/acs","want_signed_requests":true}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

// An assertion is a bearer credential. An http ACS URL hands it to the network.
func TestAnHttpAcsUrlIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Cleartext", `{"entity_id":"https://cleartext.example.test",`+
			`"acs_url":"http://cleartext.example.test/acs"}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

// --- reading and changing ---------------------------------------------------

func TestTheRegistrationIsReadBackWithTheApplication(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Readback", `{"entity_id":"https://readback.example.test",`+
			`"acs_url":"https://readback.example.test/acs"}`)), http.StatusCreated)
	id := decodeAppJSON(t, created.Body.String())["id"].(string)

	read := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA)+"/"+id, ""),
		http.StatusOK)
	registration := samlOf(t, read.Body.String())
	if registration["entity_id"] != "https://readback.example.test" {
		t.Errorf("entity_id %v", registration["entity_id"])
	}
}

// The two switches P4-08 shipped with no administrator in front of them.
func TestTheTwoSwitchesAreSettableThroughTheApi(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	_, certificate := spCertificate(t, time.Now().Add(365*24*time.Hour))

	created := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Switches", `{"entity_id":"https://switches.example.test",`+
			`"acs_url":"https://switches.example.test/acs"}`)), http.StatusCreated)
	id := decodeAppJSON(t, created.Body.String())["id"].(string)

	before := samlOf(t, created.Body.String())
	if before["want_signed_requests"] != false || before["allow_idp_initiated"] != false {
		t.Fatal("the switches did not start off, so turning them on proves nothing")
	}

	update := fmt.Sprintf(`{"saml":{"entity_id":"https://switches.example.test",`+
		`"acs_url":"https://switches.example.test/acs","want_signed_requests":true,`+
		`"allow_idp_initiated":true,"certificate":%q}}`, certificate)
	w := mustStatus(t, f.call(t, http.MethodPatch, f.apps(f.orgA, f.projectA)+"/"+id, update),
		http.StatusOK)

	after := samlOf(t, w.Body.String())
	if after["want_signed_requests"] != true {
		t.Error("want_signed_requests was not set")
	}
	if after["allow_idp_initiated"] != true {
		t.Error("allow_idp_initiated was not set")
	}

	// And it is stored, not merely echoed.
	read := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA)+"/"+id, ""),
		http.StatusOK)
	stored := samlOf(t, read.Body.String())
	if stored["allow_idp_initiated"] != true || stored["want_signed_requests"] != true {
		t.Error("the switches were echoed but not stored")
	}
}

// An omitted registration leaves it alone, like every other omitted field.
func TestRenamingASamlApplicationKeepsItsRegistration(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Before", `{"entity_id":"https://rename.example.test",`+
			`"acs_url":"https://rename.example.test/acs","attribute_release":["email"]}`)),
		http.StatusCreated)
	id := decodeAppJSON(t, created.Body.String())["id"].(string)

	w := mustStatus(t, f.call(t, http.MethodPatch, f.apps(f.orgA, f.projectA)+"/"+id,
		`{"name":"After"}`), http.StatusOK)

	registration := samlOf(t, w.Body.String())
	if registration["entity_id"] != "https://rename.example.test" {
		t.Errorf("a rename changed the entity id to %v", registration["entity_id"])
	}
	release, _ := registration["attribute_release"].([]any)
	if len(release) != 1 || release[0] != "email" {
		t.Errorf("a rename cleared attribute_release: %v", registration["attribute_release"])
	}
}

// A registration sent to an application that has none is refused rather than
// silently discarded.
func TestARegistrationSentToAWebApplicationIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created, _ := f.createConfidential(t, "Ordinary web app")
	id := created.Id.String()

	w := f.call(t, http.MethodPatch, f.apps(f.orgA, f.projectA)+"/"+id,
		`{"saml":{"entity_id":"https://x.example.test","acs_url":"https://x.example.test/acs"}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

// --- the expiry warning ------------------------------------------------------

// A certificate that expires soon is reported before it produces an outage.
//
// The clock is injected rather than the certificate being generated 29 days
// out, so the boundary is tested at the boundary instead of near it.
func TestACertificateNearingExpiryIsReportedAsExpiringSoon(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	expiry := time.Now().Add(365 * 24 * time.Hour)
	_, certificate := spCertificate(t, expiry)

	created := mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Expiring", fmt.Sprintf(
			`{"entity_id":"https://expiring.example.test",`+
				`"acs_url":"https://expiring.example.test/acs","certificate":%q}`, certificate))),
		http.StatusCreated)
	id := decodeAppJSON(t, created.Body.String())["id"].(string)

	// Today: a year of life left.
	if samlOf(t, created.Body.String())["certificate_expires_soon"] != false {
		t.Fatal("a certificate a year out is already reported as expiring")
	}

	// One day inside the window.
	f.applications.Now = func() time.Time {
		return expiry.Add(-saml.CertificateWarningWindow).Add(24 * time.Hour)
	}
	w := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA)+"/"+id, ""),
		http.StatusOK)
	if samlOf(t, w.Body.String())["certificate_expires_soon"] != true {
		t.Error("a certificate inside the warning window is not reported as expiring soon")
	}

	// One day outside it, so the flag is the window rather than always true.
	f.applications.Now = func() time.Time {
		return expiry.Add(-saml.CertificateWarningWindow).Add(-24 * time.Hour)
	}
	w = mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA)+"/"+id, ""),
		http.StatusOK)
	if samlOf(t, w.Body.String())["certificate_expires_soon"] != false {
		t.Error("a certificate outside the warning window is reported as expiring soon")
	}
}

// An attribute nobody releases is refused rather than stored.
//
// `Email` with a capital would otherwise save cleanly and release nothing, and
// the administrator who ticked it would believe the service provider now
// receives an address. The refusal names the ones that exist.
func TestAnUnknownAttributeIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Typo", `{"entity_id":"https://typo.example.test",`+
			`"acs_url":"https://typo.example.test/acs","attribute_release":["Email"]}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "display_name, email, role_keys, username") {
		t.Errorf("the refusal does not name the releasable attributes: %s", w.Body.String())
	}
}

// The list carries each registration, so the console can show a service
// provider and its certificate warning without a request per row — and a
// non-SAML application carries none.
func TestTheListCarriesEachRegistration(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		samlBody("Listed SAML", `{"entity_id":"https://listed.example.test",`+
			`"acs_url":"https://listed.example.test/acs"}`)), http.StatusCreated)
	f.createConfidential(t, "Listed web")

	w := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA), ""), http.StatusOK)

	var list struct {
		Applications []map[string]any `json:"applications"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding the list: %v", err)
	}

	seen := map[string]bool{}
	for _, app := range list.Applications {
		name, _ := app["name"].(string)
		registration, hasSaml := app["saml"].(map[string]any)
		switch name {
		case "Listed SAML":
			seen[name] = true
			if !hasSaml {
				t.Fatal("the SAML application is listed without its registration")
			}
			if registration["entity_id"] != "https://listed.example.test" {
				t.Errorf("entity_id %v", registration["entity_id"])
			}
		case "Listed web":
			seen[name] = true
			if hasSaml {
				t.Error("a web application is listed with a SAML registration")
			}
		}
	}
	if !seen["Listed SAML"] || !seen["Listed web"] {
		t.Fatalf("the list did not contain both applications: %v", seen)
	}
}

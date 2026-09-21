package saml

import (
	"crypto/x509"
	"encoding/base64"
	html2 "html"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Delivering a response through the browser (P4-08 F-4, F-5, A-4).

const acsURL = "https://sp.example.test/acs"

func TestAResponseCarriesTheSignedAssertionAndStaysVerifiable(t *testing.T) {
	key := samlKey(t)
	now := time.Now()

	assertion, err := Issue(key, testIssuer, testSP(), testSubject(), "_req1", now)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	response, err := Response(testIssuer, "_req1", acsURL, StatusSuccess, assertion, now)
	if err != nil {
		t.Fatalf("building the response: %v", err)
	}

	if got := response.SelectAttrValue("Destination", ""); got != acsURL {
		t.Errorf("Destination is %q, want the registered ACS URL", got)
	}
	if got := response.SelectAttrValue("InResponseTo", ""); got != "_req1" {
		t.Errorf("InResponseTo is %q, want the request id", got)
	}

	// C-2: the ASSERTION is signed, so a service provider that verifies only
	// the assertion — or only the response — is safe either way. This checks
	// the assertion survives the envelope and still verifies.
	cert, err := key.Certificate()
	if err != nil {
		t.Fatalf("reading the certificate: %v", err)
	}
	v, err := Verify(serialise(t, response), "Assertion", []*x509.Certificate{cert})
	if err != nil {
		t.Fatalf("the assertion inside the response did not verify: %v", err)
	}
	if err := CheckConditions(v, ourEntityID, now); err != nil {
		t.Errorf("the assertion inside the response failed its conditions: %v", err)
	}
}

func TestAFailureResponseNeedsNoAssertion(t *testing.T) {
	now := time.Now()
	response, err := Response(testIssuer, "_req1", acsURL, StatusNoAuthnContext, nil, now)
	if err != nil {
		t.Fatalf("building a failure response: %v", err)
	}
	code := response.FindElement("./samlp:Status/samlp:StatusCode")
	if code == nil || code.SelectAttrValue("Value", "") != StatusNoAuthnContext {
		t.Error("the failure response does not carry the status")
	}
	if response.FindElement("./Assertion") != nil {
		t.Error("a failure response carries an assertion")
	}
}

func TestASuccessfulResponseWithoutAnAssertionIsRefused(t *testing.T) {
	if _, err := Response(testIssuer, "_r", acsURL, StatusSuccess, nil, time.Now()); err == nil {
		t.Error("a Success response with no assertion was built")
	}
}

// A-4: RelayState is attacker-influenced input echoed into HTML.
func TestRelayStateIsEscapedInTheForm(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	response, _ := Response(testIssuer, "", acsURL, StatusSuccess, assertion, now)

	hostile := `"><script>alert(1)</script><input value="`
	html, err := PostForm(acsURL, response, hostile)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	doc := string(html)
	if strings.Contains(doc, "<script>alert(1)</script>") {
		t.Error("RelayState was echoed into the form unescaped")
	}
	// It must still be carried — escaped, not dropped, or the service provider
	// cannot match it against what it stored.
	if !strings.Contains(doc, "&lt;script&gt;") && !strings.Contains(doc, "&#") {
		t.Error("RelayState was dropped rather than escaped")
	}
}

// The same for the destination, which is registered rather than attacker-chosen
// but is still interpolated into an attribute.
func TestTheDestinationIsEscapedInTheForm(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	response, _ := Response(testIssuer, "", acsURL, StatusSuccess, assertion, now)

	html, err := PostForm(`https://sp.example.test/acs" onload="alert(1)`, response, "")
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(string(html), `onload="alert(1)"`) {
		t.Error("the destination broke out of the action attribute")
	}
}

func TestTheFormCarriesTheResponseAsBase64(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "_req1", now)
	response, _ := Response(testIssuer, "_req1", acsURL, StatusSuccess, assertion, now)

	html, err := PostForm(acsURL, response, "/dashboard")
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	field := regexp.MustCompile(`name="SAMLResponse" value="([^"]*)"`).FindStringSubmatch(string(html))
	if field == nil {
		t.Fatal("the form carries no SAMLResponse field")
	}
	// Unescaped first, because that is what a browser does before submitting.
	// html/template writes `+` as `&#43;` inside an attribute — correct HTML,
	// and standard base64 is full of them. A test that decoded the raw markup
	// would be testing the escaper rather than the field.
	decoded, err := base64.StdEncoding.DecodeString(html2.UnescapeString(field[1]))
	if err != nil {
		t.Fatalf("the SAMLResponse field is not base64: %v", err)
	}
	if !strings.Contains(string(decoded), "_req1") {
		t.Error("the posted document is not the response that was built")
	}

	if !strings.Contains(string(html), `name="RelayState" value="/dashboard"`) {
		t.Error("RelayState is not carried")
	}
}

// An absent RelayState means no field at all, rather than an empty one: a
// service provider that distinguishes "absent" from "empty" is entitled to.
func TestNoRelayStateMeansNoField(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	response, _ := Response(testIssuer, "", acsURL, StatusSuccess, assertion, now)

	html, err := PostForm(acsURL, response, "")
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(string(html), "RelayState") {
		t.Error("an empty RelayState was carried as a field")
	}
}

func TestAnOversizedRelayStateIsRefusedRatherThanTruncated(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	response, _ := Response(testIssuer, "", acsURL, StatusSuccess, assertion, now)

	// Truncating would produce a value the service provider cannot match
	// against anything it stored, failing on their side with no explanation
	// available here.
	if _, err := PostForm(acsURL, response, strings.Repeat("x", MaxRelayStateBytes+1)); err == nil {
		t.Error("an oversized RelayState was accepted")
	}
}

// The form works without script, or a login fails with no explanation for
// anyone who has it disabled.
func TestTheFormSubmitsWithoutScript(t *testing.T) {
	key := samlKey(t)
	now := time.Now()
	assertion, _ := Issue(key, testIssuer, testSP(), testSubject(), "", now)
	response, _ := Response(testIssuer, "", acsURL, StatusSuccess, assertion, now)

	html, _ := PostForm(acsURL, response, "")
	doc := string(html)
	if !strings.Contains(doc, "<noscript>") {
		t.Error("the form has no noscript path")
	}
	if !strings.Contains(doc, `type="submit"`) {
		t.Error("the form has no submit control for a browser without script")
	}
}

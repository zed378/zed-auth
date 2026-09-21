package saml

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"time"

	"github.com/beevik/etree"
)

// Getting an assertion to a service provider (P4-08 F-4, F-5, A-4).
//
// SAML's answer travels through the user's browser, which is the whole reason
// the protocol exists in this shape and also its main hazard: the document is
// handed to the thing an attacker is most likely to control. Two consequences
// shape this file.
//
// **The assertion is signed, not just the response** (C-2). Some service
// providers verify only one of the two, and an unsigned assertion inside a
// signed response is exactly the shape a wrapping attack takes. Signing the
// assertion means a service provider that checks either one is safe.
//
// **The form is built with html/template and nothing else.** `RelayState` is
// attacker-influenced input that gets echoed into HTML, and a form assembled by
// string concatenation is an injection waiting for somebody to try it.

// Status codes this service returns.
const (
	StatusSuccess        = "urn:oasis:names:tc:SAML:2.0:status:Success"
	StatusRequester      = "urn:oasis:names:tc:SAML:2.0:status:Requester"
	StatusResponder      = "urn:oasis:names:tc:SAML:2.0:status:Responder"
	StatusNoAuthnContext = "urn:oasis:names:tc:SAML:2.0:status:NoAuthnContext"
	StatusRequestDenied  = "urn:oasis:names:tc:SAML:2.0:status:RequestDenied"
)

// MaxRelayStateBytes bounds what is echoed back.
//
// The specification says 80 bytes and every real service provider stays far
// inside it. 8 KiB here, matching the column's CHECK: refusing a legitimate
// login over a limit nobody follows is a worse failure than storing a few
// kilobytes, and it is still a bound rather than an invitation.
const MaxRelayStateBytes = 8192

// Response wraps an assertion, or a failure, for one service provider.
//
// The assertion is passed in already signed. This function does NOT sign the
// response envelope, and that is deliberate: a service provider verifying only
// the envelope would be satisfied by an envelope around somebody else's
// assertion, so the signature that matters is the one on the assertion itself
// (C-2). Signing both is possible and adds a second thing to get wrong for no
// property the first does not already give.
func Response(issuer, inResponseTo, destination, status string, assertion *etree.Element, now time.Time) (*etree.Element, error) {
	if issuer == "" || destination == "" {
		return nil, fmt.Errorf("saml: a response needs an issuer and a destination")
	}
	if status == StatusSuccess && assertion == nil {
		return nil, fmt.Errorf("saml: a successful response needs an assertion")
	}

	id, err := assertionID()
	if err != nil {
		return nil, err
	}

	el := etree.NewElement("samlp:Response")
	el.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	el.CreateAttr("ID", id)
	el.CreateAttr("Version", "2.0")
	el.CreateAttr("IssueInstant", now.UTC().Format(time.RFC3339))

	// Destination is the registered ACS URL. A service provider is entitled to
	// check that the response was addressed to it, and one that does is
	// protected from a response replayed at a different endpoint.
	el.CreateAttr("Destination", destination)
	if inResponseTo != "" {
		el.CreateAttr("InResponseTo", inResponseTo)
	}

	el.CreateElement("Issuer").SetText(issuer)

	statusEl := el.CreateElement("samlp:Status")
	statusEl.CreateElement("samlp:StatusCode").CreateAttr("Value", status)

	if assertion != nil {
		el.AddChild(assertion.Copy())
	}
	return el, nil
}

// postForm is the self-submitting form the HTTP-POST binding requires.
//
// Every value goes through html/template's contextual escaping. The form posts
// on load, and the button exists for the seconds before script runs and for
// anyone who has script disabled — a page that silently does nothing without
// JavaScript is a login that fails with no explanation.
//
// No inline event handler and no inline script beyond the submit: the login
// surface's Content-Security-Policy is strict, and SAML does not get an
// exemption from it (threat review T4-7). The script carries a nonce supplied
// by the caller for that reason.
var postForm = template.Must(template.New("saml-post").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Signing you in</title>
</head>
<body onload="document.forms[0].submit()">
<noscript><p>Your browser does not run scripts. Press the button to continue.</p></noscript>
<form method="post" action="{{.Destination}}">
<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}">
{{if .RelayState}}<input type="hidden" name="RelayState" value="{{.RelayState}}">{{end}}
<noscript><input type="submit" value="Continue"></noscript>
</form>
</body>
</html>
`))

type postFormData struct {
	Destination  string
	SAMLResponse string
	RelayState   string
}

// PostForm renders the HTML that delivers a response to a service provider.
//
// `destination` is the REGISTERED ACS URL. It is a parameter rather than read
// from the response document so that a caller cannot accidentally post to a
// destination the document claims — the two are set from the same registration
// by the handler, and this function is the second place that stays true.
func PostForm(destination string, response *etree.Element, relayState string) ([]byte, error) {
	if destination == "" {
		return nil, fmt.Errorf("saml: no destination to post to")
	}
	if response == nil {
		return nil, fmt.Errorf("saml: no response to post")
	}
	if len(relayState) > MaxRelayStateBytes {
		// Refused rather than truncated. A truncated RelayState is a value the
		// service provider cannot match against anything it stored, and it
		// would fail on their side with no explanation available here.
		return nil, fmt.Errorf("saml: RelayState is %d bytes, over the %d bound", len(relayState), MaxRelayStateBytes)
	}

	doc := etree.NewDocument()
	doc.SetRoot(response.Copy())
	raw, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("saml: serialising the response: %w", err)
	}

	var out bytes.Buffer
	err = postForm.Execute(&out, postFormData{
		Destination:  destination,
		SAMLResponse: base64.StdEncoding.EncodeToString(raw),
		RelayState:   relayState,
	})
	if err != nil {
		return nil, fmt.Errorf("saml: rendering the post form: %w", err)
	}
	return out.Bytes(), nil
}

package login

import (
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/samlapi"
)

// The id this page ACCEPTS and the id the dispatcher ROUTES are the same id.
//
// This is a contract test between two rules that were written independently
// and did not agree. `validPendingID` decides whether a request id may reach
// the Authorization seam at all; `samlapi.RequestPrefix` and `IsSAMLRequest`
// decide where it goes once it does. Both were correct in isolation and the
// join was broken: the page refused every `saml:` id one step before the
// dispatcher that exists to route it, and every SAML login through the hosted
// login failed on the deployment.
//
// Importing samlapi here is deliberate and is confined to a test. Production
// code in this package still knows nothing about which protocols exist — that
// is what the dispatcher is for — but *something* has to assert that the two
// conventions line up, and a rule restated in two places is a rule that drifts.
func TestTheLoginPageAcceptsTheIdsTheDispatcherRoutes(t *testing.T) {
	// The shapes a SAML service provider actually mints, and the one this
	// service mints for an IdP-initiated sign-on: `_` plus a hex digest.
	ids := []string{
		"_8e8dc5f69a98cc4c1ff3427e5ce34606fd672f91e6",
		"id-4f3c2b1a",
		"sp.example.test.1",
		"_" + strings.Repeat("a", 40),
		"ONELOGIN_8e8dc5f69a98cc4c1ff3427e5ce34606",
	}

	for _, id := range ids {
		namespaced := samlapi.RequestPrefix + id

		if !validPendingID(namespaced) {
			t.Errorf("the login page refuses %q, which the dispatcher would route to SAML — "+
				"the request never reaches the seam", namespaced)
		}
		if !samlapi.IsSAMLRequest(namespaced) {
			t.Errorf("the dispatcher does not recognise %q as SAML", namespaced)
		}
	}
}

// And the prefix itself conforms to the convention the page enforces.
//
// If RequestPrefix were ever changed to something the namespace rule rejects —
// uppercase, longer than the bound, or without the colon — every SAML login
// would break in exactly the way it did before, and this says so at the moment
// of the change rather than on a deployment.
func TestTheSAMLPrefixConformsToTheIdConvention(t *testing.T) {
	prefix := samlapi.RequestPrefix

	name, rest, ok := strings.Cut(prefix, ":")
	if !ok {
		t.Fatalf("RequestPrefix %q carries no colon, so it is not a namespace", prefix)
	}
	if rest != "" {
		t.Errorf("RequestPrefix %q has content after the colon", prefix)
	}
	if !validIDNamespace(name) {
		t.Errorf("RequestPrefix names %q, which the login page's namespace rule rejects", name)
	}
}

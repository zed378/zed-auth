package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A namespaced request id reaches the page instead of being refused before it.
//
// This is the test that was missing when P4-08's dispatcher landed, and the gap
// it left was total: the dispatcher routed a `saml:` id correctly and this page
// rejected it one step earlier, so every SAML login through the hosted login
// answered "Nothing to sign in to" — on staging, for every service provider.
//
// Nothing caught it. The SAML tests call the Authorization seam directly, so
// they never render this page; the tests for this page only ever used an ids
// this service mints itself. The seam was tested, the page was tested, and the
// one line between them was not.
func TestANamespacedRequestIdReachesThePage(t *testing.T) {
	f := newFixture(t)

	const id = "saml:_8e8dc5f69a98cc4c1ff3427e5ce34606fd672f91e6"

	r := httptest.NewRequest(http.MethodGet, Path+"?request="+id, nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d for a namespaced id, want 200 — the page refused it "+
			"before the dispatcher could route it: %s", w.Code, firstLines(w.Body.String()))
	}
	if strings.Contains(w.Body.String(), "Nothing to sign in to") {
		t.Error("the page answered its no-request notice for a well-formed namespaced id")
	}
}

// And the bound is real: the shapes that must still be refused.
//
// Without these the change above is indistinguishable from deleting the check.
// The namespaced body is chosen by a SAML service provider rather than minted
// here, so it is attacker-influenced and every one of these is a value that
// would otherwise reach a URL and a template.
func TestTheRequestIdShapeIsStillBounded(t *testing.T) {
	refused := map[string]string{
		"empty":                     "",
		"too short for an OAuth id": "abc",
		"too long for an OAuth id":  strings.Repeat("a", 44),
		"a slash":                   "saml:" + strings.Repeat("a", 10) + "/etc",
		"a space":                   "saml:req 1",
		"an angle bracket":          "saml:<script>",
		"a quote":                   `saml:"onload=`,
		"a percent escape":          "saml:%2e%2e",
		"a second colon":            "saml:oauth:" + strings.Repeat("a", 20),
		"an empty namespace":        ":" + strings.Repeat("a", 20),
		"an empty body":             "saml:",
		"a namespace that is not a short lowercase word": "SAML:" + strings.Repeat("a", 20),
		"a body past the bound":                          "saml:" + strings.Repeat("a", maxNamespacedIDBytes+1),
	}

	for name, id := range refused {
		t.Run(name, func(t *testing.T) {
			if validPendingID(id) {
				t.Errorf("validPendingID(%q) accepted it", id)
			}
		})
	}

	accepted := map[string]string{
		"an id this service mints":     testPendingID,
		"a SAML id with an underscore": "saml:_8e8dc5f69a98cc4c1ff3427e5ce34606fd672f91e6",
		"a SAML id with a hyphen":      "saml:id-4f3c2b1a",
		"a SAML id with a dot":         "saml:sp.example.test.1",
		"at the bound":                 "saml:" + strings.Repeat("a", maxNamespacedIDBytes),
	}

	for name, id := range accepted {
		t.Run(name, func(t *testing.T) {
			if !validPendingID(id) {
				t.Errorf("validPendingID(%q) refused a well-formed id", id)
			}
		})
	}
}

func firstLines(body string) string {
	lines := strings.SplitN(body, "\n", 6)
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, " ")
}

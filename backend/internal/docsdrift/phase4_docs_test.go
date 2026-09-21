// The Phase 4 delegation claims on the published site, against the code (P4-04).
//
// `P4-04`'s Definition of Done asks for the revocation window to be documented
// publicly, because "an integrator who believes revocation is instant when it is
// not will build an incorrect security model". A number on a page is a promise;
// this is what keeps the promise tied to the constant behind it.
package docsdrift

import (
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/authz"
)

func TestTheAuthorizationGuideStatesTheDelegatedRevocationWindow(t *testing.T) {
	const name = "guides/authorization-checks.md"
	body := page(t, name)

	const heading = "## Roles delegated by another organization — Phase 4"
	mustContain(t, name, body, heading,
		"the section must exist, and must carry its phase label — the capability audit "+
			"fails any docs section that mentions Project Grants without naming Phase 4")

	section := body[strings.Index(body, heading):]

	// The backstop is the cache TTL, and it is the number an integrator designs
	// an irreversible action around.
	mustContain(t, name, section, "**"+seconds(authz.DefaultTTL)+"** backstop",
		"the delegated window must state the same TTL the cache actually uses")

	// The two properties that make the window meaningful rather than decorative.
	mustContain(t, name, section, "re-derived from the grant on every check",
		"a guide that omits this implies the assignment row alone decides, which is the "+
			"misunderstanding P4-02's write-time validation invites")
	mustContain(t, name, section, "intersection of what you delegated and what the partner assigned",
		"the effective set is the intersection; a guide that says otherwise overstates what a partner holds")

	// And the honest limit: no live path issues a token carrying a delegated
	// role, because a partner's users cannot sign in to the granting
	// organization's applications yet (ADR-025 decided the policy; nothing
	// implements it).
	mustContain(t, name, section, "do **not** appear in access tokens yet",
		"claiming tokens carry delegated roles would be false until cross-organization sign-in exists")
}

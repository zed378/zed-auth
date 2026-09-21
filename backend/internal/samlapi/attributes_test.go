package samlapi

import (
	"database/sql"
	"sort"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/saml"
)

// The names the API accepts and the names a subject carries are one set.
//
// A name in saml.ReleasableAttributes that no subject produces is a setting an
// administrator can save and that releases nothing. A name produced here and
// missing from the list is an attribute nobody can ask for. Either is a gap
// between two rules written in two places, which is the shape this task keeps
// finding.
func TestTheReleasableAttributesAreExactlyTheOnesASubjectCarries(t *testing.T) {
	carried := availableAttributes("a@example.test", []string{"viewer"},
		sql.NullString{String: "A", Valid: true}, sql.NullString{String: "a", Valid: true})

	var produced []string
	for name := range carried {
		produced = append(produced, name)
	}
	sort.Strings(produced)

	listed := append([]string(nil), saml.ReleasableAttributes...)
	sort.Strings(listed)

	if len(produced) != len(listed) {
		t.Fatalf("a subject carries %v and the API accepts %v", produced, listed)
	}
	for i := range produced {
		if produced[i] != listed[i] {
			t.Errorf("a subject carries %v and the API accepts %v", produced, listed)
			break
		}
	}
}

package application

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/saml"
)

// The contract's attribute enum and the service's releasable list are one set.
//
// The enum is what the console renders as checkboxes and what the public API
// reference documents. saml.ReleasableAttributes is what the service accepts
// and — through a second contract test in internal/samlapi — what a subject
// actually carries. Three places, joined by two tests, so that none of them
// can gain or lose a name on its own.
//
// Read from openapi.yaml rather than from the generated constants. A list of
// constants written out here would not notice a fifth value added to the spec:
// the generator would emit it, this test would not name it, and it would pass.
func TestTheContractEnumIsTheReleasableList(t *testing.T) {
	raw, err := os.ReadFile("../../../openapi/openapi.yaml")
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}

	block := regexp.MustCompile(`(?m)^    SamlAttribute:\n(?:      .*\n)*?      enum: \[([^\]]*)\]`).
		FindSubmatch(raw)
	if block == nil {
		t.Fatal("the contract has no SamlAttribute enum — the schema moved or changed shape, " +
			"and this test would otherwise compare nothing")
	}

	var contract []string
	for _, name := range strings.Split(string(block[1]), ",") {
		contract = append(contract, strings.TrimSpace(name))
	}
	sort.Strings(contract)

	service := append([]string(nil), saml.ReleasableAttributes...)
	sort.Strings(service)

	if strings.Join(contract, ",") != strings.Join(service, ",") {
		t.Errorf("the contract offers %v and the service accepts %v", contract, service)
	}
}

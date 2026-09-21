module github.com/zed378/zed-auth/scripts/acceptance/saml

// The two XML libraries are deliberately NOT pinned to the versions
// backend/go.mod uses. This module exists to disagree with the service when
// the service is wrong, and a verifier locked to exactly the code under test
// shares its bugs by construction. Whichever is newer is the right one here.
//
// Both require Go 1.23 and the staging VM has 1.22 installed. That is not a
// problem and must not be "fixed" by pinning older ones: Go switches
// toolchains automatically for a module that asks for a newer one, which is
// already how `go run ./cmd/passwordhash` works there against a backend
// requiring 1.26.5. Downgrading instead would mean goxmldsig below v1.6.0,
// which carries the GO-2026-4753 signature bypass — an acceptance check that
// might accept a forged signature and report a pass.

go 1.23.0

require (
	github.com/beevik/etree v1.8.0
	github.com/russellhaering/goxmldsig v1.6.1
)

require github.com/jonboulle/clockwork v0.5.0 // indirect

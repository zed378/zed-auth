module github.com/zed378/zed-auth/scripts/acceptance/saml

// The two XML libraries are deliberately NOT pinned to the versions
// backend/go.mod uses. This module exists to disagree with the service when
// the service is wrong, and a verifier locked to exactly the code under test
// shares its bugs by construction. Whichever is newer is the right one here.

go 1.23.0

require (
	github.com/beevik/etree v1.8.0
	github.com/russellhaering/goxmldsig v1.6.1
)

require github.com/jonboulle/clockwork v0.5.0 // indirect

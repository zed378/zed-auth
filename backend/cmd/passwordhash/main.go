// Command passwordhash prints an Argon2id hash for a password.
//
// It exists for one reason: **seeding the first administrator.** There is no
// API that sets a password for somebody else — `P1-19` made that absence
// deliberate, because the only thing that proves an address belongs to a
// person is that they received the link — and there is no API that creates the
// first organization either (`PG-26`). So a brand-new database is seeded with
// SQL, and SQL needs a hash.
//
// It is NOT in the runtime image. `backend/Dockerfile` builds `./cmd/authservice`
// and `./cmd/migrate` by name; this is a developer and CI tool, used by
// `scripts/e2e-up.sh`.
//
// It reads the password from an environment variable rather than from argv,
// because argv is visible in `ps` to every user on the machine and ends up in
// shell history. That matters less for a seeded test account than for a real
// one, and the habit is worth more than the exception.
//
//	PASSWORD='…' go run ./cmd/passwordhash
package main

import (
	"fmt"
	"os"

	"github.com/zed378/zed-auth/backend/internal/authn"
)

func main() {
	password := os.Getenv("PASSWORD")
	if password == "" {
		fmt.Fprintln(os.Stderr, "PASSWORD is required (and is read from the environment, not argv)")
		os.Exit(2)
	}

	hash, err := authn.Hash(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hashing:", err)
		os.Exit(1)
	}

	// No trailing newline: the caller substitutes this straight into SQL, and
	// a stray newline inside a quoted literal is a hash that never matches.
	fmt.Print(hash)
}

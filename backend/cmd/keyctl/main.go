// Command keyctl manages the token signing keys.
//
// Rotation is an operator command rather than a timer, and that is a decision
// rather than an omission (P1-03 step 5). A scheduled rotation that fails at
// 03:00 is worse than a deliberate one at 11:00: the failure modes here —
// a key that cannot be resolved, a consumer whose JWKS cache has not
// refreshed — are ones a person should be watching for. docs/PLAN/09's 90-day
// cadence is the operational expectation, kept in the runbook and in a
// calendar, not in this binary.
//
// Usage:
//
//	keyctl list                  show every key and its state
//	keyctl generate              create a key in `next` and write its private half
//	keyctl rotate                promote `next` to `current`, demote the old one
//	keyctl retire <kid>          remove a `previous` key from JWKS
//	keyctl jwks                  print the published key set
//
// Every command takes -purpose, which selects the key set. `oidc` signs tokens;
// `saml` signs assertions. They are separate key sets because they are
// separately trusted: a service provider pins the SAML certificate out of band
// and would keep trusting it after an OIDC rotation, and a key that signs both
// makes one compromise two.
//
// Runbook: deploy/vm/RUNBOOK-key-rotation.md
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/signing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "keyctl: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	purpose := flag.String("purpose", string(signing.PurposeOIDC),
		"which key set to act on: oidc (signs tokens) or saml (signs assertions)")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		return errors.New("no command given")
	}

	// Named explicitly rather than inferred from the command, because every
	// command here is destructive-adjacent and the two key sets look alike at
	// a glance. `keyctl rotate` against the wrong one is a silent outage for
	// whichever protocol was not meant.
	var set signing.Purpose
	switch signing.Purpose(*purpose) {
	case signing.PurposeOIDC:
		set = signing.PurposeOIDC
	case signing.PurposeSAML:
		set = signing.PurposeSAML
	default:
		return fmt.Errorf("unknown -purpose %q: want oidc or saml", *purpose)
	}

	// The OWNER connection. Rotation is a schema-adjacent operation performed
	// by an operator, not by the running service — the runtime role can read
	// signing_keys and must not be able to promote a key.
	dsn := os.Getenv("AUTH_MIGRATE_DSN")
	if dsn == "" {
		return errors.New("AUTH_MIGRATE_DSN is not set; keyctl connects as the schema owner, " +
			"not as the runtime role")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}

	// Strict permissions on secret files, as the service uses.
	store := signing.NewStore(db, config.NewSecretResolver(true), set)

	switch cmd := flag.Arg(0); cmd {
	case "list":
		return list(ctx, store)
	case "generate":
		return generate(ctx, store, set)
	case "rotate":
		return rotate(ctx, store)
	case "retire":
		if flag.NArg() < 2 {
			return errors.New("retire needs a kid: keyctl retire <kid>")
		}
		return retire(ctx, store, flag.Arg(1))
	case "jwks":
		if set == signing.PurposeSAML {
			// Not an oversight and not a missing feature. SAML has no JWKS:
			// a service provider pins the certificate it read from
			// /saml/metadata, which is why a SAML key carries one and an
			// OIDC key does not. Printing an empty or improvised key set
			// here would suggest an endpoint that does not exist.
			return errors.New("saml keys are published as a certificate in /saml/metadata, not as a JWKS")
		}
		return printJWKS(ctx, store)
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `keyctl — token signing key management

  list              show every key and its state
  generate          create a key in `+"`next`"+` and write its private half
  rotate            promote `+"`next`"+` to `+"`current`"+`, demote the old one to `+"`previous`"+`
  retire <kid>      remove a `+"`previous`"+` key from the published set
  jwks              print the published key set

Flags:
  -purpose oidc|saml   which key set to act on (default oidc)

Environment:
  AUTH_MIGRATE_DSN     required — connects as the schema owner
  AUTH_SECRETS_DIR     where generate writes the private key (default /etc/zed-auth/secrets)
  AUTH_ISSUER          required for -purpose saml — the CommonName in the certificate,
                       and the entity ID service providers will see in the metadata

Rotation is deliberate, not scheduled. See deploy/vm/RUNBOOK-key-rotation.md.
`)
}

func list(ctx context.Context, store *signing.Store) error {
	keys, err := store.List(ctx)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		fmt.Println("No signing keys. Run: keyctl generate && keyctl rotate")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KID\tALG\tSTATUS\tCREATED\tACTIVATED\tRETIRED")

	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			k.KID, k.Algorithm, k.Status,
			formatTime(k.CreatedAt), formatTime(k.ActivatedAt), formatTime(k.RetiredAt))
	}

	return w.Flush()
}

func generate(ctx context.Context, store *signing.Store, set signing.Purpose) error {
	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		return err
	}

	// A SAML key without a certificate is a key nothing can verify: a service
	// provider has no JWKS to look it up in, so the certificate IS the
	// publication. Migration 041 says so with a CHECK, and generating it here
	// rather than in a later step means there is no window in which the row
	// exists and the certificate does not.
	if set == signing.PurposeSAML {
		issuer := strings.TrimSpace(os.Getenv("AUTH_ISSUER"))
		if issuer == "" {
			return errors.New("AUTH_ISSUER is not set; a SAML certificate names the entity ID " +
				"that service providers will pin, and guessing it would produce a certificate " +
				"nobody can match")
		}
		certificate, err := saml.SelfSignedCertificate(pair, issuer, time.Now())
		if err != nil {
			return err
		}
		pair.CertificatePEM = certificate
	}

	dir := os.Getenv("AUTH_SECRETS_DIR")
	if dir == "" {
		dir = "/etc/zed-auth/secrets"
	}

	// The filename is built from a thumbprint, and the check below makes that
	// an enforced property rather than an assumption.
	//
	// gosec flagged this as path traversal (G703) because `dir` comes from the
	// environment and the name is concatenated. Today neither part can escape:
	// `dir` is operator configuration, and a kid is a base64url-encoded
	// SHA-256 thumbprint, whose alphabet contains no separator or dot. But
	// "today the value happens to be safe" is not a property — it is an
	// observation about code that could change, and the whole point of
	// deriving the kid was that nobody chooses it.
	if !isBase64URL(pair.KID) {
		return fmt.Errorf("refusing to write a key file: kid %q is not base64url", pair.KID)
	}

	// The prefix names the key set, so an operator listing the secrets
	// directory can tell which file signs what without opening it.
	prefix := "jwt-signing-"
	if set == signing.PurposeSAML {
		prefix = "saml-signing-"
	}

	path := filepath.Join(dir, prefix+pair.KID+".pem")

	// Belt and braces: the joined path must still be inside the directory.
	// This catches a `dir` containing something unexpected as well.
	if cleaned := filepath.Clean(path); !strings.HasPrefix(cleaned, filepath.Clean(dir)+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write outside %s", dir)
	}

	// 0400, and written before the row is inserted.
	//
	// The order matters: a row referencing a file that does not exist makes
	// the service refuse to start, while a file with no row is inert. If one
	// of the two has to fail, it should be the harmless one.
	// #nosec G703 -- the two checks above bound this: `pair.KID` is validated
	// against the base64url alphabet (no separator, no dot), and the joined
	// path is asserted to remain inside `dir`. gosec's taint analysis does not
	// follow either. The validation is the fix; this annotation records the
	// invariant so a reviewer can check the claim in the ten lines above it.
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		return fmt.Errorf("writing the private key: %w", err)
	}

	if err := store.Insert(ctx, pair, "file:"+path); err != nil {
		return fmt.Errorf("recording the key (its private half is at %s and can be deleted): %w",
			path, err)
	}

	fmt.Printf("Generated %s %s key %s\n", set, pair.Algorithm, pair.KID)
	fmt.Printf("  private key: %s (mode 0400)\n", path)
	if set == signing.PurposeSAML {
		fmt.Printf("  state:       next — certificate published in /saml/metadata, not yet signing\n\n")
	} else {
		fmt.Printf("  state:       next — published in JWKS, not yet signing\n\n")
	}
	fmt.Printf("The key must be owned by the service uid before it can be read:\n")
	fmt.Printf("  sudo %s/../deploy/vm/secrets.sh fix\n\n", dir)
	fmt.Printf("Give consumers time to fetch it, then: keyctl rotate\n")
	return nil
}

func rotate(ctx context.Context, store *signing.Store) error {
	result, err := store.Rotate(ctx)
	if err != nil {
		if errors.Is(err, signing.ErrNoNextKey) {
			return errors.New("no key in the `next` state. Run `keyctl generate` first, " +
				"and give consumers time to fetch it before rotating")
		}
		return err
	}

	fmt.Printf("Rotated.\n")
	fmt.Printf("  now signing:  %s\n", result.Promoted)
	if result.Demoted != "" {
		fmt.Printf("  now previous: %s (still verifies; retire it after a full token lifetime)\n",
			result.Demoted)
	} else {
		fmt.Printf("  (nothing to demote — this was the first rotation)\n")
	}
	fmt.Printf("\nRunning instances pick this up within the key cache TTL (%s).\n",
		signing.DefaultCacheTTL)
	return nil
}

func retire(ctx context.Context, store *signing.Store, kid string) error {
	if err := store.Retire(ctx, kid); err != nil {
		return err
	}

	fmt.Printf("Retired %s.\n", kid)
	fmt.Printf("Tokens signed by it no longer verify. This is not reversible in effect:\n")
	fmt.Printf("any token still held by a client that was signed by this key is now dead.\n")
	return nil
}

func printJWKS(ctx context.Context, store *signing.Store) error {
	set, err := store.Load(ctx)
	if err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(set.JWKS(), "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JWKS: %w", err)
	}

	fmt.Println(string(encoded))
	return nil
}

// isBase64URL reports whether s uses only the base64url alphabet.
//
// No separator, no dot, so a value that passes cannot traverse a path.
func isBase64URL(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func formatTime(t sql.NullTime) string {
	if !t.Valid {
		return "-"
	}
	return t.Time.UTC().Format("2006-01-02 15:04")
}

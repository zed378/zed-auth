//go:build integration

package samlapi

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// What /saml/metadata publishes, through the real key store (P4-09).
//
// This test exists because the first version of Published did not work and
// every test passed anyway. It asked signing.Key.Signer for each key, and that
// accessor deliberately refuses anything that is not `current` — so every
// `next` key failed the request and was skipped, the document went back to
// naming one certificate, and the rotation overlap the whole change was for
// did not exist.
//
// Nothing caught it. The metadata test called saml.Metadata directly with two
// keys; the handler test used a fake Keys. The halves were tested and the join
// between them was not, which is the same shape as the three defects P4-08
// shipped to staging.
//
// So this drives the real CachedKeys over a real signing.Store.

type samlSecrets struct {
	dir      string
	resolver config.SecretResolver
}

func newSamlSecrets(t *testing.T) *samlSecrets {
	t.Helper()
	return &samlSecrets{dir: t.TempDir(), resolver: config.NewSecretResolver(true)}
}

func (s *samlSecrets) store(t *testing.T, kid, pem string) string {
	t.Helper()
	path := filepath.Join(s.dir, kid+".pem")
	if err := os.WriteFile(path, []byte(pem), 0o400); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	return "file:" + path
}

func (s *samlSecrets) Resolve(ref config.SecretRef) ([]byte, error) { return s.resolver.Resolve(ref) }

// samlKeyStore opens the owner connection and returns a store for SAML keys.
func samlKeyStore(t *testing.T, stack *testsupport.Stack, secrets *samlSecrets) (*signing.Store, *sql.DB) {
	t.Helper()

	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("opening the owner connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return signing.NewStore(db, secrets, signing.PurposeSAML), db
}

// generateSamlKey creates a SAML key with its certificate, as keyctl does.
func generateSamlKey(t *testing.T, store *signing.Store, secrets *samlSecrets) string {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	certificate, err := saml.SelfSignedCertificate(pair, "https://auth.example.test", time.Now())
	if err != nil {
		t.Fatalf("certifying the key: %v", err)
	}
	pair.CertificatePEM = certificate

	ref := secrets.store(t, pair.KID, pair.PrivatePEM)
	if err := store.Insert(context.Background(), pair, ref); err != nil {
		t.Fatalf("recording the key: %v", err)
	}
	return pair.KID
}

func TestPublishedCarriesTheKeyThatWillSignAsWellAsTheOneThatDoes(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)
	secrets := newSamlSecrets(t)
	store, _ := samlKeyStore(t, stack, secrets)

	ctx := context.Background()

	// The first key, promoted to current, as the first rollout does it.
	signingKID := generateSamlKey(t, store, secrets)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("promoting the first key: %v", err)
	}

	// The second, generated and NOT yet promoted. This is the state a
	// rotation is in while service providers are being given the new
	// certificate, and the only state in which the overlap matters.
	nextKID := generateSamlKey(t, store, secrets)

	keys := NewCachedKeys(signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(ctx)
	}, time.Minute))

	published, err := keys.Published(ctx)
	if err != nil {
		t.Fatalf("Published: %v", err)
	}

	if len(published) != 2 {
		t.Fatalf("%d certificate(s) published, want 2 — a service provider cannot trust "+
			"a key the document does not carry, so the rotation is an outage that waits on email",
			len(published))
	}

	// The one that signs comes first, and it is the one the issuing path uses.
	current, err := keys.SAML(ctx)
	if err != nil {
		t.Fatalf("SAML: %v", err)
	}
	signingCert, err := current.Certificate()
	if err != nil {
		t.Fatalf("current certificate: %v", err)
	}
	if !published[0].Equal(signingCert) {
		t.Error("the first published certificate is not the one assertions are signed with")
	}
	if published[0].Equal(published[1]) {
		t.Error("both published certificates are the same, so there is no overlap")
	}

	_ = signingKID
	_ = nextKID
}

// A demoted key is not published.
//
// This service issues rather than consumes: a `previous` key verifies nothing
// anybody is asking about, and publishing it would keep it trusted after it
// stopped being used.
func TestPublishedLeavesOutADemotedKey(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)
	secrets := newSamlSecrets(t)
	store, _ := samlKeyStore(t, stack, secrets)

	ctx := context.Background()

	generateSamlKey(t, store, secrets)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("promoting the first key: %v", err)
	}
	// A second rotation demotes the first to `previous`.
	generateSamlKey(t, store, secrets)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("second rotation: %v", err)
	}

	keys := NewCachedKeys(signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(ctx)
	}, time.Minute))

	published, err := keys.Published(ctx)
	if err != nil {
		t.Fatalf("Published: %v", err)
	}

	// Exactly the one that signs. There is no `next` at this point, and the
	// demoted key must not appear.
	if len(published) != 1 {
		t.Fatalf("%d certificate(s) published, want 1 — a demoted key is still being advertised",
			len(published))
	}

	current, err := keys.SAML(ctx)
	if err != nil {
		t.Fatalf("SAML: %v", err)
	}
	signingCert, err := current.Certificate()
	if err != nil {
		t.Fatalf("current certificate: %v", err)
	}
	if !published[0].Equal(signingCert) {
		t.Error("the published certificate is not the one assertions are signed with")
	}
}

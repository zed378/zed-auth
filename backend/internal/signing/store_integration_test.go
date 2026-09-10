//go:build integration

package signing

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

// fileResolver writes generated private keys to a temp directory and resolves
// them back, standing in for the secret manager (P0-14).
//
// Real files with real permissions rather than a map: the production resolver
// refuses a key file that is group- or world-readable, and a map-backed fake
// would skip the check that has already caused one outage on this project.
type fileResolver struct {
	dir      string
	resolver config.SecretResolver
}

func newFileResolver(t *testing.T) *fileResolver {
	t.Helper()
	return &fileResolver{
		dir: t.TempDir(),
		// Strict permissions, as in production.
		resolver: config.NewSecretResolver(true),
	}
}

func (f *fileResolver) store(t *testing.T, kid, pem string) string {
	t.Helper()

	path := filepath.Join(f.dir, kid+".pem")
	if err := os.WriteFile(path, []byte(pem), 0o400); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	return "file:" + path
}

func (f *fileResolver) Resolve(ref config.SecretRef) ([]byte, error) {
	return f.resolver.Resolve(ref)
}

func openDB(t *testing.T, stack *testsupport.Stack) *sql.DB {
	t.Helper()

	// The owner connection: signing_keys is instance-level, has no org_id and
	// no RLS policy, and the rotation command runs as an operator.
	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newStore returns a store with one generated key already in `next`.
func newStore(t *testing.T) (*Store, *fileResolver, *testsupport.Stack) {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	files := newFileResolver(t)
	store := NewStore(openDB(t, stack), files, PurposeOIDC)
	return store, files, stack
}

// addKey generates a key, writes its private half, and records it as `next`.
func addKey(t *testing.T, store *Store, files *fileResolver, alg Algorithm) *KeyPair {
	t.Helper()

	pair, err := Generate(alg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	ref := files.store(t, pair.KID, pair.PrivatePEM)

	if err := store.Insert(context.Background(), pair, ref); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return pair
}

// P1-03's Definition of Done, and the reason the four-state model exists:
// a token issued before a rotation must still validate after it.
//
// The unit test proves the key set behaves correctly given the right states.
// This proves the DATABASE produces those states — that Rotate actually moves
// current to previous and next to current, in a real transaction, against the
// real constraints.
func TestRotationKeepsOlderTokensValid(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	addKey(t, store, files, RS256)

	// First rotation: nothing to demote, the next key starts signing.
	first, err := store.Rotate(ctx)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	if first.Demoted != "" {
		t.Errorf("the first rotation should demote nothing, demoted %q", first.Demoted)
	}

	cache := NewCache(func() (*KeySet, error) { return store.Load(ctx) }, 0)

	token, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_before_rotation"}`))
	if err != nil {
		t.Fatalf("signing before rotation: %v", err)
	}

	// A second key, then rotate onto it.
	addKey(t, store, files, RS256)
	second, err := store.Rotate(ctx)
	if err != nil {
		t.Fatalf("second rotation: %v", err)
	}
	if second.Demoted != first.Promoted {
		t.Errorf("rotation demoted %q, expected the previously current key %q",
			second.Demoted, first.Promoted)
	}

	cache.Invalidate()

	// The property that matters.
	if _, err := NewVerifier(cache).Verify(token, TypeJWT); err != nil {
		t.Fatalf("a token signed before the rotation must still verify: %v", err)
	}

	// And the new key is the one signing now.
	kid, err := NewSigner(cache).CurrentKID()
	if err != nil {
		t.Fatalf("current kid: %v", err)
	}
	if kid != second.Promoted {
		t.Errorf("signing with %q, expected the newly promoted %q", kid, second.Promoted)
	}
}

// Retirement ends a key's life, and only a `previous` key can be retired.
func TestRetireRemovesAKeyFromVerification(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	addKey(t, store, files, RS256)
	first, _ := store.Rotate(ctx)

	cache := NewCache(func() (*KeySet, error) { return store.Load(ctx) }, 0)
	token, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	addKey(t, store, files, RS256)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	cache.Invalidate()

	// Still valid while previous.
	if _, err := NewVerifier(cache).Verify(token, TypeJWT); err != nil {
		t.Fatalf("token should verify while its key is previous: %v", err)
	}

	if err := store.Retire(ctx, first.Promoted); err != nil {
		t.Fatalf("retire: %v", err)
	}
	cache.Invalidate()

	if _, err := NewVerifier(cache).Verify(token, TypeJWT); err == nil {
		t.Fatal("a token signed by a retired key must stop verifying")
	}
}

// Only a `previous` key may be retired. Retiring the current key would leave
// nothing signing; retiring a `next` key would discard one consumers may
// already have cached.
func TestOnlyPreviousKeysCanBeRetired(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	next := addKey(t, store, files, RS256)

	if err := store.Retire(ctx, next.KID); err == nil {
		t.Error("retiring a next key should fail")
	}

	rotation, _ := store.Rotate(ctx)
	if err := store.Retire(ctx, rotation.Promoted); err == nil {
		t.Error("retiring the current key should fail")
	}
}

// The database refuses two current keys. This is the partial unique index
// doing real work: "which key signed this token" must be deterministic, and
// making the alternative unrepresentable beats checking for it.
func TestDatabaseRefusesTwoCurrentKeys(t *testing.T) {
	store, files, stack := newStore(t)
	ctx := context.Background()

	addKey(t, store, files, RS256)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	second := addKey(t, store, files, RS256)

	db := openDB(t, stack)
	_, err := db.ExecContext(ctx,
		`UPDATE signing_keys SET status = 'current' WHERE kid = $1`, second.KID)

	if err == nil {
		t.Fatal("the database must refuse a second current key for one purpose")
	}
}

// A key set that cannot be assembled is a startup failure, not a degraded
// mode. A service that starts without a signing key accepts requests and fails
// every login, which is a worse outage and a much harder one to diagnose.
func TestLoadFailsWhenThePrivateKeyIsUnreadable(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	pair, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// A reference pointing at nothing.
	ref := "file:" + filepath.Join(files.dir, "absent.pem")
	if err := store.Insert(ctx, pair, ref); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if _, err := store.Load(ctx); err == nil {
		t.Fatal("loading must fail when the current key's private half cannot be resolved")
	}
}

// The schema refuses PEM material in the reference column (P0-07). That
// constraint is the one stopping the "simplification" that would violate
// docs/PLAN/02 § Constraints while every test still passed.
func TestSchemaRefusesPrivateKeyMaterialAsAReference(t *testing.T) {
	store, _, _ := newStore(t)

	pair, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The mistake: storing the key instead of a reference to it.
	err = store.Insert(context.Background(), pair, pair.PrivatePEM)
	if err == nil {
		t.Fatal("SECURITY: the schema accepted private key material in private_key_ref")
	}
}

// Restarting must not change the key set (P1-03 Definition of Done). A service
// that generated a key at startup would pass every other test here and break
// every consumer on every deploy.
func TestKeySetSurvivesReload(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	addKey(t, store, files, RS256)
	if _, err := store.Rotate(ctx); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	first, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	firstKey, _ := first.Current()

	// A second Store over the same database, as a restarted process would be.
	second, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	secondKey, _ := second.Current()

	if firstKey.KID != secondKey.KID {
		t.Errorf("the current kid changed across loads: %q then %q", firstKey.KID, secondKey.KID)
	}
}

// Rotation with nothing to promote is an error, not a silent no-op. An
// operator who runs the command expecting a new key and gets silence would
// believe the rotation happened.
func TestRotateWithoutANextKeyFails(t *testing.T) {
	store, _, _ := newStore(t)

	if _, err := store.Rotate(context.Background()); err == nil {
		t.Fatal("rotating with no next key must fail")
	}
}

// List is what the operator sees. Untested, it is the command most likely to
// misreport during an incident — which is exactly when someone is reading it
// to decide whether a rotation worked.
func TestListReportsEveryKeyAndItsTimestamps(t *testing.T) {
	store, files, _ := newStore(t)
	ctx := context.Background()

	addKey(t, store, files, RS256)
	first, err := store.Rotate(ctx)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	addKey(t, store, files, ES256)
	second, err := store.Rotate(ctx)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("list returned %d keys, want 2", len(keys))
	}

	byKID := map[string]KeyInfo{}
	for _, k := range keys {
		byKID[k.KID] = k
	}

	demoted, ok := byKID[first.Promoted]
	if !ok {
		t.Fatalf("the demoted key %q is missing from the list", first.Promoted)
	}
	if demoted.Status != string(StatusPrevious) {
		t.Errorf("demoted key status = %q, want previous", demoted.Status)
	}
	if !demoted.ActivatedAt.Valid {
		t.Error("a key that has been current must carry an activated_at")
	}
	if demoted.RetiredAt.Valid {
		t.Error("a previous key must not carry a retired_at")
	}

	current, ok := byKID[second.Promoted]
	if !ok {
		t.Fatalf("the promoted key %q is missing from the list", second.Promoted)
	}
	if current.Status != string(StatusCurrent) {
		t.Errorf("promoted key status = %q, want current", current.Status)
	}
	if current.Algorithm != string(ES256) {
		t.Errorf("algorithm = %q, want ES256 — the list must report what was stored", current.Algorithm)
	}

	// Retiring sets the timestamp, so the operator can see when a key's
	// tokens stopped being accepted.
	if err := store.Retire(ctx, first.Promoted); err != nil {
		t.Fatalf("retire: %v", err)
	}
	after, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list after retire: %v", err)
	}
	for _, k := range after {
		if k.KID == first.Promoted {
			if k.Status != string(StatusRetired) || !k.RetiredAt.Valid {
				t.Errorf("retired key reports status=%q retired_at.valid=%v",
					k.Status, k.RetiredAt.Valid)
			}
		}
	}
}

//go:build integration

package saml

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/testsupport"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The SAML key is a real row, separate from the OIDC one (P4-07 F-1, A-7).
//
// The separation has been a schema property since `P1-03` — `purpose IN
// ('oidc','saml')` with one `current` each — and this is the first code that
// uses it. A test is worth more than the constraint alone, because the
// constraint says two keys CAN coexist and this says the service reads the one
// it asked for.

// ownerDB connects as the schema owner: signing_keys is instance-level, has no
// org_id and no RLS policy, and key management is an operator's job.
func ownerDB(t *testing.T, stack *testsupport.Stack) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("opening the owner connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// resolver reads a private key straight from the reference, for tests.
type resolver struct{ material map[string][]byte }

func (r resolver) Resolve(ref config.SecretRef) ([]byte, error) {
	return r.material[string(ref)], nil
}

func TestASAMLKeyIsStoredWithItsCertificateAndIsNotTheOIDCKey(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	db := ownerDB(t, stack)
	files := resolver{material: map[string][]byte{}}

	// An OIDC key, exactly as P1-03 creates one: no certificate.
	oidcPair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating the oidc key: %v", err)
	}
	files.material["file:oidc"] = []byte(oidcPair.PrivatePEM)
	oidc := signing.NewStore(db, files, signing.PurposeOIDC)
	if err := oidc.Insert(context.Background(), oidcPair, "file:oidc"); err != nil {
		t.Fatalf("inserting the oidc key: %v", err)
	}

	// A SAML key, which must carry a certificate or the database refuses it.
	samlPair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating the saml key: %v", err)
	}
	certPEM, err := SelfSignedCertificate(samlPair, testIssuer, time.Now())
	if err != nil {
		t.Fatalf("certifying: %v", err)
	}
	samlPair.CertificatePEM = certPEM
	files.material["file:saml"] = []byte(samlPair.PrivatePEM)
	saml := signing.NewStore(db, files, signing.PurposeSAML)
	if err := saml.Insert(context.Background(), samlPair, "file:saml"); err != nil {
		t.Fatalf("inserting the saml key: %v", err)
	}

	// Each store sees its own purpose and nothing else.
	oidcSet, err := oidc.Load(context.Background())
	if err != nil {
		t.Fatalf("loading oidc keys: %v", err)
	}
	samlSet, err := saml.Load(context.Background())
	if err != nil {
		t.Fatalf("loading saml keys: %v", err)
	}

	if oidcSet.Len() != 1 {
		t.Errorf("the oidc store loaded %d keys — it must not see the saml one", oidcSet.Len())
	}
	if samlSet.Len() != 1 {
		t.Errorf("the saml store loaded %d keys — it must not see the oidc one", samlSet.Len())
	}

	oidcKey, err := oidcSet.ByKID(oidcPair.KID)
	if err != nil {
		t.Fatalf("the oidc store cannot find its own key: %v", err)
	}
	samlKeyRow, err := samlSet.ByKID(samlPair.KID)
	if err != nil {
		t.Fatalf("the saml store cannot find its own key: %v", err)
	}

	// Neither store can reach the other's key, which is the separation the
	// schema promises and this is the first code to depend on.
	if _, err := oidcSet.ByKID(samlPair.KID); err == nil {
		t.Error("the oidc key set resolved the SAML key")
	}
	if _, err := samlSet.ByKID(oidcPair.KID); err == nil {
		t.Error("the saml key set resolved the OIDC key")
	}

	if samlKeyRow.CertificatePEM == "" {
		t.Error("the saml key came back with no certificate")
	}
	if oidcKey.CertificatePEM != "" {
		t.Errorf("the oidc key carries a certificate %q — it publishes JWKS and needs none", oidcKey.CertificatePEM)
	}
}

// The database refuses a SAML key with no certificate, so the failure happens at
// the insert rather than at the first login.
func TestASAMLKeyWithoutACertificateIsRefused(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	db := ownerDB(t, stack)
	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	files := resolver{material: map[string][]byte{"file:saml": []byte(pair.PrivatePEM)}}

	store := signing.NewStore(db, files, signing.PurposeSAML)
	if err := store.Insert(context.Background(), pair, "file:saml"); err == nil {
		t.Error("a saml key with no certificate was inserted — nothing could verify what it signs")
	}
}

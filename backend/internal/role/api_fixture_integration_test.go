//go:build integration

package role

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const issuer = "https://auth.example.test"

// setupAPI builds the whole /v1 chain, because the things this file asserts —
// the policy entry, the scope, the 403-versus-404 split — live in the chain
// rather than in the handler. A test that called the handler directly would
// pass with no policy entry at all, which is precisely the bug it should fail
// on.
func setupAPI(t *testing.T) *apiFixture {
	t.Helper()

	base := setup(t)

	// A second project in organization A, and one in organization B. Two
	// projects in ONE organization is where a PROJECT_OWNER's narrowness
	// shows; the third is for the cross-tenant case.
	otherProject := base.projectA2
	projectB := base.projectB

	stack := testsupport.Start(t)
	keys := signingKeys(t, stack)
	signer := signing.NewSigner(keys)

	var consoleProject, clientID string
	base.factory.QueryRow(&consoleProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, base.orgA)
	base.factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, consoleProject, base.orgA)

	// Three callers, each with exactly the grants their name implies.
	adminUser := base.factory.User(base.orgA)
	base.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`,
		adminUser, base.orgA)

	ownerUser := base.factory.User(base.orgA)
	base.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'PROJECT_OWNER', $2)`,
		ownerUser, base.projectA)

	// No manager role at all. A perfectly valid token, and nothing else.
	memberUser := base.factory.User(base.orgA)

	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(base.db, discard(), nil)
	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: base.db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 300, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(base.db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}, httpserver.Deps{
		Logger: discard(),
		Health: &httpserver.Health{},
		V1:     chain,
		Organizations: &organization.Handler{
			Store: organization.NewStore(), DB: base.db, Audit: auditor, Log: discard(),
		},
		ProjectAPI:     &project.Handler{Store: project.NewStore(), DB: base.db, Audit: auditor, Log: discard()},
		ApplicationAPI: application.New(base.db, auditor, discard()),
		RoleAPI:        New(base.db, auditor, discard()),
		GrantAPI:       grant.New(base.db, auditor, discard()),
		UserAPI:        &user.Handler{Store: user.NewStore(), DB: base.db, Audit: auditor, Log: discard()},
		AuditAPI:       &auditlog.Handler{DB: base.db, Log: discard()},
	})

	mint := func(userID string) string {
		t.Helper()
		claims, err := token.AccessTokenClaims(token.Subject{
			Issuer: issuer, Audience: issuer, ClientID: clientID,
			OrgID: base.orgA, UserID: userID, Scope: []string{"openid"},
		}, time.Now())
		if err != nil {
			t.Fatalf("claims: %v", err)
		}
		payload, err := claims.Encode()
		if err != nil {
			t.Fatalf("encoding: %v", err)
		}
		signed, err := signer.SignWithType(payload, signing.TypeAccessToken)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		return signed
	}

	return &apiFixture{
		fixture:           base,
		handler:           srv.Handler(),
		adminUser:         adminUser,
		adminToken:        mint(adminUser),
		projectOwnerToken: mint(ownerUser),
		memberToken:       mint(memberUser),
		otherProject:      otherProject,
		projectB:          projectB,
	}
}

func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

var _ http.Handler = (http.Handler)(nil)

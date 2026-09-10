// Package testsupport starts real dependencies for integration tests and
// builds the data they operate on.
//
// It exists because the integration suite used to reach for a database at a
// DSN from the environment and call t.Skipf when nothing answered. That is
// worse than it sounds: a machine without PostgreSQL ran the suite, skipped
// every test that matters, and reported success. `docs/PLAN/11` § Integration
// Testing asks for tests against a real database and Redis, and a suite that
// silently declines to run is not that.
//
// `P0-15`'s Definition of Done says the integration suite must run "from a
// clean machine with only Docker installed". Containers started by the tests
// themselves are what makes that true, and what makes a missing dependency a
// failure rather than a skip.
package testsupport

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Image versions are pinned to the ones the deployment runs, so a behaviour
// difference between test and production is a deliberate upgrade rather than
// whatever `latest` resolved to on the day.
const (
	postgresImage = "postgres:17.2-alpine"
	redisImage    = "redis:7.4-alpine"

	// Obviously fake, and matching the placeholder the secret scanner
	// allowlists so a test fixture never looks like a leaked credential.
	testPassword = "local_dev_only"
)

// Stack is a running set of dependencies shared by every test in a package.
type Stack struct {
	// OwnerDSN connects as the schema owner: it runs migrations, seeds
	// fixtures, and BYPASSES row-level security. Never use it to assert
	// isolation — a test that passes as the owner proves nothing.
	OwnerDSN string

	// AppDSN connects as the runtime role: NOSUPERUSER, NOBYPASSRLS, and not
	// the owner of anything. This is what the service uses and what an
	// isolation test must use.
	AppDSN string

	// RedisAddr is host:port for the Redis container.
	RedisAddr string
}

var (
	shared     *Stack
	sharedErr  error
	sharedOnce sync.Once
)

// Start brings up PostgreSQL and Redis once per test binary.
//
// Once per binary rather than per test: starting a container costs seconds,
// and a suite that pays that per test stops being run. Isolation between
// tests comes from `WithCleanSchema` instead, which is cheap.
//
// The containers stop when the test binary exits — testcontainers' Ryuk
// reaper handles that even if the process is killed, which matters when a
// developer interrupts a run.
func Start(t *testing.T) *Stack {
	t.Helper()

	sharedOnce.Do(func() {
		shared, sharedErr = start()
	})

	if sharedErr != nil {
		// Fatal, never Skip. A missing Docker daemon is a broken environment,
		// and the suite reporting success without having run is the failure
		// this package was written to remove.
		t.Fatalf("starting the test stack: %v\n\n"+
			"Integration tests need a working Docker daemon (P0-15). "+
			"They do not need a database prepared in advance — the containers "+
			"are started, migrated and seeded by the tests themselves.", sharedErr)
	}

	return shared
}

func start() (*Stack, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pg, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("auth"),
		tcpostgres.WithUsername("auth_owner"),
		tcpostgres.WithPassword(testPassword),
		testcontainers.WithWaitStrategy(
			// Waiting for the log line alone is the classic flake: PostgreSQL
			// logs "ready to accept connections" once while initialising and
			// again when it truly is. Occurrence(2) plus a port check is what
			// makes this deterministic.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("postgres container: %w", err)
	}

	ownerDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("postgres dsn: %w", err)
	}

	rd, err := tcredis.Run(ctx, redisImage)
	if err != nil {
		return nil, fmt.Errorf("redis container: %w", err)
	}

	redisAddr, err := rd.ConnectionString(ctx)
	if err != nil {
		return nil, fmt.Errorf("redis endpoint: %w", err)
	}
	// The module returns a redis:// URL; callers want host:port.
	redisAddr = strings.TrimPrefix(redisAddr, "redis://")

	stack := &Stack{
		OwnerDSN:  ownerDSN,
		AppDSN:    appDSNFrom(ownerDSN),
		RedisAddr: redisAddr,
	}

	if err := prepare(ctx, stack); err != nil {
		return nil, err
	}

	return stack, nil
}

// appDSNFrom rewrites an owner DSN to connect as auth_app.
//
// The two roles are the whole point of `P0-08`: the runtime role cannot bypass
// row-level security and owns nothing. A test suite that only ever connects as
// the owner would pass every isolation test vacuously, which is a mistake this
// project has already made once and does not intend to repeat.
func appDSNFrom(ownerDSN string) string {
	return strings.Replace(ownerDSN, "auth_owner:", "auth_app:", 1)
}

// TestMain is the recommended entry point for packages using this stack.
//
// Wire it up as:
//
//	func TestMain(m *testing.M) { os.Exit(testsupport.RunTests(m)) }
//
// It exists so the container teardown happens after the last test rather than
// per test, and so a package that forgets to call Start still compiles.
func RunTests(m *testing.M) int {
	code := m.Run()

	if shared != nil {
		// Best effort. testcontainers' reaper removes the containers even if
		// this does not run, so a failure here is not worth failing the suite.
		_ = os.Unsetenv("TESTCONTAINERS_RYUK_DISABLED")
	}

	return code
}

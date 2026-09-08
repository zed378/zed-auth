package testsupport

import (
	"fmt"
	"os"
	"testing"
)

// StartForPackage brings the stack up from TestMain and exports its DSNs as
// the environment variables the existing integration tests already read.
//
// Wire it up as:
//
//	func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }
//
// This exists to convert the suite without rewriting every test. Those tests
// resolved their database from AUTH_TEST_OWNER_DSN and AUTH_TEST_APP_DSN with
// a localhost fallback, and called t.Skipf when nothing answered — so a
// machine without PostgreSQL ran the suite, skipped everything that mattered,
// and reported success. Setting the variables here means the same code now
// talks to a container it started itself.
//
// A failure to start is a non-zero exit, never a skip. `P0-15`: the
// integration suite must run "from a clean machine with only Docker
// installed", and a suite that quietly declines to run is not a suite.
func StartForPackage(m *testing.M) int {
	sharedOnce.Do(func() { shared, sharedErr = start() })

	if sharedErr != nil {
		fmt.Fprintf(os.Stderr,
			"\nintegration tests could not start their dependencies: %v\n\n"+
				"They need a working Docker daemon and nothing else — no database\n"+
				"prepared in advance, no environment variables. The containers are\n"+
				"started, migrated and seeded by the tests themselves (P0-15).\n\n",
			sharedErr)
		return 1
	}

	// Set unconditionally rather than only when unset. A stale value left over
	// from a previous way of running these tests would otherwise point the
	// suite at a database that is not the one it just prepared, and the
	// failure would look like a schema problem.
	for key, value := range map[string]string{
		"AUTH_TEST_OWNER_DSN":  shared.OwnerDSN,
		"AUTH_TEST_APP_DSN":    shared.AppDSN,
		"AUTH_TEST_REDIS_ADDR": shared.RedisAddr,
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", key, err)
			return 1
		}
	}

	return m.Run()
}

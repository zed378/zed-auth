//go:build integration

// The authorization cache (P2-07).
//
// A cache in front of an authorization decision is a correctness risk wearing a
// performance improvement's clothes, so these tests are about the two ways it
// can be wrong: serving a revoked permission, and turning a cache outage into
// an allow.
package authz

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Proactive invalidation, end to end: a revocation is honoured immediately,
// not after the TTL.
//
// The TTL is 30 seconds and this test does not wait, so the only thing that can
// make it pass is the invalidation actually landing.
func TestARevocationIsInvalidatedRatherThanWaitedOut(t *testing.T) {
	f := setupCached(t)

	f.grantRole(t, f.subject, "approver")

	if got := f.check(t, f.subject, "approve", "purchase_request"); !got.Allowed {
		t.Fatalf("the granted permission was denied: %+v", got)
	}
	// Now cached. Confirmed, so the revocation below has something to invalidate.
	if _, hit := f.cache.RoleKeys(context.Background(), f.orgA, f.subject, f.projectA); !hit {
		t.Fatal("the grant was not cached, so this test proves nothing about invalidation")
	}

	// Through the API, which is what invalidates.
	rec := f.raw(t, f.subject, "approve", "purchase_request")
	if rec.Code != 200 {
		t.Fatalf("precondition: %d", rec.Code)
	}
	f.revokeThroughAPI(t, f.subject, f.projectA)

	if _, hit := f.cache.RoleKeys(context.Background(), f.orgA, f.subject, f.projectA); hit {
		t.Error("the cache entry survived a revocation; the TTL is now the revocation window")
	}

	after := f.check(t, f.subject, "approve", "purchase_request")
	if after.Allowed {
		t.Error("a revoked role was still honoured from cache")
	}
}

// Changing what a role CARRIES invalidates the project's definitions — one
// delete, whatever the number of users holding it.
func TestChangingARolesPermissionsInvalidatesTheProject(t *testing.T) {
	f := setupCached(t)
	f.grantRole(t, f.subject, "approver")

	if got := f.check(t, f.subject, "approve", "purchase_request"); !got.Allowed {
		t.Fatal("precondition: the permission was not granted")
	}
	if _, hit := f.cache.Roles(context.Background(), f.orgA, f.projectA); !hit {
		t.Fatal("the role definitions were not cached")
	}

	// Take the permission away from the role itself, through the API.
	f.narrowRoleThroughAPI(t, "approver")

	if _, hit := f.cache.Roles(context.Background(), f.orgA, f.projectA); hit {
		t.Error("the role definitions survived an edit to what the role carries")
	}

	after := f.check(t, f.subject, "approve", "purchase_request")
	if after.Allowed {
		t.Error("a permission removed from a role was still honoured from cache")
	}
}

// `P2-07` step 6: a cache backend failure falls through to the database rather
// than failing the check — and never to an allow.
//
// Both halves are asserted, because a cache outage that denied everything would
// also satisfy "never allow" while being an outage of its own.
func TestACacheOutageFallsThroughToTheDatabase(t *testing.T) {
	f := setupCached(t)
	f.grantRole(t, f.subject, "approver")

	// Point the cache at a port with nothing on it.
	f.cache.client = redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond,
		ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond,
	})

	allowed := f.check(t, f.subject, "approve", "purchase_request")
	if !allowed.Allowed {
		t.Error("a cache outage denied a permission the subject holds — it should fall through to the database")
	}

	// And a permission they do not hold is still denied, so the fall-through
	// is a real read rather than a blanket answer.
	denied := f.check(t, f.subject, "delete", "purchase_request")
	if denied.Allowed {
		t.Error("a cache outage allowed a permission the subject does not hold")
	}
}

// An entry carries when it was written, so the age it was served at can be
// observed. A TTL is an upper bound anybody can read off a constant; this is
// what was actually served.
func TestACachedEntryReportsItsAge(t *testing.T) {
	f := setupCached(t)
	f.grantRole(t, f.subject, "approver")

	observer := &recordingObserver{}
	f.cache.Observer = observer

	// Warm it, then read it.
	f.check(t, f.subject, "approve", "purchase_request")
	f.check(t, f.subject, "approve", "purchase_request")

	if observer.hits == 0 {
		t.Fatal("no cache hit was recorded")
	}
	if len(observer.ages) == 0 {
		t.Error("a cache hit recorded no entry age, so staleness is unobservable")
	}
	for _, age := range observer.ages {
		if age < 0 || age > DefaultTTL {
			t.Errorf("an entry was served at age %s, outside [0, %s]", age, DefaultTTL)
		}
	}
}

type recordingObserver struct {
	hits   int
	misses int
	ages   []time.Duration
}

func (o *recordingObserver) CacheLookup(_ string, hit bool) {
	if hit {
		o.hits++
		return
	}
	o.misses++
}
func (o *recordingObserver) CacheEntryAge(_ string, age time.Duration) { o.ages = append(o.ages, age) }
func (o *recordingObserver) CacheUnavailable()                         {}
func (o *recordingObserver) InvalidationFailed(string)                 {}

// --- fixture helpers --------------------------------------------------------

// setupCached is the standard fixture with the cache wired into all three
// places it belongs: the decision reads it, and the two handlers that change
// what it holds invalidate it.
func setupCached(t *testing.T) *fixture {
	t.Helper()

	f := setup(t)
	f.cache = NewCache(f.redis, nil, discard())
	f.authz.Cache = f.cache
	f.grants.Cache = f.cache
	f.roles.Cache = f.cache
	return f
}

func (f *fixture) revokeThroughAPI(t *testing.T, userID, projectID string) {
	t.Helper()
	rec := f.request(t, "DELETE",
		"/v1/organizations/"+f.orgA+"/users/"+userID+"/grants/"+projectID, "", f.adminToken)
	if rec.Code != 204 {
		t.Fatalf("revoking: %d %s", rec.Code, rec.Body)
	}
}

func (f *fixture) narrowRoleThroughAPI(t *testing.T, roleKey string) {
	t.Helper()

	var roleID string
	f.factory.QueryRow(&roleID,
		`SELECT id FROM roles WHERE project_id = $1 AND key = $2`, f.projectA, roleKey)

	rec := f.request(t, "PATCH",
		"/v1/organizations/"+f.orgA+"/projects/"+f.projectA+"/roles/"+roleID,
		`{"display_name":"Approver","permission_keys":["purchase_request:read"]}`, f.adminToken)
	if rec.Code != 200 {
		t.Fatalf("narrowing the role: %d %s", rec.Code, rec.Body)
	}
}

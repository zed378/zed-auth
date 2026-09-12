package ratelimit

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// consume counts one request and reports the running total.
//
// Atomic, and it has to be: INCR followed by a separate EXPIRE is two round
// trips, and a process that dies between them leaves a counter with no expiry —
// which is a client rate-limited forever, by a total that never resets. The
// script sets the expiry on the increment that CREATES the key, in the same
// call, so that state cannot exist.
//
// PEXPIRE only when the counter is 1: re-setting it on every request would
// slide the window forward under a client that keeps calling, which turns a
// fixed window into a bound that never resets while it is being hit.
var consume = redis.NewScript(`
	local count = redis.call("INCR", KEYS[1])
	if count == 1 then
		redis.call("PEXPIRE", KEYS[1], ARGV[1])
	end
	return count
`)

// Quotas counts requests per client.
//
// Separate from Limiter rather than a method on it, because they answer
// different questions with different failure modes — and because a request
// counter that shared the cooldown limiter's State would count a successful
// management call as a failed login.
//
// **It fails open, loudly**, for ADR-017's reason applied here: the Management
// API's own availability must not depend on Redis. An outage that silently
// removed the bound is what the Unavailable metric exists to prevent.
type Quotas struct {
	client   redis.UniversalClient
	observer Observer
	log      *slog.Logger

	quota Quota

	// namespace separates one bound's counter from another's. Two quotas
	// sharing a key would share an allowance, so the higher one would be
	// spent by traffic the lower one was meant to bound — which is the
	// opposite of having two.
	namespace string
}

func NewQuotas(client redis.UniversalClient, observer Observer, log *slog.Logger) *Quotas {
	return &Quotas{client: client, observer: observer, log: log, quota: PerClient}
}

// WithQuota returns a copy bounded by q, counting under its own namespace.
//
// The namespace is not optional for a second bound: without it the two share a
// counter and the higher allowance is consumed by the traffic the lower one
// governs. Passing an empty namespace keeps the original key, which is what
// the default bound uses.
func (q *Quotas) WithQuota(quota Quota, namespace string) *Quotas {
	copied := *q
	copied.quota = quota
	copied.namespace = namespace
	return &copied
}

// Quota reports the bound in force, so a caller can render it without
// assuming the package default.
func (q *Quotas) Quota() Quota { return q.quota }

// BoundClient names this bound in the metric and the log.
const BoundClient = "client"

// Consume counts one request against a client's allowance.
//
// Counted BEFORE the handler runs and regardless of what the handler answers.
// Counting only successes would let an enumeration run — which is mostly 404s —
// proceed unbounded, and that is precisely the shape a stolen credential takes.
func (q *Quotas) Consume(ctx context.Context, clientID string, now time.Time) Verdict {
	key := ClientKey(clientID, q.quota.WindowStart(now))
	if key != "" && q.namespace != "" {
		key += ":" + q.namespace
	}
	if key == "" || q.client == nil {
		// No client id: the caller has already decided what to do about that.
		// Reporting the full allowance here rather than refusing keeps this
		// function from being an authentication check by accident.
		return q.quota.Unlimited(now)
	}

	// The TTL runs to the end of the window plus a second of slack, so a
	// counter cannot expire fractionally before the Reset this service just
	// told the client to wait for.
	ttl := q.quota.WindowStart(now).Add(q.quota.Window + time.Second).Sub(now)

	count, err := consume.Run(ctx, q.client, []string{key}, ttl.Milliseconds()).Int()
	if err != nil {
		q.unavailable(err)
		return q.quota.Unlimited(now)
	}

	verdict := q.quota.Decide(count, now)
	if !verdict.Allowed && q.observer != nil {
		q.observer.Refused(BoundClient)
	}
	return verdict
}

// unavailable is the loud half of failing open.
//
// The client id is logged: unlike the login limiter's key, it is not a
// person's submitted address but the identifier of an application, and an
// operator reading this line needs to know whose bound stopped being enforced.
func (q *Quotas) unavailable(err error) {
	if q.observer != nil {
		q.observer.Unavailable()
	}
	if q.log != nil {
		q.log.Warn("the request quota could not reach its store; management calls are proceeding unlimited",
			"error", err.Error())
	}
}

// ConsumeMail counts one message against a recipient's allowance.
//
// Shares Consume's Lua script, TTL arithmetic and fail-open behaviour, because
// a second implementation of any of those is a second thing to get wrong — and
// ADR-017's fail-open reasoning applies here unchanged: refusing every
// invitation because Redis is down converts a cache outage into an inability
// to onboard anybody.
func (q *Quotas) ConsumeMail(ctx context.Context, address string, now time.Time) Verdict {
	key := MailKey(address, q.quota.WindowStart(now))
	if key == "" || q.client == nil {
		return q.quota.Unlimited(now)
	}

	ttl := q.quota.WindowStart(now).Add(q.quota.Window + time.Second).Sub(now)

	count, err := consume.Run(ctx, q.client, []string{key}, ttl.Milliseconds()).Int()
	if err != nil {
		q.unavailable(err)
		return q.quota.Unlimited(now)
	}

	verdict := q.quota.Decide(count, now)
	if !verdict.Allowed && q.observer != nil {
		q.observer.Refused(BoundMail)
	}
	return verdict
}

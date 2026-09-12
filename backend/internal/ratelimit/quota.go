package ratelimit

import (
	"strconv"
	"strings"
	"time"
)

// A request-rate bound, which is a different thing from the rest of this
// package (P1-15, closing PG-19).
//
// Everything else here counts FAILURES and answers with a cooldown, because it
// exists to make guessing a credential expensive. A quota counts REQUESTS and
// answers with a remaining allowance, because it exists to bound how much work
// one client can ask the estate to do — including a client whose credentials
// are perfectly valid and have been stolen.
//
// docs/PLAN/05 Part B: "Rate limits applied per client_id/API key (not just IP),
// with standard X-RateLimit-* headers."

// Quota is N requests per window.
type Quota struct {
	Limit  int
	Window time.Duration
}

// PerClient bounds one client's calls to the Management API.
//
// 600 a minute — ten a second sustained, which is far above any console
// session and comfortably above a provisioning script walking a few thousand
// users at a page at a time, while still being a real bound on a compromised
// credential enumerating an estate.
//
// Per CLIENT rather than per user or per IP, and that is the abuse case: a
// service account's credentials are long-lived, are copied into CI, and do not
// change when the person who created them leaves. Keying on the client id also
// survives a secret rotation (spec abuse case A-5) — the id is precisely the
// part that does not change when a secret does.
var PerClient = Quota{Limit: 600, Window: time.Minute}

// PerClientAuthz bounds `/v1/authz/check`, which is a different kind of traffic
// (P2-17).
//
// PerClient is sized for a console session and a provisioning script. A
// consumer calls THIS endpoint on every protected request — `docs/PLAN/12`
// calls it "called frequently at runtime by resource servers" — so ten a second
// is a ceiling one busy application reaches with a handful of users, and the
// 429 lands on its hot path rather than on an administrator's tooling.
//
// `P2-17`'s load test found exactly that: 19,658 of 40,515 requests refused at
// 20 concurrent workers, against an endpoint whose own latency targets are the
// tightest in the plan.
//
// 6,000 a minute — a hundred a second sustained per client. Still a real bound
// on a stolen credential, and the thing a stolen credential would do here is
// worth naming: this endpoint answers allow/deny for one action at a time,
// within the caller's own project, and is deliberately unhelpful as an oracle
// (a nonexistent subject and an unauthorized one are byte-identical). So the
// enumeration value of the volume between 600 and 6,000 is close to nothing,
// while the availability cost of the lower bound is a consumer's users seeing
// failures.
var PerClientAuthz = Quota{Limit: 6000, Window: time.Minute}

// Verdict is what a quota check answers, and it is shaped to be the headers.
//
// Every field is reported on EVERY response, not only on a refusal. A client
// that learns its remaining allowance only when it has already run out cannot
// pace itself, which is the difference between a limit and a trap.
type Verdict struct {
	Allowed bool

	// Limit and Remaining become X-RateLimit-Limit and X-RateLimit-Remaining.
	// Remaining is never negative: a client is told zero, not how far past the
	// bound its own retries have pushed the counter.
	Limit     int
	Remaining int

	// Reset is when the window turns over, for X-RateLimit-Reset.
	Reset time.Time

	// RetryAfter is Reset expressed as a duration, for the Retry-After header
	// docs/PLAN/05's 429 carries. Zero when allowed.
	RetryAfter time.Duration
}

// WindowStart is the beginning of the window `now` falls in.
//
// A FIXED window, and the tradeoff is worth stating rather than discovering: a
// client can send Limit requests just before a boundary and Limit again just
// after, so the true worst case over a sliding minute is 2×Limit. A sliding
// window would close that, at the cost of a counter per request and an
// X-RateLimit-Reset that no longer names a real moment.
//
// For what this bound is FOR — stopping one client from consuming the estate,
// not metering a paid API — a factor of two at a boundary does not change the
// answer, and a client being able to reason about `Reset` does.
func (q Quota) WindowStart(now time.Time) time.Time {
	return now.Truncate(q.Window)
}

// Decide turns a count into a verdict.
//
// Pure, so the arithmetic can be tested without Redis, a clock or a request —
// the same split Evaluate makes for the cooldown policy. `count` is the value
// AFTER this request was counted, so the first request of a window arrives
// here as 1.
func (q Quota) Decide(count int, now time.Time) Verdict {
	reset := q.WindowStart(now).Add(q.Window)

	v := Verdict{
		Limit:     q.Limit,
		Remaining: q.Limit - count,
		Reset:     reset,
		Allowed:   count <= q.Limit,
	}
	if v.Remaining < 0 {
		v.Remaining = 0
	}
	if !v.Allowed {
		v.RetryAfter = reset.Sub(now)
		if v.RetryAfter < 0 {
			v.RetryAfter = 0
		}
	}
	return v
}

// Unlimited is the verdict used when the store could not answer.
//
// It reports the full allowance rather than a fabricated remainder. Telling a
// client "599 remaining" when nothing was counted would be a number this
// service made up, and a client pacing itself against it would be pacing
// against fiction. The Unavailable metric is what makes the gap visible
// (ADR-017).
func (q Quota) Unlimited(now time.Time) Verdict {
	return Verdict{
		Allowed:   true,
		Limit:     q.Limit,
		Remaining: q.Limit,
		Reset:     q.WindowStart(now).Add(q.Window),
	}
}

// ClientKey is the counter for one client in one window.
//
// The window start is IN the key, which is what makes expiry self-correcting:
// each window is a new key that dies on its own, so there is no reset step to
// race with and no counter that can be left behind holding a stale total.
func ClientKey(clientID string, windowStart time.Time) string {
	if clientID == "" {
		return ""
	}
	return "ratelimit:client:" + clientID + ":" + strconv.FormatInt(windowStart.Unix(), 10)
}

// BoundMail names the email-amplification bound in metrics and logs.
const BoundMail = "mail"

// MailKey is the counter for one recipient in one window.
//
// **Keyed on the recipient, not on the caller.** The abuse this bounds is
// invite-flooding a THIRD PARTY (`docs/SECURITY/02` §10): an administrator with
// a legitimate account sending twenty invitations an hour to somebody who never
// asked for one. A per-caller bound does not touch that — the caller is
// entitled to be there, and it is the mailbox that suffers.
//
// The address is lowercased for the same reason AddressKey lowercases it: two
// spellings of one mailbox must share one counter, or the bound is a
// formality.
func MailKey(address string, windowStart time.Time) string {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return ""
	}
	return "ratelimit:mail:" + address + ":" + strconv.FormatInt(windowStart.Unix(), 10)
}

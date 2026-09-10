package authn

import (
	"bufio"
	"context"
	"crypto/sha1" // #nosec G505 -- see the comment on Client: this is the corpus service's index, never a password hash.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Breached-password rejection, per docs/PLAN/09 § Passwords & Credentials.
//
// The composition rules in policy.go bound the search space an attacker must
// cover. This bounds a different thing: the password that satisfies every rule
// and is still the first one tried, because a few million other people also
// chose it. `P4$$w0rd2026!` passes a twelve-character mixed-case
// digit-and-symbol policy and appears in the corpus hundreds of thousands of
// times. Neither control substitutes for the other.
//
// Specification: MEMORY/specs/P1-02-password-policy.md § 12, § 16.

// ErrBreachServiceUnavailable is returned when the corpus could not be
// consulted — a timeout, a transport failure, a bad status, or a response this
// code could not parse.
//
// It is deliberately distinct from a "yes, breached" answer, and every caller
// must keep them distinct. Conflating a service failure with a clean answer is
// how a fail-open policy silently becomes an always-open one, with nothing in
// the logs to say when it happened.
var ErrBreachServiceUnavailable = errors.New("authn: the breached-password service could not be reached")

// BreachChecker answers whether a password appears in a breach corpus.
//
// An interface because the implementation is a third-party HTTP service that
// tests must not depend on, and because the corpus may be self-hosted later
// without any caller changing (spec § 22).
type BreachChecker interface {
	// Breached reports whether the password appears in the corpus.
	//
	// A non-nil error means the corpus could not be consulted. It never means
	// "probably breached" — the boolean is only meaningful when err is nil.
	Breached(ctx context.Context, password string) (bool, error)
}

// DefaultBreachAPI is the Pwned Passwords range endpoint.
//
// The k-anonymity model is the reason this particular service is acceptable
// to send anything to at all: it receives a 5-character prefix shared with
// hundreds of thousands of other hashes and returns every suffix under it. It
// cannot tell which one was asked about, so compromising it — or observing the
// traffic to it — reveals nothing about any password here.
const DefaultBreachAPI = "https://api.pwnedpasswords.com/range/"

// prefixLength is the k-anonymity parameter, in hex characters.
//
// Five is the service's contract, and it is also the whole privacy argument:
// 2^20 possible prefixes over a corpus of hundreds of millions means each
// prefix covers a large anonymity set. Sending more would narrow it.
const prefixLength = 5

// Client queries a breach corpus over HTTP using k-anonymity.
//
// **On SHA-1.** This computes a SHA-1 digest, and that is correct here and
// would be alarming anywhere else in this package: SHA-1 is the corpus
// service's index, not a password hash. Nothing SHA-1 touches is ever stored —
// users.password_hash is Argon2id and only Argon2id (P1-01). The digest exists
// for the duration of one lookup, and only its first five characters leave the
// process.
type Client struct {
	// Endpoint is the range API base. The prefix is appended directly.
	Endpoint string

	// HTTP is the client used for the lookup. Its Timeout bounds the call
	// alongside the request context.
	HTTP *http.Client
}

// DefaultBreachTimeout bounds one corpus lookup.
//
// Measured against the live service from the staging VM: 273ms for a 77KB
// response. Two seconds is generous against that and still short enough that a
// hung dependency does not hold a password-change request open. It is off the
// login path entirely — expiry checking at login reads a timestamp and never
// calls out (spec § NFR-2).
const DefaultBreachTimeout = 2 * time.Second

// NewBreachClient returns a Client with the defaults.
func NewBreachClient() *Client {
	return &Client{
		Endpoint: DefaultBreachAPI,
		HTTP:     &http.Client{Timeout: DefaultBreachTimeout},
	}
}

// Breached looks the password up by hash prefix.
//
// The password never leaves the process, and neither does its full hash. What
// goes out is five hex characters; the remaining 35 are compared against the
// response locally.
func (c *Client) Breached(ctx context.Context, password string) (bool, error) {
	prefix, suffix := hashPrefixSuffix(password)

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultBreachAPI
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+prefix, nil)
	if err != nil {
		return false, fmt.Errorf("%w: building the request: %v", ErrBreachServiceUnavailable, err)
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: DefaultBreachTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		// The URL is deliberately not in the error. It contains the prefix,
		// and an error string travels to places a request URL should not —
		// though five characters is not sensitive, keeping derived material
		// out of error text by habit is cheaper than auditing where each one
		// ends up.
		return false, fmt.Errorf("%w: %v", ErrBreachServiceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: the service answered %d", ErrBreachServiceUnavailable, resp.StatusCode)
	}

	return scanForSuffix(resp.Body, suffix)
}

// hashPrefixSuffix splits the uppercase hex SHA-1 into the part that is sent
// and the part that is compared locally.
func hashPrefixSuffix(password string) (prefix, suffix string) {
	sum := sha1.Sum([]byte(password)) // #nosec G401 -- corpus index, not a password hash. See Client.
	digest := strings.ToUpper(hex.EncodeToString(sum[:]))
	return digest[:prefixLength], digest[prefixLength:]
}

// maxBreachResponseBytes bounds the response read.
//
// A prefix returns roughly 800 suffixes at 77KB (measured). One megabyte is
// ample headroom, and it means a third party — or something impersonating one
// — cannot make this allocate without limit. The response is untrusted input
// (spec § NFR-5).
const maxBreachResponseBytes = 1 << 20

// suffixLength is what remains of the 40-character hex digest after the
// prefix is taken off.
const suffixLength = 40 - prefixLength

// scanForSuffix reads the range response and reports whether our suffix is in
// it.
//
// The service returns `SUFFIX:COUNT` per line. The count is read past and
// deliberately discarded: "this password appears 4.7 million times" is a true
// and interesting fact whose presence in a log tells the reader something
// about the password (spec § 15).
//
// **Valid entries are counted, not lines.** A response has to look like a
// range response before "your suffix is not in it" means anything. Every
// prefix in this corpus has hundreds of suffixes under it, so a body with no
// well-formed entry is something answering 200 without answering the question
// — a captive portal, a proxy error page, a changed API. Reading that as clean
// is a silent always-open, and counting lines rather than entries was exactly
// that bug: an HTML error page is one line, and one line is not zero.
func scanForSuffix(body io.Reader, suffix string) (bool, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, maxBreachResponseBytes))

	entries := 0
	found := false

	for scanner.Scan() {
		candidate, _, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		candidate = strings.TrimSpace(candidate)
		if !isHexSuffix(candidate) {
			continue
		}

		entries++
		// Scanning continues after a match rather than returning early, so
		// that a match in a body that is otherwise malformed still has to have
		// come from a response that looked like a range response.
		if strings.EqualFold(candidate, suffix) {
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("%w: reading the response: %v", ErrBreachServiceUnavailable, err)
	}

	if entries == 0 {
		return false, fmt.Errorf("%w: the response contained no well-formed entries", ErrBreachServiceUnavailable)
	}

	return found, nil
}

// isHexSuffix reports whether s is a hash suffix of the expected length.
func isHexSuffix(s string) bool {
	if len(s) != suffixLength {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// --- composition ------------------------------------------------------------

// Outcome is what a breach check produced, for the metric and the audit event.
//
// A distinct value for each way the check can end, because the whole
// justification for failing open is that a skip is visible. An outcome that
// collapsed `clean` and `skipped` into one would remove exactly the signal
// that makes the trade defensible.
type Outcome string

const (
	OutcomeClean    Outcome = "clean"
	OutcomeBreached Outcome = "breached"
	OutcomeSkipped  Outcome = "skipped"
	OutcomeDisabled Outcome = "disabled"
)

// CheckBreach runs the corpus check and applies the fail-open decision.
//
// **Fail open, never silently** (ADR-015, spec § 12). When the corpus cannot
// be consulted the password is accepted, and the caller must record the
// returned Outcome — that is not a suggestion, it is the half of the decision
// that makes it defensible. A fail-open nobody can see is indistinguishable
// from a breach check that was never wired up.
//
// Failing closed would make a third party a hard dependency of password
// changes, and the moment that matters most is the worst possible one for it:
// during an incident, users are told to rotate passwords, this path spikes,
// and a corpus outage would block the exact remediation the incident calls
// for. The risk accepted is bounded — one password admitted unchecked, still
// subject to every composition rule, still Argon2id-hashed, and re-checkable
// later. The risk refused has no ceiling.
//
// Fail-open covers service failure only. A definitive "in the corpus" is
// always a rejection, and an unparseable response is a failure rather than an
// answer.
func CheckBreach(ctx context.Context, checker BreachChecker, password string) ([]Violation, Outcome, error) {
	if checker == nil {
		return nil, OutcomeDisabled, nil
	}

	breached, err := checker.Breached(ctx, password)
	if err != nil {
		return nil, OutcomeSkipped, err
	}
	if breached {
		return []Violation{{
			Rule: RuleBreached,
			// No count, and no naming of the corpus. The user needs to know to
			// choose something else, not the size of their mistake.
			Message: "this password has appeared in a known data breach and cannot be used",
		}}, OutcomeBreached, nil
	}

	return nil, OutcomeClean, nil
}

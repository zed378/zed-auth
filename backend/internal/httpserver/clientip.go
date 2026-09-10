package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// Working out who a request actually came from.
//
// P1-13's FR-6: the rate limit must not be bypassable by rotating a header.
// P1-12 answered that by refusing to read `X-Forwarded-For` at all and using
// RemoteAddr, which is unforgeable and — on the deployment this service
// actually runs on — useless.
//
// `cloudflared` runs as a host service and reaches the published port, so
// RemoteAddr is the Docker gateway: THE SAME ADDRESS FOR EVERY USER IN THE
// WORLD. A per-IP limiter computed from it is not a per-IP limiter; it is a
// global one, and a single attacker could use it to lock every user out of the
// service. That is a denial of service delivered by the security control.
//
// So the client IP needs a configured source, and the configuration is two
// things rather than one:
//
//	AUTH_CLIENT_IP_HEADER      which header carries it, e.g. CF-Connecting-IP
//	AUTH_TRUSTED_PROXY_CIDRS   which peers are believed when they set it
//
// The header is read ONLY when the immediate peer is inside a trusted range.
// That is what makes it unforgeable: a client setting `CF-Connecting-IP`
// itself is not connecting from a trusted proxy, so its header is ignored.
// Either setting alone falls back to RemoteAddr, because a header believed
// from anywhere is a header anybody can write.

// ClientIP resolves the address a request came from.
type ClientIP struct {
	// Header is the header to read, or empty to always use RemoteAddr.
	Header string

	// Trusted are the peers whose Header value is believed.
	Trusted []*net.IPNet
}

// NewClientIP builds a resolver from configuration.
//
// Returns the parsed CIDRs it could not read, so a misconfiguration is
// reported at startup rather than silently narrowing the trusted set to
// nothing — which would look identical to working and would quietly turn every
// user into one IP.
func NewClientIP(header string, cidrs []string) (ClientIP, []string) {
	resolver := ClientIP{Header: strings.TrimSpace(header)}

	var bad []string
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			bad = append(bad, raw)
			continue
		}
		resolver.Trusted = append(resolver.Trusted, network)
	}

	// Both or neither. A header with no trusted peers would be read from
	// everybody, which is the forgeable case FR-6 forbids; trusted peers with
	// no header have nothing to read.
	if resolver.Header == "" || len(resolver.Trusted) == 0 {
		return ClientIP{}, bad
	}

	return resolver, bad
}

// Configured reports whether a header source is in use.
//
// The caller warns at startup when it is not, because behind a proxy that
// means the per-IP bound is a global one. It does not disable itself: a
// limiter that quietly turns off is worse than one that is loudly
// misconfigured.
func (c ClientIP) Configured() bool { return c.Header != "" && len(c.Trusted) > 0 }

// Of returns the client address for a request.
func (c ClientIP) Of(r *http.Request) string {
	peer := peerOf(r)

	if !c.Configured() || !c.trusts(peer) {
		return peer
	}

	// The FIRST value, and only when there is exactly one.
	//
	// A header a trusted proxy sets carries one address. A list means
	// something appended to it — either an untrusted hop, or the client itself
	// sending a value the proxy did not replace — and picking an element out
	// of a list somebody else can extend is how the bypass works. Refusing the
	// whole thing falls back to the peer, which is at worst the shared address
	// this endpoint would have used anyway.
	value := strings.TrimSpace(r.Header.Get(c.Header))
	if value == "" || strings.Contains(value, ",") {
		return peer
	}

	if net.ParseIP(value) == nil {
		return peer
	}
	return value
}

func (c ClientIP) trusts(peer string) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, network := range c.Trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// peerOf is the address the TCP connection came from. Unforgeable, and on a
// proxied deployment the same for everybody.
func peerOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

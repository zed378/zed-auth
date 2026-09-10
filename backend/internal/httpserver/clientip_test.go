package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func request(t *testing.T, remote, header, value string) *http.Request {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = remote
	if header != "" {
		r.Header.Set(header, value)
	}
	return r
}

func trusted(t *testing.T) ClientIP {
	t.Helper()

	resolver, bad := NewClientIP("CF-Connecting-IP", []string{"172.16.0.0/12", "10.0.0.0/8"})
	if len(bad) != 0 {
		t.Fatalf("valid CIDRs were rejected: %v", bad)
	}
	if !resolver.Configured() {
		t.Fatal("the resolver reports itself unconfigured")
	}
	return resolver
}

// The control this exists for: a header set by a trusted proxy is believed.
func TestATrustedProxysHeaderIsUsed(t *testing.T) {
	c := trusted(t)

	got := c.Of(request(t, "172.18.0.1:5000", "CF-Connecting-IP", "203.0.113.7"))

	if got != "203.0.113.7" {
		t.Errorf("client IP = %q, want the header value", got)
	}
}

// FR-6, and the whole point of pairing the header with a trusted set: a client
// that sets the header ITSELF is not connecting from a trusted proxy, so its
// value is ignored. Without this, rotating the header resets the limit.
func TestAnUntrustedPeersHeaderIsIgnored(t *testing.T) {
	c := trusted(t)

	got := c.Of(request(t, "198.51.100.9:44321", "CF-Connecting-IP", "203.0.113.7"))

	if got != "198.51.100.9" {
		t.Errorf("client IP = %q; an untrusted peer chose its own address", got)
	}
}

// A header a trusted proxy sets carries one address. A list means something
// appended to it, and picking an element out of a list somebody else can
// extend is how the bypass works.
func TestAListIsRefusedRatherThanParsed(t *testing.T) {
	c := trusted(t)

	for _, value := range []string{
		"203.0.113.7, 198.51.100.9",
		"198.51.100.9,203.0.113.7",
		"203.0.113.7, 203.0.113.8, 203.0.113.9",
	} {
		got := c.Of(request(t, "172.18.0.1:5000", "CF-Connecting-IP", value))
		if got != "172.18.0.1" {
			t.Errorf("value %q produced %q; a list was parsed instead of refused", value, got)
		}
	}
}

func TestAMalformedHeaderFallsBackToThePeer(t *testing.T) {
	c := trusted(t)

	for _, value := range []string{"", "not-an-ip", "999.999.999.999", "<script>"} {
		got := c.Of(request(t, "172.18.0.1:5000", "CF-Connecting-IP", value))
		if got != "172.18.0.1" {
			t.Errorf("value %q produced %q", value, got)
		}
	}
}

// Either setting alone is not a configuration. A header with no trusted peers
// would be read from everybody — the forgeable case — and trusted peers with
// no header have nothing to read.
func TestBothSettingsAreRequired(t *testing.T) {
	cases := map[string]struct {
		header string
		cidrs  []string
	}{
		"header with no trusted peers": {"CF-Connecting-IP", nil},
		"trusted peers with no header": {"", []string{"172.16.0.0/12"}},
		"neither":                      {"", nil},
		"empty strings":                {"   ", []string{"  "}},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resolver, _ := NewClientIP(c.header, c.cidrs)

			if resolver.Configured() {
				t.Fatal("a half-configuration reports itself configured")
			}
			// And it uses the peer, which is unforgeable.
			got := resolver.Of(request(t, "172.18.0.1:5000", "CF-Connecting-IP", "203.0.113.7"))
			if got != "172.18.0.1" {
				t.Errorf("client IP = %q, want the peer", got)
			}
		})
	}
}

// A misconfigured CIDR is reported rather than silently dropped. Narrowing the
// trusted set to nothing looks identical to working and would quietly turn
// every user into one IP.
func TestABadCIDRIsReported(t *testing.T) {
	resolver, bad := NewClientIP("CF-Connecting-IP", []string{"172.16.0.0/12", "not-a-cidr", "10.0.0.0/8"})

	if len(bad) != 1 || bad[0] != "not-a-cidr" {
		t.Errorf("bad = %v, want the one unparseable entry", bad)
	}
	// The valid ones still work, so one typo does not disable the control.
	if !resolver.Configured() {
		t.Error("one bad entry disabled the whole resolver")
	}
}

// With nothing configured the peer is used, which is correct and — behind a
// proxy — the same for everybody. That is the situation the startup warning is
// about, and this test pins the behaviour it warns about.
func TestWithNoConfigurationEveryProxiedRequestLooksTheSame(t *testing.T) {
	resolver, _ := NewClientIP("", nil)

	first := resolver.Of(request(t, "172.18.0.1:5000", "CF-Connecting-IP", "203.0.113.7"))
	second := resolver.Of(request(t, "172.18.0.1:5001", "CF-Connecting-IP", "198.51.100.9"))

	if first != second {
		t.Fatal("two proxied requests resolved differently with no configuration")
	}
	if first != "172.18.0.1" {
		t.Errorf("client IP = %q, want the shared peer", first)
	}
}

func TestIPv6PeersAreHandled(t *testing.T) {
	resolver, bad := NewClientIP("CF-Connecting-IP", []string{"2001:db8::/32"})
	if len(bad) != 0 {
		t.Fatalf("an IPv6 CIDR was rejected: %v", bad)
	}

	got := resolver.Of(request(t, "[2001:db8::1]:5000", "CF-Connecting-IP", "203.0.113.7"))
	if got != "203.0.113.7" {
		t.Errorf("client IP = %q, want the header from a trusted IPv6 peer", got)
	}
}

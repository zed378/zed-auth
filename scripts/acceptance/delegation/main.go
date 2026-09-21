// Command delegation checks the cross-organization delegation surface against a
// running deployment and prints the evidence (P4-01 … P4-06).
//
// It answers the questions the console cannot answer for itself, because the
// interesting ones are about what a caller is refused:
//
//	The receiving organization can DISCOVER the grants made to it, named.
//	That list excludes the grants it MADE, which its own RLS also lets it see.
//	A role the grant delegates can be assigned; one it withholds cannot.
//	A third organization sees none of it.
//	A revoked grant stays listed, marked, and grants nothing.
//
// Like `scripts/acceptance/phase3`, it runs ON the staging VM against the
// service's own port while looking like the tunnel, so it is not also testing
// Cloudflare. `scripts/acceptance-delegation.sh` seeds two throwaway
// organizations with their administrators and applications, and removes them
// afterwards.
//
// Every negative is credited only beside its positive. "A withheld role is
// refused" is also true of an endpoint that refuses everything, so each refusal
// here is preceded by the same operation succeeding with a delegated role.
//
// Standard library only, for the reason `demo/` and the load test give:
// evidence that needs a dependency tree to reproduce is weaker evidence.
package main

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- transport ---------------------------------------------------------------

type client struct {
	base, host string
	http       *http.Client
	cookie     map[string]string
}

func newClient(base, host string) *client {
	return &client{
		base: base, host: host,
		http: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       20 * time.Second,
		},
		cookie: map[string]string{},
	}
}

type result struct {
	status   int
	body     string
	location string
}

func (c *client) send(method, path, contentType string, body io.Reader, bearer string) result {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		die("building a request: %v", err)
	}
	req.Host = c.host
	req.Header.Set("X-Forwarded-Proto", "https")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	parts := make([]string, 0, len(c.cookie))
	for k, v := range c.cookie {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		die("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, sc := range resp.Header.Values("Set-Cookie") {
		head := strings.SplitN(sc, ";", 2)[0]
		if k, v, ok := strings.Cut(head, "="); ok {
			if v == "" || strings.Contains(strings.ToLower(sc), "max-age=0") {
				delete(c.cookie, k)
			} else {
				c.cookie[k] = v
			}
		}
	}
	return result{status: resp.StatusCode, body: string(raw), location: resp.Header.Get("Location")}
}

func (c *client) form(path string, v url.Values) result {
	return c.send("POST", path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()), "")
}

func (c *client) get(path, bearer string) result { return c.send("GET", path, "", nil, bearer) }

func (c *client) json(method, path, body, bearer string) result {
	return c.send(method, path, "application/json", strings.NewReader(body), bearer)
}

// --- output ------------------------------------------------------------------

var passed, failed int

func section(s string) { fmt.Printf("\n\033[1;36m%s\033[0m\n", s) }
func pass(format string, a ...any) {
	passed++
	fmt.Printf("  \033[32m✓\033[0m %s\n", fmt.Sprintf(format, a...))
}
func fail(format string, a ...any) {
	failed++
	fmt.Printf("  \033[31m✗\033[0m %s\n", fmt.Sprintf(format, a...))
}
func note(format string, a ...any) { fmt.Printf("      \033[2m%s\033[0m\n", fmt.Sprintf(format, a...)) }
func die(format string, a ...any) {
	fmt.Printf("\n\033[31m✗ %s\033[0m\n", fmt.Sprintf(format, a...))
	os.Exit(2)
}

// --- sign-in -----------------------------------------------------------------

var (
	reCSRF    = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)
	reRequest = regexp.MustCompile(`name="request" value="([^"]*)"`)
)

type account struct {
	clientID, email, password string
}

type tokens struct {
	Access string `json:"access_token"`
	Error  string `json:"error"`
}

func pkce() (string, string) {
	b := make([]byte, 32)
	_, _ = crand.Read(b)
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// signIn runs the hosted flow end to end and returns an access token. The
// console does exactly this; using the same path means a token that works here
// is a token the console would have.
func signIn(base, host string, a account) string {
	c := newClient(base, host)
	verifier, challenge := pkce()
	redirect := "http://localhost:9998/callback"

	q := url.Values{
		"response_type": {"code"}, "client_id": {a.clientID}, "redirect_uri": {redirect},
		"scope": {"openid"}, "state": {"s"}, "nonce": {"n"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	r := c.get("/oauth/authorize?"+q.Encode(), "")
	if r.status != http.StatusFound || !strings.Contains(r.location, "/login?request=") {
		die("authorize did not offer the login page for %s: %d %s", a.email, r.status, r.location)
	}
	page := c.get(strings.TrimPrefix(r.location, "https://"+host), "")
	csrf, req := reCSRF.FindStringSubmatch(page.body), reRequest.FindStringSubmatch(page.body)
	if csrf == nil || req == nil {
		die("the login page has no csrf_token or request field")
	}
	posted := c.form("/login", url.Values{
		"csrf_token": {csrf[1]}, "request": {req[1]}, "email": {a.email}, "password": {a.password},
	})
	u, err := url.Parse(posted.location)
	if err != nil || u.Query().Get("code") == "" {
		die("%s could not sign in: %d %s", a.email, posted.status, posted.body)
	}
	exchanged := c.form("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {u.Query().Get("code")},
		"client_id": {a.clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	var t tokens
	_ = json.Unmarshal([]byte(exchanged.body), &t)
	if t.Access == "" {
		die("the code exchange failed for %s: %s", a.email, t.Error)
	}
	return t.Access
}

// --- the shapes this reads ----------------------------------------------------

type receivedGrant struct {
	ID              string   `json:"id"`
	ProjectID       string   `json:"project_id"`
	ProjectName     string   `json:"project_name"`
	GrantingOrgID   string   `json:"granting_org_id"`
	GrantingOrgName string   `json:"granting_org_name"`
	GrantedRoleKeys []string `json:"granted_role_keys"`
	HolderCount     int      `json:"holder_count"`
	Status          string   `json:"status"`
}

type receivedList struct {
	Grants []receivedGrant `json:"grants"`
}

func received(c *client, token, org string) receivedList {
	r := c.get("/v1/organizations/"+org+"/project-grants", token)
	if r.status != http.StatusOK {
		die("listing received grants for %s: %d %s", org, r.status, r.body)
	}
	var out receivedList
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		die("decoding the received list: %v — %s", err, r.body)
	}
	return out
}

func find(list receivedList, id string) *receivedGrant {
	for i := range list.Grants {
		if list.Grants[i].ID == id {
			return &list.Grants[i]
		}
	}
	return nil
}

// --- the checks ---------------------------------------------------------------

func main() {
	base := getenv("ACCEPT_BASE", "http://127.0.0.1:10800")
	host := getenv("ACCEPT_HOST", "auth.zedth.my.id")

	vendorOrg := must("DELEG_VENDOR_ORG")   // A — makes the grant
	partnerOrg := must("DELEG_PARTNER_ORG") // B — receives it
	bystanderOrg := must("DELEG_BYSTANDER_ORG")
	grantID := must("DELEG_GRANT_ID")        // A → B, active
	ownGrantID := must("DELEG_OWN_GRANT_ID") // B → C, so B can see a grant it MADE
	shared := must("DELEG_SHARED_ROLE")
	withheld := must("DELEG_WITHHELD_ROLE")
	memberID := must("DELEG_PARTNER_MEMBER")

	partner := account{clientID: must("DELEG_PARTNER_CLIENT"), email: must("DELEG_PARTNER_EMAIL"), password: must("DELEG_PARTNER_PASSWORD")}
	bystander := account{clientID: must("DELEG_BYSTANDER_CLIENT"), email: must("DELEG_BYSTANDER_EMAIL"), password: must("DELEG_BYSTANDER_PASSWORD")}
	vendor := account{clientID: must("DELEG_VENDOR_CLIENT"), email: must("DELEG_VENDOR_EMAIL"), password: must("DELEG_VENDOR_PASSWORD")}

	fmt.Printf("\033[1mDelegation acceptance\033[0m — %s (via %s)\n", host, base)

	c := newClient(base, host)
	partnerToken := signIn(base, host, partner)
	bystanderToken := signIn(base, host, bystander)
	vendorToken := signIn(base, host, vendor)

	// --- 1. discovery ---------------------------------------------------------
	section("1. The receiving organization can find what it was given, and name it")

	list := received(c, partnerToken, partnerOrg)
	got := find(list, grantID)
	if got == nil {
		die("the partner cannot see the grant made to it — nothing else here is meaningful")
	}
	pass("the grant made to the partner is listed")

	if got.ProjectName != "" && got.GrantingOrgName != "" {
		pass("named: project %q from %q", got.ProjectName, got.GrantingOrgName)
	} else {
		fail("the row carries no names — project %q, organization %q; a partner sees two UUIDs",
			got.ProjectName, got.GrantingOrgName)
	}
	if got.GrantingOrgID == vendorOrg {
		pass("the granting organization is the vendor")
	} else {
		fail("granting_org_id is %s, want %s", got.GrantingOrgID, vendorOrg)
	}
	if len(got.GrantedRoleKeys) == 1 && got.GrantedRoleKeys[0] == shared {
		pass("exactly the delegated role is published: %v", got.GrantedRoleKeys)
	} else {
		fail("granted_role_keys is %v, want [%s] — the withheld role must not appear", got.GrantedRoleKeys, shared)
	}

	// --- 2. the filter that is the control ------------------------------------
	section("2. The list excludes the grants this organization MADE")

	if find(list, ownGrantID) == nil {
		pass("the grant the partner itself made is absent")
		note("it is visible to the partner under the two-sided policy; the filter, not RLS, leaves it out")
	} else {
		fail("the partner's OWN grant appears in its received list — visibility has been mistaken for authority")
	}

	// --- 3. assignment, within the grant and not beyond it --------------------
	section("3. A delegated role can be assigned; a withheld one cannot")

	body := fmt.Sprintf(`{"user_id":%q,"role_keys":[%q]}`, memberID, shared)
	assigned := c.json("POST", "/v1/organizations/"+partnerOrg+"/project-grants/"+grantID+"/user-grants", body, partnerToken)
	if assigned.status == http.StatusCreated {
		pass("the delegated role was assigned to one of the partner's own people")
	} else {
		fail("assigning the delegated role: %d %s", assigned.status, assigned.body)
	}

	refusedBody := fmt.Sprintf(`{"user_id":%q,"role_keys":[%q]}`, memberID, withheld)
	refused := c.json("POST", "/v1/organizations/"+partnerOrg+"/project-grants/"+grantID+"/user-grants", refusedBody, partnerToken)
	if refused.status == http.StatusBadRequest || refused.status == http.StatusForbidden {
		pass("a role the grant withholds is refused (%d)", refused.status)
		if strings.Contains(refused.body, withheld) {
			note("the refusal names it, so an administrator can see which key was rejected")
		}
	} else {
		fail("assigning the WITHHELD role answered %d — %s", refused.status, refused.body)
	}

	after := find(received(c, partnerToken, partnerOrg), grantID)
	if after != nil && after.HolderCount == 1 {
		pass("holder_count is 1 — the blast radius is visible from the receiving side")
	} else if after != nil {
		fail("holder_count is %d after one assignment, want 1", after.HolderCount)
	}

	// --- 4. nobody else ------------------------------------------------------
	section("4. A third organization is not a party to either side")

	if outside := received(c, bystanderToken, bystanderOrg); len(outside.Grants) == 0 {
		pass("the bystander's own received list is empty")
	} else {
		fail("the bystander sees %d grants of a delegation it is not part of", len(outside.Grants))
	}

	probe := c.get("/v1/organizations/"+partnerOrg+"/project-grants", bystanderToken)
	if probe.status == http.StatusNotFound {
		pass("asking under the partner's path answers 404 — the organization is not confirmed to exist")
	} else {
		fail("the bystander got %d under the partner's path, want 404: %s", probe.status, probe.body)
	}

	// --- 5. revocation -------------------------------------------------------
	section("5. A revoked grant stays listed, marked, and grants nothing")

	revoked := c.send("DELETE",
		"/v1/organizations/"+vendorOrg+"/projects/"+got.ProjectID+"/grants/"+grantID, "", nil, vendorToken)
	if revoked.status != http.StatusNoContent {
		die("the vendor could not revoke its own grant: %d %s", revoked.status, revoked.body)
	}
	pass("the vendor revoked the grant")

	ended := find(received(c, partnerToken, partnerOrg), grantID)
	switch {
	case ended == nil:
		fail("the revoked grant vanished from the partner's list — access ended and the record went with it")
	case ended.Status != "revoked":
		fail("the revoked grant still reads %q", ended.Status)
	default:
		pass("it is still listed, marked revoked")
	}

	stillAssigning := c.json("POST", "/v1/organizations/"+partnerOrg+"/project-grants/"+grantID+"/user-grants", body, partnerToken)
	if stillAssigning.status == http.StatusConflict {
		pass("assigning through the revoked grant answers 409")
	} else {
		fail("assigning through a revoked grant answered %d, want 409: %s", stillAssigning.status, stillAssigning.body)
	}

	// --- verdict -------------------------------------------------------------
	fmt.Printf("\n\033[1m%d passed, %d failed\033[0m\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		die("%s is required", k)
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

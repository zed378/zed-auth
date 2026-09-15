# Phase 4 Threat-Model Review

| | |
|---|---|
| **Date** | 2026-09-15 |
| **Task** | `P3-15` step 3 — `docs/PLAN/09` § Secure Development Practices |
| **Reviewing** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md`, before any of it is built |
| **Method** | `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`'s categories and Phase 3's verified lessons against each card, then each card against the code it will have to change |

---

Phase 3's pattern was that every security mechanism brings a recovery path, and
the recovery path becomes the new attack surface. Phase 4's pattern is related.
**Every card adds a way to issue or change access without the one login flow
that the policy checks were built around.** A delegated user signs in to
another organization's application. A SAML service provider gets an assertion
from a session it never saw established. A provider callback creates a
session. A SCIM token creates users. A webhook sends events out of the tenant
entirely.

Phase 3 has already shown what that costs. `P3-07`'s specification said "the
refresh grant re-reads the policy", and nothing did. The mandate applied only
to people who typed a password (`P3-14`). Phase 4 multiplies the number of
paths that can repeat that mistake.

The cards are good at naming the obvious threats. Several of them, though, were
written without reading the code they will change. That code already enforces
invariants that Phase 4 has to relax on purpose. Most of what follows is about
where those invariants live.

## T4-1 — Delegation has no sign-in path, and the code that exists refuses one

**Cards**: `P4-04`, `P4-01`, `P4-02`.

`docs/PLAN/08` Part C's Procurement Portal scenario means a vendor's staff
member, who is a user in organization B, signs in to an application in
organization A's project. Today nothing lets that happen, and two places are
built to stop it:

- `internal/login/handler.go` authenticates the password inside
  `pending.App.OrgID`'s tenant, so an org B user is never found at org A's
  login page.
- `internal/oauth/authorize/handler.go` refuses any session where
  `current.OrgID != app.OrgID`, on both the silent path and the login path:
  "a session for another organization is not a session for this client".

None of `P4-01`–`P4-04` mentions either. `P4-04` is written as a claims task,
but the claims come after a sign-in the plan never designs. Relaxing that
comparison is the most dangerous edit in Phase 4, because it is the check
that stops a code being issued across tenants. The plan also never says
**whose policy governs**. If org A mandates MFA on its application and
org B does not, delegation is a route around A's mandate. The same applies
to `session_lifetime_hours` and `allowed_login_methods`.

**Ask of P4-04** (as a spec section, before `P4-01` starts): write the
cross-organization sign-in rule down explicitly. The recommended rule: a
session from org B is valid for an application in org A only while an
**active** Project Grant for that application's project names B, and the user
holds a delegated `user_grants` row under it. Anything else keeps today's
refusal. Record which organization's login policy applies. The recommended
answer is that the granting organization's MFA mandate and allowed methods
apply *in addition to* the user's own, because an organization's policy for its
own application must not be something a partner can opt out of. Test both
halves. Org A mandates MFA past its grace and org B does not, and a B user with
a delegated role and no factor is sent to enrolment at A's application. A B
user with no delegated row still gets exactly today's refusal. This is a gap in
`docs/PLAN/08` Part C and should go through the plan-change process rather than
be settled in code.

## T4-2 — The subset is checked where grants are written, and every reader ignores it

**Cards**: `P4-02`, `P4-04`, `P4-01`.

`P4-02`'s rule is "subset validation on every request". Its Definition of Done
tests it by narrowing a grant and confirming a **new assignment** is refused.
That covers the write path only. The rows written before the narrowing still
exist, and the code that turns them into access never looks at
`project_grants`:

- `grant.TokenClaims.ForToken` reads `user_grants.role_keys` directly for the
  token's role claim.
- `authz.readRoleKeys` does the same for `/v1/authz/check`.
- `P2-03` designed the Phase 4 check as a replacement body for the trigger
  `user_grants_delegation_is_not_yet_implemented`. A trigger runs on INSERT
  and UPDATE, so it cannot make a narrowed grant stop working.

Revocation has the same gap. `P4-01` step 7 makes revocation a status change,
and `user_grants.project_grant_id` cascades only on a hard DELETE. So a revoked
grant leaves every delegated row in place, and both readers keep serving those
rows. This is `P3-07`'s A-2 again: a control written in the abuse table and
applied only on the path where it was easiest to write.

**Ask of P4-02 and P4-04**: every reader of `user_grants` joins
`project_grants`. That includes the token claims, `/v1/authz/check`, its cache
fill, and the SAML attribute builder from T4-6. For a delegated row, a reader
returns `role_keys ∩ granted_role_keys` and nothing unless `status = 'active'`.
Narrowing may also rewrite the rows in the same transaction, but the reader
check is still required, because a grant changed by direct SQL skips any
application code. Add Definition of Done rows that test the readers, not the
writer: assign `cashier` and `manager` under a grant, narrow it to `cashier`,
then refresh a token and call `/v1/authz/check` for that user. `manager` must
be absent from both. Repeat after revocation. Each test must be shown to fail
with the join removed.

## T4-3 — A row that two tenants must see, in a schema built so that none can

**Cards**: `P4-01`, `P4-02`, `P4-04`.

The existing invariants all assume one row belongs to one tenant, and they are
correct for everything built so far:

- `user_grants_tenant_isolation`: `org_id = current_org_id()` for USING and
  WITH CHECK.
- `org_must_match_project()` (`20260912000024`) runs as the caller, under RLS.
  In the receiving tenant, org A's project is "simply not found", so a
  delegated insert from org B's tenant is refused by design.
- `project_grants_tenant_isolation` is the one two-sided policy, and its
  comment says "RLS bounds what can be seen, not what can be claimed".

So the first working version of `P4-02` will have to change one of these. The
tempting fixes are the ones this project has refused all along: a
`SECURITY DEFINER` function, a write under the granting tenant's context
chosen by the request, or the owner connection. Each of them brings back "a
misscoped query can't leak data across organizations" (`docs/PLAN/08` Part B)
as a live bug class.

The cache has the same shape. `authz:grants:{org}:{user}:{project}` is keyed
by one organization, and `Cache.InvalidateUser` removes one user's entry.
Revoking a grant held by 400 users means 400 invalidations, which is the
"scan or a guess" that `cache.go`'s own comment rules out of a request path.

**Ask of P4-01 and P4-02**: the spec states which `org_id` a delegated
`user_grants` row carries. It writes a named RLS policy for delegated rows that
admits the granting org, plus the receiving org only through a join to an
active `project_grants` row. It extends `tests/security/isolation_test.go`
with a third organization that must see nothing. No `SECURITY DEFINER` and no
tenant switch without an ADR. **Ask of P4-04**: invalidate by grant rather
than by user. A per-grant generation number that each cache entry is keyed on
turns revocation into one write. Then publish the real revocation window per
path (see T4-13).

## T4-4 — Manager-role inheritance now crosses TB-4

**Card**: `P4-03`.

`docs/PLAN/08` draws `PROJECT_GRANT_OWNER` beneath `PROJECT_OWNER` beneath
`ORG_OWNER`, and "permissions flow downward". In one organization that is
unambiguous. With delegation, the org above `PROJECT_OWNER` is the
**granting** organization. Read literally, that org's owner inherits the right
to assign delegated roles to org B's users. That is a confused deputy: org A
acts inside org B's tenant, on B's people, without B's consent. Meanwhile the
receiving organization's own `ORG_OWNER` inherits nothing over the grant it
received, unless someone decides it does.

Two structural facts make this worse. `manager_roles` has no RLS (recorded in
`20260908000007`), and `scope_id` has no foreign key. A project's uuid and a
grant's uuid live in the same column. `PG-31` also records that nothing writes
`manager_roles` today, so `P4-03` step 5 builds the **first** API that
assigns a manager role. `docs/SECURITY/02` §3 expects that event to be rare
and alerted on.

**Ask of P4-03**: write a truth table with the granting org's
OO/OA/PO, the receiving org's OO/OA, and PGO as rows, and "assign a delegated
role to a receiving-org user", "…to a granting-org user", "…to a third-org
user" and "assign PGO" as columns. Add it to `P2-05`'s combinatorial test.
Every check on a `scope_id` names the role in the same predicate. A PGO row
works only while the grant its `scope_id` names is active, so revoking a grant
and granting again under a new id must not bring the old PGO back to life.
Test that exact sequence. The new manager-role write endpoint is audited at
elevated visibility and notifies the receiving organization's owners.

## T4-5 — Every path that issues without a login re-checks every policy

**Cards**: `P4-04`, `P4-07`, `P4-08`, `P4-10`, `P4-11`, `P4-13`.

`P3-14` fixed the mandate on refresh and silent authorize by adding
`authn.MandateCheck`. The next policy with the same hole is already in the
code. `LoginPolicy.Allows` is called in exactly one place, at the password
step (`internal/login/handler.go:801`). Once `P4-10` adds `github` to the
permitted values, an organization that removes it still has users whose
sessions keep issuing silent codes, and whose refresh families keep issuing
tokens for 90 days (`token.FamilyLifetime`). The policy would apply only to
people who clicked the button again.

Phase 4 adds these issuing paths: SP-initiated SAML on an existing session,
IdP-initiated SAML, the social callback for an already-linked identity, a
delegated session (T4-1), and SCIM reactivation of a user. Each needs the same
answer to five questions. Is the MFA mandate met? Did the session sign in with
a method that is still allowed? Is the user active? If the session is
delegated, is the grant still active? Does the session's organization match
the application's, or does an active grant stand in for that match?

**Ask**: one issuance check, shared the way `MandateCheck` is shared, called
from every path, and refusing when it cannot read. `P4-15` gets a table test
with the paths as rows and the five conditions as columns. Each cell is proven
by breaking that one condition and watching that one test fail. The `amr`
recorded on the session (`P1-11`) is how the "method still allowed" column is
answered, so `P4-10` step 7 writing the provider into `amr` is load-bearing
here and not only for the audit trail.

## T4-6 — `P4-07` threat-models a Service Provider; this service is the Identity Provider

**Card**: `P4-07`.

Several of `P4-07`'s abuse cases are for the consuming side: "XXE via a
crafted assertion", "assertion replay" tracked by assertion ID, and "an
assertion for SP A accepted by SP B". This service **issues** assertions. It
never consumes one. Audience checks and assertion-ID replay caches are the
SP's job. The IdP-side threats are different, and the card leaves them out:

- **The destination comes from the registry, never from the request.** The
  ACS URL and the Audience are looked up by the AuthnRequest's `Issuer`
  against the registered application. `P4-08` step 4 covers the ACS URL, and
  the Audience needs the same rule.
- **Sign the Assertion, not only the Response.** Some SPs verify only one of
  the two. An unsigned assertion inside a signed response is what the
  signature-wrapping attacks on SPs exploit.
- **`InResponseTo` names a stored, single-use request ID**, so one
  AuthnRequest cannot be answered twice.
- **The HTTP-Redirect binding is DEFLATE-compressed.** A size bound on the
  query string does not bound the inflated document. Bound the inflated byte
  count, since decompression bombs have had an advisory in the main Go SAML
  library before.
- **NameID is never email by default.** Use a persistent, per-SP identifier.
  An SP keyed on email hands a deleted user's account to whoever is issued
  that address next (`P1-19` deactivation), and an email shared across SPs
  lets them correlate users.
- **`AuthnContextClassRef` comes from the session's factors**, as T3-2
  required for `amr`. A `RequestedAuthnContext` the session cannot satisfy
  triggers step-up or a `NoAuthnContext` status. It is never silently ignored.

The XML asks need care in Go specifically. `encoding/xml`, which the Go SAML
and XML-DSig libraries are built on, returns a DTD as an uninterpreted
directive and resolves no external entity. **A test that feeds it an XXE or
billion-laughs document passes whatever the configuration is.** That is
vacuous verification by construction. The known Go failure is round-trip
instability, where the parse that verifies a signature and the parse that
reads the document disagree about namespaces. That is why
`mattermost/xml-roundtrip-validator` exists.

**Ask of P4-07**: rewrite the abuse table from the IdP's side using the list
above. Record the library choice as an ADR that names its advisory history at
the pinned version and states that it runs a round-trip validator before
verification. Keep the XXE and entity-expansion tests, but label them as
regression guards against a future parser swap. The tests that must be shown
to fail are signature wrapping and multiple-assertion handling on every
document this service **does** verify: signed AuthnRequests, LogoutRequests,
and uploaded metadata (T4-8).

## T4-7 — SAML in a real browser: cookies, script, and form targets

**Cards**: `P4-08`, `P4-07`.

Three existing, deliberate controls collide with SAML's browser bindings.
Each collision has a tempting fix that weakens the control.

1. **`SameSite=Lax`.** `P4-07`'s Definition of Done says "a session established
   via OIDC satisfies a SAML request without re-authentication". With the
   HTTP-POST binding the AuthnRequest arrives as a cross-site POST, and a Lax
   cookie is not sent on one. The user is prompted every time. The tempting
   fix, `SameSite=None` on the SSO cookie, removes the protection that
   `internal/login/csrf.go`'s double-submit design relies on and that
   `csrf_test.go` asserts.
2. **No script on hosted pages (`PG-40`).** Posting the response to the ACS is
   usually an auto-submitting form, which needs script.
3. **`form-action`.** `P1-27` found that Chrome applies it along the redirect
   chain. A response page with `form-action 'self'` cannot post to the SP.

IdP-initiated SSO has its own problem. If it is a GET URL naming an SP, any
third-party page can make a signed-in user's browser log in to that SP
silently, with a `RelayState` the attacker chose. `P4-08` makes the flow
opt-in, which is right. Opt-in limits which applications are exposed. It does
not fix how the flow is triggered.

**Ask of P4-08**: accept a POST-binding request, store it server-side under a
handle, and 303 to a GET on the issuer origin. Lax cookies are sent on
top-level GET navigations, so SSO works with the cookie unchanged. The
response page is a no-script form with a visible Continue button, or a
hash-pinned inline script under `PG-40`'s rules. Its `form-action` is the
**registered** ACS origin through the existing `formAction()`, never an origin
taken from the request. IdP-initiated login starts only from a POST with a CSRF
token on this service's own launcher page. Its `RelayState` is bounded and
treated as data for the SP, never as a redirect this service follows.
`SessionNotOnOrAfter` is no later than the organization's
`session_lifetime_hours`. The guide states plainly that revoking a session
(`P3-09`) does not end an SP's session unless Single Logout is implemented.
The SSO-reuse test has to run in a browser: `P1-27` is the record of a login
page that passed every curl test and could not log anyone in.

## T4-8 — Metadata, certificates, and an SSRF row with no fetch behind it

**Cards**: `P4-09`, `P4-14`.

`docs/SECURITY/02` §7 names "SAML metadata URL fetching" as the SSRF surface,
and `docs/SECURITY/05` specifies a verification test for it. `P4-09` supports
upload and manual entry only. That is the right scope, but it leaves a
planned test with nothing to test. If it is written anyway, it passes because
no fetch exists.

Uploading metadata replaces the certificate used to verify an SP's signed
AuthnRequests. That makes it a **credential change** on the application, not a
configuration edit. On the other side, the IdP's own signing certificate is
pinned by every SP from the metadata they imported. Rotating it the way
`RUNBOOK-key-rotation.md` rotates JWKS breaks every SAML application at once,
because SPs do not refetch.

**Ask of P4-09**: keep URL import out of Phase 4, and record through the
plan-change process that §7's SAML row and `docs/SECURITY/05`'s SSRF scenario
now point at webhooks (T4-11). If URL import is ever added, it uses the webhook
egress client and nothing else. A metadata upload that changes an SP
certificate is audited like a client-secret rotation. IdP metadata publishes
the *next* signing certificate before it becomes active, and the rotation
runbook includes a staging drill against the SP fixture. Certificate-expiry
warnings go to a place someone watches, with an alert when the warning job
itself stops running. `BL-01` is the record of a healthy-looking timer whose
service died every night.

## T4-9 — Social identity: what counts as the same person

**Card**: `P4-10`.

`P4-10` treats the three providers as one protocol. They are not:

- **GitHub is OAuth 2.0, not OpenID Connect.** There is no ID token, no
  `nonce`, and no `email_verified` claim. Step 3's "validate the provider's ID
  token fully" has nothing to validate. Verified status comes from
  `/user/emails`. The stable identifier is the numeric `id`, not `login`,
  because usernames can be renamed and later reclaimed by someone else.
- **Microsoft's multi-tenant endpoint has one issuer per tenant** (`tid`), and
  its `email` claim was the account-takeover vector in the 2023 "nOAuth"
  disclosure: a mutable, unverified attribute. `provider_subject` without the
  tenant is not a stable identity. `docs/PLAN/04`'s `user_identities` has no
  column for the issuer.
- **Provider mix-up.** With several providers behind one callback, the code
  from one can be redeemed against another unless the state records which
  provider it started with.

Step 4 also contradicts `P4-11`. It offers "require `email_verified` **or**
fall back to explicit linking", and the Definition of Done tests only that an
*unverified* email does not auto-link. Read together, a verified email does
auto-link. `P4-11` step 3 says any email collision prompts for authentication.
`P4-11` is right: "verified" means something different at each provider, and
nOAuth shows it can mean nothing.

Finally, step 5 creates "a local `users` record for every federated user", but
nothing says **in which organization, or who may join**. The login page takes
its tenant from the client. So an organization that allows `google` would let
anyone with a Google account create themselves a user in that organization.
`allowed_login_methods` controls how people sign in, not who may join.

**Ask of P4-10** (amend before work starts): per-provider rules for identity
(`iss`+`tid`+`sub`/`oid` for Microsoft, numeric `id` for GitHub) and for
verified status. Store the issuer with the subject, which is a `docs/PLAN/04`
change. Email never links an account automatically, verified or not. Use one
callback path per provider, and a `state` bound to the browser by a cookie and
to the pending authorize request (`PendingTTL`), that records the provider.
Use query response mode so that cookie can stay Lax (T4-7). Just-in-time
creation of a user is a separate organization setting that defaults to off,
optionally limited to verified email domains. Keep `allowed_login_methods`
per provider (`google`, `microsoft`, `github`, as in `docs/PLAN/08` Part B),
not the single `social` value mentioned in the current OpenAPI description,
because GitHub accounts cost nothing to create and an organization must be able
to refuse GitHub alone.

## T4-10 — Linking is a CSRF target, and "last method" needs a definition

**Card**: `P4-11`.

Step 1 requires "an authenticated session to link", and a session is exactly
what cross-site request forgery rides on. An attacker completes the provider
leg with their own Google account and gets the victim's browser to load the
callback. If the callback links whatever arrives to whoever is signed in, the
attacker's identity is now a sign-in method for the victim's account. It is a
quiet takeover that survives a password reset.

Two lessons from Phase 3 apply. First, the SSO session cookie lives on the
issuer origin, not the console's. `P3-10` had to move passkey registration to
`/account/passkeys` on the issuer origin, with a recent sign-in required and a
`return_to` checked against registered origins. Linking has the same shape and
should follow the same precedent. Second, the "documented path when the
provider is down" (`P4-10` step 8) is a recovery path. If that path is a
self-service password reset, it gives a password to users of an organization
that allows only social sign-in, and it hands the account to whoever controls
the mailbox.

**Ask of P4-11**: start linking from the hosted issuer-origin page, which
requires a recent sign-in. The callback accepts a link only when its `state`
matches a link-intent cookie set by that page for that user. Send the user a
notification out of band for every link and unlink. Define "last remaining
method" as a method that can complete sign-in by itself **and** is permitted
by the organization's current `allowed_login_methods`. A password the policy
forbids is not a remaining method, and neither is a second factor. **Ask of
P4-10** step 8: the provider-outage path is a named, audited, administrator-
initiated flow that respects the organization's policy, not a self-service
default.

## T4-11 — Webhook egress from this deployment specifically

**Card**: `P4-12`.

`P4-12` names SSRF as the central concern, and it is. What the card cannot
list is what this deployment can reach. From inside the `authservice`
container on the tunnel stack (`deploy/vm/docker-compose.tunnel.yml`):

- `mailpit:8025`, whose HTTP API reads every message the service has sent:
  verification links, password resets, and, once anomaly notifications are
  switched on, "not me" links.
- `postgres`, `redis`, and the service's own admin listener on `:9090`.
- The host's other stacks on ports above 10000, through the Docker gateway,
  and the rest of `10.1.200.0/24`.
- The service's **own public hostname**. It resolves to Cloudflare addresses,
  passes every private-range filter, and comes back in through `cloudflared`,
  which is the easiest amplification loop to build.

The card's wording, "re-resolve DNS at request time", describes a
time-of-check/time-of-use gap: check one resolution, then let `net/http`
resolve again. Go's default client also follows ten redirects and honours
`HTTP_PROXY`. With a proxy set, the address being checked is the proxy's, not
the destination's.

**Ask of P4-12** (amend step 2's wording): check the **connected** IP in
`net.Dialer.Control`, so redirects, rebinding and happy-eyeballs fallbacks all
pass through one check. Also:

- No redirects and no environment proxy.
- `https` on 443 only, outside development.
- Deny loopback, private, link-local, CGNAT, IPv4-mapped IPv6, single-label
  hostnames, and this service's own issuer and console hostnames.
- Bounded connect, read and total time.
- The delivery log (step 7) records status and latency, **never the response
  body or headers**. With the body stored, a blind SSRF becomes a read.

The SSRF suite must run on the compose network against a target that would
answer, such as `http://mailpit:8025/api/v1/messages`. Otherwise its refusals
prove nothing.

## T4-12 — Webhooks as a data channel: secrets, payloads, persistence, silence

**Card**: `P4-12`.

- **The data model cannot sign.** `docs/PLAN/04` stores
  `webhook_endpoints.secret_hash`, but HMAC signing needs the key itself at
  send time. Someone following the model either cannot sign, or stores the
  plaintext in a column named `_hash`. It needs to be sealed the way `P3-02`
  seals factor secrets.
- **Payloads are not the audit row.** `webhook_deliveries.event_id` points at
  `events`, which invites sending the row as the payload. `P1-14` built
  `user.login.failed` for operators: IP address, user agent, reason. A
  third-party endpoint is a different audience. Event names also disagree:
  `docs/PLAN/05` says `login.failed`, while the code emits `user.login.failed`.
  A subscription to a name that is never emitted delivers nothing, and nobody
  finds out.
- **A webhook is persistence.** A compromised `ORG_ADMIN` who registers a
  `user.login.success` endpoint keeps receiving events after their password
  is reset and their sessions are revoked.
- **Consumers will deprovision from these events.** A delivery worker that
  stops is `BL-01` again: no errors, only an absence. A `role.assigned`
  retried after a later `role.revoked` restores access at the consumer.
- **Delegation creates two audiences.** A B user's login at A's application
  is an event both organizations may subscribe to.

**Ask of P4-12**: seal the secret, and route the `docs/PLAN/04` change
through the plan-change process. Sign `id.timestamp.body`, with two active
secrets during rotation. Build payloads from a per-event-type allowlist and
reject unknown `event_types` on write. Registering or changing an endpoint
requires `ORG_OWNER` and a recent sign-in, is audited at elevated visibility,
notifies the owners, and appears in `docs/SECURITY/04`'s account-compromise
checklist. Each payload carries a per-endpoint sequence number so receivers
can discard stale events. Alert on the age of the oldest undelivered event,
not on failure counts. The spec decides which organization's endpoints
receive a delegated login, and a test shows the other organization receives
nothing it did not subscribe to.

## T4-13 — SCIM, only if the gate opens

**Card**: `P4-13`.

The gate is right, and nothing here argues for opening it. If a named
integration does open it, the SCIM core schema includes writable
`password`, `roles`, `entitlements` and `groups` attributes. A provisioning
token that accepts them can set an administrator's password, or grant roles
that no local administrator assigned. Deprovisioning as "deactivation" (step
5) does nothing to a 90-day refresh family unless it revokes that family.
Rate limits keyed by IP address meet `PG-19`'s finding: behind the Cloudflare
tunnel, and from a single identity provider, every request looks like one
client.

**Ask of P4-13**: ignore or reject `password`, `roles`, `entitlements` and
`manager_roles` in every form. `groups` map to role keys only through a
mapping an administrator configures in this service. No SCIM operation touches
a user who holds a manager role. Deactivation revokes sessions and refresh
families and invalidates the authz cache in the same transaction, using
`P3-09`'s machinery. Reactivating a user whom a local administrator
deactivated is refused. Filters are parsed into parameterized queries. Tokens
are hashed, bound to one organization, and rate-limited per token.

## T4-14 — The claims, and the evidence behind them

**Cards**: `P4-14`, `P4-15`, `P4-16`.

- **"Immediately" is true on one path.** `docs/PLAN/17`'s criterion says
  "revoking a Project Grant **immediately** removes access". That holds for
  `/v1/authz/check`, and for a cached decision only when invalidation landed
  (`authz.DefaultTTL`, 30 seconds, when it did not). A consumer reading token
  claims keeps the role for up to `AccessTokenLifetime`, which is 10 minutes.
  A SAML SP keeps its session until `SessionNotOnOrAfter`. An acceptance run
  that checks only `/v1/authz/check` will record "immediate", and the claim
  will be false for most consumers.
- **The docs audit will lose its teeth.**
  `TestNoPageDescribesAPhase4CapabilityAsAvailable` blocks every Phase 4
  claim together. The first card to ship will be tempted to delete it, and
  from then on nothing stops a page claiming SCIM, Single Logout, or a
  provider that was never finished.
- **"SAML fuzzing runs in CI" can pass on zero executions.** `go test -fuzz`
  with a pattern that matches no target prints a warning and passes. `P3-14`
  found a workflow that had not parsed for a phase and a half.

**Ask of P4-16**: record evidence per path, as T4-3 lists them, and have the
public revocation window (`P4-04` step 4) state each number. A `docsdrift`
test asserts that the page states them. **Ask of P4-14**: invert the drift
test one capability at a time as each ships, the way `P2-03`'s guard was
designed to be inverted. SCIM stays blocked unless the gate opened, Single
Logout stays blocked unless it was built, and the guide index lists exactly
the providers that pass end to end. **Ask of P4-15**: the fuzz job asserts a
non-zero execution count and is shown to fail on a seeded crashing input.

---

## Summary

| Finding | Cards | Change a card before work starts? |
|---|---|---|
| T4-1 Delegation has no sign-in path | `P4-04`, `P4-01`, `P4-02` | **Yes.** Cross-org sign-in and whose policy applies are undesigned, and the plan (`docs/PLAN/08` Part C) is silent |
| T4-2 Readers ignore the grant | `P4-02`, `P4-04` | **Yes.** Definition of Done must test the token and `/v1/authz/check`, not only the write |
| T4-3 Two-tenant row, per-user cache | `P4-01`, `P4-02`, `P4-04` | **Yes**, in the `P4-01` spec: the RLS policy and invalidation by grant |
| T4-4 Inheritance across TB-4 | `P4-03` | In the spec: the cross-org truth table |
| T4-5 One issuance check for every path | `P4-04`, `P4-07`, `P4-08`, `P4-10`, `P4-11`, `P4-13` | **Yes.** Add the path × condition table to `P4-15` now, so each card builds against it |
| T4-6 IdP, not SP; XML in Go | `P4-07` | **Yes.** The abuse table is for the wrong side of the protocol |
| T4-7 Cookies, script, `form-action` | `P4-08`, `P4-07` | In the spec |
| T4-8 Metadata, certificates, §7 | `P4-09`, `P4-14` | No card change; `docs/SECURITY/02` §7 and `/05` through the plan-change process |
| T4-9 Social identity | `P4-10` | **Yes.** Step 4 contradicts `P4-11`, GitHub is not OIDC, JIT creation is undecided, `user_identities` needs the issuer |
| T4-10 Linking CSRF, last method | `P4-11`, `P4-10` | In the spec |
| T4-11 Webhook egress | `P4-12` | **Yes**, step 2's wording |
| T4-12 Webhook data channel | `P4-12` | **Yes.** `secret_hash` cannot sign (`docs/PLAN/04`) |
| T4-13 SCIM | `P4-13` | Only if the gate opens |
| T4-14 Claims and evidence | `P4-14`, `P4-15`, `P4-16` | Before `P4-15` starts |

Plan-document gaps found, to raise through `CODEOWNERS` rather than fix in code:
`docs/PLAN/08` Part C (cross-organization sign-in and whose policy governs),
`docs/PLAN/04` (`webhook_endpoints.secret_hash`, `user_identities` issuer),
`docs/PLAN/05` against `internal/audit` (event names), `docs/PLAN/17` Phase 4
("immediately"), `docs/SECURITY/02` §7 and `docs/SECURITY/05` (a metadata fetch
that is not planned).

## What this review cannot see

- **No Phase 4 code exists.** Every finding comes from a card read against
  code that will have to change, not from an implementation. The spec for each
  card is where these asks either become rows or get argued down.
- **Library and provider behaviour is from general knowledge.** Advisory
  histories, Microsoft's claim semantics, and GitHub's OAuth capabilities all
  change. Each must be checked against the pinned version and current provider
  documentation when `P4-07` and `P4-10` start.
- **The staging network beyond the compose file.** Zed's other services on the
  VM and the rest of `10.1.200.0/24` are not listed in the repository. T4-11's
  deny rules can be tested only against targets someone actually names.
- **Consumer behaviour.** Whether integrators call `/v1/authz/check` or trust
  claims decides how much T4-14's ten-minute window matters. This service can
  document that choice but cannot observe it (`docs/SECURITY/02` §14).
- **Whether a SCIM integration will ever be named.** T4-13 is conditional on
  a decision that has not been made.

## Recorded

Phase 4 should not start on Project Grants until `P4-04` has absorbed T4-1.
That finding decides whether T4-2 and T4-3 are describing a feature or a
bypass. SAML and social login can start in parallel once `P4-07` and `P4-10`
are amended per T4-6 and T4-9. T4-5's table goes into `P4-15` first, so no card
can call itself done while one of its paths is missing a row.

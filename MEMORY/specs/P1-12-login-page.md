# P1-12 — Hosted Login Page

Feature specification, per `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication.

---

## 1. Business Objective

The one page every application in the estate redirects to. It is the only place a password is ever typed, which makes it the single most valuable target in the system and the single best place to get the defences right once.

It is also the last piece of the Phase 1 login flow. `P1-06` issues codes, `P1-07` redeems them, and neither is reachable by a human without this.

## 2. Actors

| Actor | Interaction |
|---|---|
| An end user | Types an email and a password, possibly with a screen reader, possibly with JavaScript disabled |
| `/oauth/authorize` (`P1-06`) | Sends them here with an opaque pending-request id and expects them back |
| An attacker probing for accounts | Submits addresses and reads the differences |
| An attacker who controls a page the user visits | Tries to frame this page, or to make the user's browser POST to it |

## 3. Functional Requirements

- **FR-1** Server-render from the auth service itself, not the console SPA.
- **FR-2** The page works with JavaScript disabled.
- **FR-3** Email/username and password, with CSRF protection on the POST.
- **FR-4** `UI-UX/15`'s field-label-helper-error pattern, and `UI-UX/13`'s accessibility requirements.
- **FR-5** A wrong password and a nonexistent account produce identical responses.
- **FR-6** The pending authorization request travels by opaque server-side reference only.
- **FR-7** Restrictive headers on this page specifically: strict CSP with no inline script, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.
- **FR-8** Organization branding — logo and accent only.
- **FR-9** A "forgot password" entry point. The reset flow is `P1-19.4`.
- **FR-10** Support `prompt=login` re-authentication.

## 4. Non-Functional Requirements

- **NFR-1** No client-side JavaScript at all. Not "degrades gracefully" — none, so there is no script for a CSP to have to permit and no XSS sink to reason about.
- **NFR-2** Nothing user-supplied is rendered without escaping, and nothing from the query string is rendered at all. See §11.
- **NFR-3** Response time for a nonexistent account matches a wrong password. `P1-01`'s `VerifyDummy` does the work; this consumes it.
- **NFR-4** No password reaches a log, an error, or an audit payload.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-01` | Password verification, and the equal-cost not-found path |
| `P1-02` | Password expiry (`Expired`) at login |
| `P1-11` | Creates the session on success |
| `P1-06` | Supplies the pending request and finishes the flow |
| `P0-12` | Login success and failure are audited |

Depended on by: `P1-13` (rate limiting hooks into the failure path), `P1-14` (authentication audit events), `P1-26` (the demo applications).

## 6. Database Changes

**One additive migration**, for a gap this task exposes.

### PG-16 — organization branding has nowhere to live

Registered in `TASKS/BACKLOG.md`.

`PLAN/01-PRODUCT-SCOPE.md` and `UI-UX/05-DESIGN-SYSTEM.md` both specify per-organization branding as logo plus accent colour, and `UI-UX/08` gives an Organization Settings screen for editing it. `console/src/branding/branding.ts` already implements applying it, carefully — a closed union of overridable tokens so that `color-danger` can never be re-pointed.

`PLAN/04` § `organizations` has `settings jsonb`, and `PLAN/08` Part B enumerates its shape: `password_policy`, `mfa_required`, `session_lifetime_hours`, `allowed_login_methods`. No branding. So a capability that is specified, designed, and half-implemented in the console has no column, and step 7 of this task cannot be satisfied by reading anything.

Closed by treating `settings.branding` as a documented key with the shape `{ "logo_url": string, "accent_color": string }` — no schema change, because `settings` is already `jsonb` and already the place per-organization policy lives. What it needs is to be *specified* rather than invented at each read.

**Nothing writes it yet.** `P2-14` makes it editable. Until then every organization renders unbranded, and the login page says so in the record rather than implying otherwise.

`PLAN/08` Part B should be amended to list the key.

## 7. API Contract

Three routes, all HTML, none in the JSON API:

- `GET /login?request=<id>` — renders the form
- `POST /login` — verifies, creates a session, resumes the authorization
- `GET /login/forgot` — FR-9's entry point

**They are deliberately NOT in `openapi/openapi.yaml`**, and that is a change from the first draft of this section, which said they should join it "for the same reason `/oauth/authorize` did".

That reason does not carry. `/oauth/authorize` is in the spec because it is a protocol endpoint an integrator codes against: a consumer builds its URL, reads its redirect, and needs the parameter list to be a contract. Nobody ever calls `/login` — a browser is its only client, it is reached only by a redirect this service itself issues, and its form fields are an implementation detail that may change without any consumer noticing. `PLAN/20` renders `/docs/api-reference` directly from the spec, so putting it there would publish an "endpoint" to integrators that no integrator should ever touch, with a request and response shape we do not intend to keep stable.

Nothing checks the reverse direction — `openapi-shipped-paths.py` fails on documented-but-unshipped, not on served-but-undocumented — so this is a decision rather than a rule being evaded. It should be revisited if a served-surface audit is ever added, and the answer then is likely a separate list of browser routes rather than an entry in the API contract.

## 8. Frontend Changes

None in `console/`. That is the point of FR-1: the login page must not depend on the console's deploy cycle, and must work when the console is broken.

The page carries its own CSS inline in a `<style>` element with a CSP nonce, reusing the token *values* from `UI-UX/05` rather than importing anything. Duplication on purpose, and the same trade `check-brand-tokens.mjs` already governs for the public site.

## 9. Backend Changes

New package `internal/login`:

```go
type Handler struct { ... }
func (h *Handler) ShowForm(w, r)   // GET /login
func (h *Handler) Submit(w, r)     // POST /login
```

`internal/oauth/authorize` gains an exported way to finish a pending request:

```go
func (h *Handler) Resume(w, r, req Request, s session.Session)
```

That seam is deliberate. The login page must not re-derive a redirect URI, re-check a client, or re-validate a scope — all of that was done at `/oauth/authorize` before the request was stored, and doing it twice is two places for the exact-match rule to be got wrong. The login page authenticates a user and hands the *already validated* request back.

## 10. Authorization Rules

None. This endpoint's job is to establish identity, not to check permissions. It must be safe to reach unauthenticated, repeatedly, from anywhere.

## 11. Validation, and what is never rendered

| Input | Handling |
|---|---|
| `request` | An opaque id. Looked up server-side; never rendered, never echoed |
| Email/username | Escaped by `html/template`; re-rendered into the field so a typo need not be retyped |
| Password | Never re-rendered, not even on failure |
| CSRF token | Compared in constant time |
| Anything else in the query string | **Not rendered at all** |

That last row is the abuse case about reflected XSS through `error` or `state`. The strongest answer is not to escape them carefully — `html/template` would do that correctly — but to have no code path that puts a query parameter on the page. An error is looked up from a fixed table by a known key, so the worst a crafted URL can do is select a message we wrote.

## 12. Error Handling — uniformity

**A wrong password and a nonexistent account must be indistinguishable in body, status, headers and timing.** This is the enumeration defence and it is easy to satisfy accidentally and lose accidentally.

- Body: one message, "Your email or password is incorrect."
- Status: `200` for both, re-rendering the form. Not `401` — a status that differs from the success path's `302` is fine, but it must not differ *between* the two failures.
- Headers: identical.
- Timing: `P1-01`'s `VerifyDummy` performs a real Argon2 computation on the not-found path. That was measured at a 0.88 ratio and verified by short-circuiting it.

Other conditions:

| Condition | Behaviour |
|---|---|
| Password expired (`P1-02`) | Distinct message — the user is authenticated, so telling them is not disclosure. The reset flow is `P1-19.4`; until then this is a dead end and says so |
| Account locked or deactivated | The **same** message as a wrong password. "This account is locked" confirms the account exists |
| Pending request missing or expired | A page explaining the login took too long, with no link onward — we cannot know where they came from without a request to read |
| CSRF token missing or wrong | Re-render with a generic error. Usually a stale tab, not an attack |
| No `request` at all | The login page has no meaning outside an authorization flow. Explain, do not redirect |

## 13. Edge Cases

| Case | Handling |
|---|---|
| User already has a session and `prompt=login` sent them here | Authenticate again. On success the session **this browser was carrying** is revoked with `session.ReasonReauth`, because its cookie is being overwritten and an unreachable live session still appears on the sessions screen and still authorises a refresh token. Every OTHER session the user has is untouched — `/oauth/authorize` is explicit that `prompt=login` asks somebody to prove themselves again, not to be logged out everywhere. A session belonging to a different user in the same browser is also untouched |
| Two tabs submit the same pending request | The first wins; the second sees the expired-request page. `LoadPending` is single-use |
| JavaScript disabled | Everything works. There is none |
| Password manager autofill | A plain form with standard `autocomplete` attributes |
| Very long email | Bounded before lookup |
| Submit with both fields empty | Field-level errors, no lookup, no timing signal |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Username enumeration through error text | `SECURITY/02` §12 | One message for every credential failure |
| A-2 | Enumeration through status or headers | Same | Identical status and headers, asserted byte for byte |
| A-3 | Enumeration through timing | Same | `P1-01`'s equal-cost not-found path |
| A-4 | Enumeration through a locked-account message | Same | Locked and deactivated give the same message as wrong |
| A-5 | Clickjacking the form | `SECURITY/02` §6 | `X-Frame-Options: DENY` **and** `frame-ancestors 'none'` |
| A-6 | CSRF against the login POST | `SECURITY/02` §5 | Per-request token, constant-time comparison, `SameSite` cookie |
| A-7 | Reflected XSS via `error` or `state` | `SECURITY/02` §6 | No query parameter is rendered; errors are selected from a fixed table |
| A-8 | Stored XSS via an organization's logo URL | `SECURITY/02` §6 | Rendered as `src` only, `img-src` restricted, `javascript:` refused |
| A-9 | Password leaking into the referer of an outbound link | — | `Referrer-Policy: no-referrer` |
| A-10 | The page loading an attacker's script | `SECURITY/02` §6 | `default-src 'none'`; there is no script to permit |
| A-11 | The pending request replayed | — | Single-use; `P1-06`'s `LoadPending` is a `GETDEL` |

## 15. Logging / Audit Requirements

| Event | Payload | Never |
|---|---|---|
| `user.login.success` | user id, org id, IP, `auth_methods` | the password, the CSRF token |
| `user.login.failed` | org id, IP, reason class (`credentials`, `locked`, `expired`) | the password, **and not the submitted address** |

The address is deliberately absent from the failure event, and it is a real trade. Including it would make credential-stuffing forensics much easier. It would also write every mistyped and every probed address into an append-only table with 24-month retention, turning the audit log into a list of addresses somebody tried — which is the artefact an attacker who reaches the log most wants. `P1-13`'s rate limiting keys on the address in Redis with a short TTL, which is where that data belongs.

Metrics: `auth_login_attempts_total{outcome}` — already defined by `P0-11` and finally used.

## 16. Security Controls

- No JavaScript, so `default-src 'none'` is achievable rather than aspirational.
- The inline stylesheet is whitelisted by its **SHA-256 hash, not a nonce**. A nonce changes every response, which would make acceptance criterion 2 — two failed logins producing byte-identical responses — impossible to state, let alone test. A hash is also the stricter policy: it authorises one exact block of CSS rather than whatever the server put a nonce on.
- `X-Frame-Options: DENY` and `frame-ancestors 'none'`, belt and braces.
- `Referrer-Policy: no-referrer`.
- Per-request CSRF token, constant-time comparison.
- One message for every credential failure; equal-cost not-found path.
- No query parameter rendered.
- The pending request travels by opaque id, single use.
- The previous session revoked on re-authentication.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Header assertions: CSP, `X-Frame-Options`, `Referrer-Policy`, `Cache-Control` |
| Unit | The rendered page contains no `<script>` and no inline event handler |
| Unit | CSRF: missing, wrong, reused, correct |
| Integration | **Wrong password and unknown account produce byte-identical responses** — body, status and every header compared |
| Integration | A locked account is indistinguishable from a wrong password |
| Integration | Login creates a session, sets the cookie, and redirects to the client with a code |
| Integration | The pending request is single-use |
| Integration | `prompt=login` revokes the previous session |
| Integration | An expired password is refused with its own message |
| Security | A crafted `?error=<script>` renders nothing of it |
| Accessibility | Every input labelled; the error summary linked; tab order complete |

The byte-identical test is the one to write carefully. Comparing "both say invalid" would pass while a `Content-Length` differed.

## 18. Acceptance Criteria

1. The page renders and submits with JavaScript disabled.
2. Wrong password and unknown account are indistinguishable in body, status and headers.
3. A missing or wrong CSRF token is refused.
4. `X-Frame-Options: DENY` and `frame-ancestors 'none'` are both present.
5. A successful login lands back at the client with a code.
6. Nothing from the query string appears in the HTML.

## 19. Definition of Done

The task's six items plus the global DoD. The WCAG item is satisfied by construction and asserted where it can be — labels, the error summary, focus order — with the caveat that automated checks do not establish accessibility, which `PF-50`'s audit is for.

## 20. Implementation Sequence

1. The template and its headers, with the header tests.
2. CSRF.
3. The credential path, and the uniformity tests.
4. Session creation and `Resume`.
5. Branding, and `PG-16`.
6. Audit and metrics.

## 21. Rollback Strategy

No schema change. Rolling back removes the page, which makes interactive login impossible while leaving silent SSO and the token endpoint working — so an existing session keeps functioning and only new logins fail.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| The uniformity of the two failures is lost in a later edit | Medium | High | The byte-identical test compares whole responses, so any divergence fails |
| Somebody adds JavaScript | Medium | Medium | The CSP would block it and the test asserting no `<script>` would fail first |
| The reset flow never arrives, leaving the expired-password path a dead end | Medium | Medium | `P1-19.4` owns it; the message says a reset is needed rather than implying one is available |
| Branding is read but never written | Certain until `P2-14` | Low | `PG-16` records it; every organization renders unbranded and the record says so |

# P3-08 — Login Anomaly Detection

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-08 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Audit & Anomaly Detection, `docs/PLAN/13-OBSERVABILITY.md` § Alerting, `docs/SECURITY/03-DETECTION-AND-MONITORING.md` |
| **Depends on** | `P1-11` (sessions), `P1-14` (audit) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

Tell a user when their account was used in a way that does not look like them —
a device they have never signed in from, a place they have never been, two
logins no human could have made in the time between them.

The whole value is in the user **acting** on the notice, and the whole risk is
that they stop reading it. A notification on every login trains people to
ignore them, which is worse than none: it spends the user's attention on noise,
so the one notice that matters is filtered along with the rest. The card says
so in step 5, and it is the constraint that shapes everything below.

## 2. Actors

| Actor | Interest |
|---|---|
| A user signing in normally | Must not be notified |
| A user on a new laptop | Notified once, and able to dismiss it by ignoring it |
| A user whose password was stolen | Notified, and able to act in one click |
| An operator | Needs anomaly volume in monitoring, and a signal for the rare real case |
| A privacy-conscious organization | An identity provider must not become a surveillance system |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | A login from a device the user has not used before is detected |
| F-2 | A login from a coarse location the user has not used before is detected |
| F-3 | Two successful logins whose locations cannot both be true in the elapsed time are detected |
| F-4 | Detection is always recorded (audit + metric); **notification is a separate, deployment-level switch** |
| F-5 | A notified user has a "this wasn't me" path that revokes every session and forces a password change |
| F-6 | Anomalies notify; they do not trigger step-up (§ 20, ADR-024) |

## 4. Non-functional requirements

- Detection runs **after** the session is created and **must not** fail or delay
  the login. An anomaly detector that can refuse a sign-in is a new way for
  sign-in to break.
- No invasive fingerprinting. Only what `sessions` already stores: the IP and
  the user agent (`docs/PLAN/04`).

## 5. Dependencies

`P1-11`'s sessions table, which already records `ip` and `user_agent` per
session, is the history. `P1-19`'s mailer sends the notice. `P1-19`'s reset
token and set-password page are the "force a password change" half of F-5.

**A geolocation source is a new dependency, and no plan document names one.**
See § 20.

## 6. Data model changes

**None for detection.** The history is `sessions` — every login already writes a
row with its IP and user agent, so "has this user signed in from here before"
is a query rather than a new table.

Location is **not stored**. It is derived at detection time from the IP that is
already stored. Persisting it would put a city-level movement history of every
user into a table with long retention, which is exactly the privacy posture the
card warns against — and it would go stale the moment the geolocation data was
updated.

One additive column is needed for F-5: nothing. The "this wasn't me" link uses
`P1-19`'s existing `user_tokens` with a new purpose value.

**Correction from building it: one migration was needed after all.**
`user_tokens.purpose` is guarded by a CHECK constraint listing the allowed
values, so a new purpose is a constraint change. `20260913000033` widens it by
`report_not_me` — backward compatible in both directions, and the down
migration deletes outstanding report links before narrowing it again, because a
row the restored constraint forbids would make the rollback fail halfway.

## 7. API contract

No Management API change. One hosted page:

```
GET  /account/not-me?token=...   confirmation page
POST /account/not-me             revoke every session, send a reset link
```

A two-step confirm rather than acting on the GET, because a mail client that
pre-fetches links (several do, for link previews and malware scanning) would
otherwise sign the user out of everything the moment the notice arrived.

## 8. Frontend changes

The notification email and the confirmation page. No console screen.

## 9. Authorization rules

The "not me" token is single-use, short-lived, bound to one user, and grants
exactly one power: revoking that user's own sessions and sending a reset link to
their own address. It cannot sign anybody in and cannot set a password.

**Found while building it: the set-password page did not check a token's
purpose.** It consumed whatever a token carried, which was harmless while every
issued purpose legitimately set a password. `report_not_me` is the first that
must not — a week-long link in an email, designed to sign its owner out — so
without a fix anybody holding one could have opened `/password/set` and taken
the account. `user.SetsPassword` is now an allow-list (`invite`, `reset`) checked
before the token is consumed, and the report page applies the mirror rule. Both
directions have tests that fail with the check removed.

**"Forces a password change" means the password is cleared.** Revoking sessions
alone leaves whoever signed in holding a password that still works, so the
report sets `password_hash` to NULL in the same transaction. `authn.Authenticate`
already answers a missing hash exactly as it answers a wrong password, at the
same cost, so the thief learns nothing from trying it. The owner gets back in
through the reset link, or the forgotten-password page if the mail is lost.

## 10. Validation rules

| Input | Rule |
|---|---|
| The token | `P1-19`'s rules: hashed at rest, single use, expires |
| A user agent | Normalised to browser family + OS family before comparison — see § 12 |
| An IP | Used as-is for location; never logged in full alongside a location |

## 11. Error handling

Detection errors are logged and swallowed. The login has already succeeded, and
a detector that could turn a successful sign-in into a 500 would be a
denial-of-service against every user whenever the geolocation file was
unreadable.

## 12. Edge cases

**The device fingerprint is the part most likely to be wrong in a way that
generates noise**, so its rules are stated precisely:

| Case | Treated as |
|---|---|
| Same browser, minor version update | **Same device.** Browsers auto-update every few weeks; comparing full version strings would notify every user on every update |
| Same browser family, different OS | New device |
| An empty or absent user agent | Unknown, and **never** notified — an empty string matching nothing would notify on every API-driven login |
| The user's very first login | **Never an anomaly.** There is no history to be unlike |

| Case | Behaviour |
|---|---|
| An IP with no location in the data | New-location detection is skipped for that login, not guessed |
| A private or loopback IP | No location. Treated as unknown |
| Impossible travel where one location is unknown | Not detected. Absence of data is not evidence of travel |
| Two logins minutes apart from the same city | Not travel |
| A VPN hop between continents | **Impossible travel, correctly.** It is also a false positive for the user, which is why travel notifies rather than blocks |
| Two notices in quick succession | Issuing a report link retires the previous one, so **only the newest notice's link works**. The invalid-link page names that as the likely cause. The mail quota is consulted before a link is issued, so a notice that is never sent cannot kill the link in the one that was |
| A user who was signed out everywhere signs back in on the same device | **Familiar.** Revoked sessions count as history |
| The stranger enrolled their own factor before the report | **Not handled here.** The report clears the password, not factors; an owner locked out by a factor they do not hold needs the administrator reset (`P3-04` F-6) |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | A thief signs in from a new device | F-1 notifies the real user |
| A-2 | A thief uses the "not me" link to lock the user out | It revokes sessions and sends a reset **to the user's own address**; it cannot set a password or sign anybody in |
| A-3 | Flooding a user with notices | At most one notice per device-or-location, and never for a login that matched history |
| A-4 | Using notices to learn where a user signs in from | The notice goes only to the user's own address, and carries a coarse location only |
| A-5 | A pre-fetching mail client triggers "not me" | The GET only shows a confirmation; the action is a POST |

## 14. Logging and audit

| Event | When |
|---|---|
| `user.login.anomaly` | Any detection. Payload: which signals fired, the coarse location if known, never the full IP |
| `user.login.reported_not_me` | The user confirmed "this wasn't me". Payload: `sessions_revoked: "all"`, `refresh_tokens_revoked` (count), `password_cleared`, `reset_link_issued` |

Metric: `auth_login_anomalies_total{signal}` — labelled by signal, because unlike
refresh reuse this one **does** have a routine baseline (people buy laptops), and
an operator needs to see which signal moved.

## 15. Security controls

- Detection cannot fail a login.
- Location is derived, never stored.
- The "not me" action is a confirmed POST, not a GET.
- Notifications are off unless a deployment turns them on.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | The fingerprint normalisation table; the travel computation with synthetic coordinates, which is what the DoD asks for |
| Integration | Real Postgres: detection against real session history; the "not me" flow end to end |
| Mutation | Every control above |

## 17. Acceptance criteria

The card's Definition of Done, **except** the false-positive measurement against
staging traffic — see § 21.

## 18. Implementation sequence

1. Fingerprint normalisation.
2. The travel computation.
3. Detection against session history.
4. The locator interface and its file-backed implementation.
5. Audit, metric, and the notification switch.
6. The "not me" flow.
7. ADR-024, then tests and the mutation run.

## 19. Rollback strategy

A rolled-back build stops detecting and no longer serves `/account/not-me`, so
links in notices already sent answer 404 until they expire. Migration
`20260913000033`'s down deletes outstanding report links (see § 6). A password a
report already cleared stays cleared — the reset link that went with it still
works on the previous build, which serves `/password/set`.

## 20. Technical risks

**No plan document names a geolocation source, and both F-2 and F-3 need one.**

`docs/PLAN/09` asks for notifications on "new location" and the card asks for
impossible travel, and neither says where a location comes from. The realistic
sources all carry a decision the plan should own:

| Source | Cost |
|---|---|
| A commercial GeoIP database (e.g. MaxMind GeoLite2) | A licence agreement, a registration, and an update pipeline |
| A free database (e.g. DB-IP Lite, CC BY 4.0) | Attribution requirements, and coarser data |
| An online lookup API | **Every user's login IP sent to a third party** — unacceptable for an identity provider |

So this task builds detection **behind a `Locator` interface**, ships a reader
for the MaxMind DB file **format** (which several free and commercial databases
use), and **does not ship any database**. An operator supplies the file; with no
file configured, new-location and travel detection are off and new-device
detection still works. Recorded as `PG-41`.

**Notify, don't step up — ADR-024.** The card asks for the decision to be
recorded, and the reasoning is short: the signal is too noisy to gate on. A VPN,
a phone switching from Wi-Fi to cellular, a train crossing a border — every one
of those is an anomaly by these rules and none is an attack. Step-up on a false
positive interrupts somebody mid-task; notification on a false positive costs
them a glance at an email.

## 21. What this task does not deliver

- **"The false-positive rate is measured against real staging traffic before
  enabling notifications broadly."** **Not met, and not claimable**: staging has
  been unreachable since the deploy key was lost. What ships instead honours the
  *intent* of that item — **notifications default to off**, detection and metrics
  default to on — so nothing is enabled broadly before the rate is known. The
  metric is exactly what the measurement needs. The item stays open on the card.
- **A geolocation database.** Operator-supplied; see § 20.
- **Step-up on anomaly.** Decided against; ADR-024.

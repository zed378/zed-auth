# 16 — Mobile UX

Per `12-RESPONSIVE-BEHAVIOR.md`'s scope decision, full mobile optimization is required for exactly **one** screen group: **Personal account settings** (persona Ayu, `01-USER-PERSONAS.md`) — since end users, unlike admins, may reasonably reach these screens from a phone (e.g. immediately after realizing they lost a device, per `02-USER-JOURNEYS.md` Journey 4).

## In Scope for Mobile

- **Personal account settings**: profile, own MFA management, own sessions list/revoke.
- The **login/MFA/consent screens** the hosted login flow shows (technically outside this console's IA per `PLAN/06-FRONTEND-ARCHITECTURE.md`, but designed under the same mobile principles here since end users log into consumer apps from mobile constantly).

## Explicitly Out of Scope for Mobile

- All admin-facing screens (`08-PAGE-SPECIFICATIONS.md`'s full inventory minus Personal account settings) — per `12-RESPONSIVE-BEHAVIOR.md`, these show an "unsupported width" message below tablet size rather than a mobile-adapted layout.

## Mobile-Specific Design Decisions (In-Scope Screens Only)

- **Single-column layout**, full width, no side-by-side panels.
- **Bottom-anchored primary actions** (e.g. "Revoke session") for thumb reachability, rather than top-of-screen buttons requiring an awkward reach.
- **Session revocation stays a single-tap, immediately-effective action** (`04-USER-FLOWS.md` Flow 4) — the low-friction requirement from that flow is, if anything, more important on mobile, since this is often exactly where a security-anxious user reaches for their console first.
- **MFA enrollment (TOTP)** on mobile must account for the fact the user may be trying to scan a QR code shown on a *different* device — provide a manual entry-code fallback (a copyable text code, per `11-MICRO-INTERACTIONS.md`'s copy-to-clipboard pattern) alongside the QR code, since scanning a QR code with the same phone that needs to display it is impossible.
- **Touch target sizes** meet or exceed the minimums specified in `13-ACCESSIBILITY.md`, which already apply universally but deserve particular attention here given the mobile input method.

## Testing

Manual testing on at least one iOS and one Android device (real device or accurate emulator) for the in-scope screens before the Phase 3 milestone (`PLAN/16-IMPLEMENTATION-ROADMAP.md`, since MFA/Sessions self-service ships in that phase).

Continue to [17 — UX Acceptance Criteria](./17-UX-ACCEPTANCE-CRITERIA.md).

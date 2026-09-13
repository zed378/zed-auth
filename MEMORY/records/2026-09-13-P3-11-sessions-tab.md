# P3-11 — Console: Sessions Tab

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-11 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | console |
| **Branch** | `feat/P3-11-sessions-tab` |
| **Status** | Complete except the phone-width check, which moves to `P3-12` |

No feature spec: the card marks the `docs/UI-UX/19` chain as the specification,
and the worked example in that document *is* this component. Its twelve rows are
restated in the component's header with this implementation's answer to each, so
a reviewer checks the code against the chain in one place.

---

## Flow 4, as specified

`docs/UI-UX/04` Flow 4 is "revoke a lost-device session", and its safeguard is
that it must feel instantaneous at a moment somebody is worried about their
account. So:

- **One click, no modal.** The row disappears before the API answers.
- **Rollback.** If the API refuses, the row comes back with the reason beside it.
  A list that shows a session as gone when it is not tells a worried person they
  are safe. The component test for this fails when the rollback is removed.
- **Named targets.** "Revoke session on Safari on iOS", never a bare "Revoke" —
  a screen-reader user hears one button per session and needs to know which.
- **Revoke all other sessions** for oneself, disabled with "No other active
  sessions" when only the current one exists. Not "no sessions": the one being
  used always exists.

## The one deliberate departure

The worked example says no modal. **Revoking the current session asks first**,
because it signs the person out of the console they are using — which is not the
low-risk, reversible-by-re-login action the example describes for someone else's
device. It is the only row that asks, and on confirmation the console signs out.

## Proven end to end

The E2E test signs the same person in on two browsers, revokes one from the other
browser's Sessions tab, and then reloads the revoked browser: it is asked to sign
in. The revoking browser stays signed in. That asserts the consequence the flow
promises, not the row disappearing.

## Mobile width, stated precisely

The card asks for the revoke button to be usable at mobile width. **The console
refuses below 768px by design** (`docs/UI-UX/12`: it shows "this screen is too
narrow"), and the one screen that must work on a phone is `P3-12`'s personal
settings, which reuses this tab. The first version of the E2E test drove a
390px viewport and met that refusal — the test was wrong, not the screen.

What is verified: at 768px, the narrowest width the console serves, each revoke
button is at least 24×24 CSS pixels (WCAG 2.2 AA 2.5.8) and reachable without
horizontal scrolling. The phone-width check is on `P3-12`'s card.

## "Remember this device" — not built (DF-13)

`P3-03` passed this to `P3-11` with its own instruction: build it only if it can
be designed properly, otherwise omit it rather than ship a weak version. Built
properly it is a new credential that skips the second factor, and it needs a
signed token, a bounded lifetime, a row beside the sessions here, revocation from
that row, and a decision about whether an organization's MFA mandate allows it at
all. None of that is required by any acceptance criterion, and a half-built
version is exactly the weak version the instruction forbids. Recorded as `DF-13`
with what building it would take.

## Verified

| | |
|---|---|
| Component | 6: device-named labels and the current-session badge (axe clean), optimistic removal and rollback with the inline error, the current-session confirmation, the "no other sessions" state, revoke-all with its count, a member's view without revoke-all |
| Mutation | The rollback test fails with the rollback removed |
| E2E | Flow 4 across two browsers; the tap target at 768px; the full suite |

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.

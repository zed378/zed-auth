# 02 — User Journeys

End-to-end journeys, each tied to a persona from `01-USER-PERSONAS.md` and grounded in the backend flows in `PLAN/05-API-CONTRACT.md` and `PLAN/08-AUTHORIZATION.md`. These are the journeys the detailed flows in `04-USER-FLOWS.md` will be built from.

## Journey 1: Budi Onboards a New Employee (Org Admin)

1. Budi receives a message that a new hire, Citra, starts today.
2. Budi opens the console (already has an SSO session from earlier in the day — no login prompt).
3. Budi goes to **Users**, clicks "Invite," enters Citra's email.
4. In the **same guided flow**, Budi is prompted to pick a project and role for Citra (not a separate step he has to remember later).
5. Citra receives an invite email, sets her password, and (if the org requires it) enrolls MFA on first login.
6. Budi can immediately see Citra's status change from "invited" to "active" without refreshing manually.

**Design implication**: steps 3–4 must be one continuous flow, not two screens Budi has to navigate between (`UI-UX/04-USER-FLOWS.md` Flow 1).

## Journey 2: Sari Delegates a Project to a Partner (Project Owner)

1. Sari's team has just signed a partnership with an external vendor who needs limited access to the POS project.
2. Sari opens the project, goes to **Project Grants**, and starts "Create Grant."
3. She selects the vendor's organization and explicitly picks only the `vendor_submitter` role — the UI visibly excludes `admin`/`approver` from being selectable, rather than showing them disabled with no explanation... it shows them as available roles she is *choosing not to grant*, with a clear checkbox state.
4. A confirmation step summarizes: "Vendor ABC will be able to self-assign the `vendor_submitter` role to their own users." Sari confirms.
5. Weeks later, the partnership ends. Sari finds the grant, clicks "Revoke," and sees a consequence summary ("12 users at Vendor ABC will lose access immediately") before confirming.

**Design implication**: both creation and revocation need a consequence-preview confirmation step — this is the highest-stakes flow in the whole console (`UI-UX/04-USER-FLOWS.md` Flow 2, `UI-UX/09-INTERACTION-DESIGN.md`).

## Journey 3: Reza Manages His Own Team's Access (Vendor Admin)

1. Reza logs into the console for the first time via an invite link sent by Sari's team.
2. He lands on **Granted Projects**, sees the POS project listed with exactly one available role (`vendor_submitter`) — no confusion about roles he can't use.
3. He invites two of his own staff and assigns them the available role, without ever needing to contact Sari's team.
4. If Sari's team later revokes the grant, Reza's next visit to Granted Projects clearly shows it's gone, rather than erroring unexplained.

**Design implication**: the "Granted Projects" experience must feel complete and self-contained, not like a crippled version of the full Projects screen (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Journey 4: Ayu Loses Her Phone (End User, Self-Service)

1. Ayu realizes her phone (with her TOTP app and an active session) is lost.
2. She logs into the console from a different device (using a backup code or password reset).
3. She goes to her **personal account settings → Sessions**, sees the session tied to her lost phone, and revokes it immediately.
4. She goes to **MFA**, removes the old TOTP method, and enrolls a new one on her current device.

**Design implication**: session revocation must be effective immediately (not "eventually," see `PLAN/17-ACCEPTANCE-CRITERIA.md`), and this flow must be reachable without needing help from an org admin (`UI-UX/04-USER-FLOWS.md` Flow 4).

## Journey 5: Fajar Writes and Tests an ABAC Policy (Policy Author, Phase 4b)

1. Fajar needs to express: "approvers can only approve purchase requests from their own department, up to their personal limit."
2. He opens **Policies (ABAC)**, writes the Rego policy in a syntax-aware editor.
3. Before activating, he runs **dry-run** against a handful of real recent requests and sees which ones would now be allowed/denied differently from the current active policy.
4. Satisfied, he activates it — this creates a new policy version.
5. A week later, a support ticket reports an unexpected denial. Fajar checks the audit log's `matched_policy`/`reasons` for that specific request and immediately sees why.

**Design implication**: dry-run comparison view and the reasons-based audit trail are not "nice to have" — they are the entire safety mechanism for this feature (`UI-UX/08-PAGE-SPECIFICATIONS.md`, `PLAN/08-AUTHORIZATION.md` Part D).

## Journey 6: Nadia Evaluates the Product (Prospective Evaluator, Public Site)

1. Nadia lands on the homepage from a search result or a shared link, with maybe 10 seconds of attention before deciding whether to keep reading.
2. The hero section must answer "what is this and is it relevant to me" immediately — not require scrolling through generic value-proposition language first (`UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`).
3. She clicks through to Docs → Concepts to verify the product actually supports what she needs (e.g. multi-tenant RBAC with delegation, `PLAN/08-AUTHORIZATION.md`).
4. Satisfied the concepts fit, she opens the Quickstart guide and follows it against a real (or sandboxed) instance to validate the integration is as straightforward as claimed.
5. If it works, she either signs up / requests access, or shares the link internally with her team — either way, this is the moment the public site's job is done and the console/product experience (`UI-UX/00-DESIGN-DIRECTION.md` onward) takes over.

**Design implication**: the homepage's very first screen must communicate concrete product identity (not just brand feeling), and the path from homepage → concepts → quickstart must be reachable in very few clicks, since Nadia's attention is the scarcest resource in this entire journey (`UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`).

Continue to [03 — Information Architecture](./03-INFORMATION-ARCHITECTURE.md).

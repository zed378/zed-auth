# Public Website

The marketing site, the integrator documentation, and the generated API reference at
`public-site/` — a separate application from the console, deployed separately, sharing no
code with it.

`docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` is the technical intent,
`docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` the design, and
`docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` the copy rules. These documents describe what
was built and how its claims are kept honest.

The governing rule for this surface is one sentence, and it is unusual enough to state up
front:

> **Copy never describes a capability that is not shipped in the current roadmap phase.**

Two build-time checks enforce the mechanical half of that. A person keeps the rest true, in
`public-site/CLAIMS.md`.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-SITE-PURPOSE-AND-AUDIENCE.md`](./00-SITE-PURPOSE-AND-AUDIENCE.md) | Who the site is for, what it must not become | Implemented |
| [`01-INFORMATION-ARCHITECTURE.md`](./01-INFORMATION-ARCHITECTURE.md) | Every page, the navigation, one project rather than two | Implemented |
| [`02-CONTENT-GOVERNANCE.md`](./02-CONTENT-GOVERNANCE.md) | The claims rule, the capability audit, the two gates | Implemented |
| [`03-GENERATED-API-REFERENCE.md`](./03-GENERATED-API-REFERENCE.md) | Why it is generated, how, and what that forbids | Implemented |
| [`04-DOCS-CONTENT-PLAN.md`](./04-DOCS-CONTENT-PLAN.md) | The guides and concepts that exist, and what is owed | Partially implemented |
| [`05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md`](./05-SEO-PERFORMANCE-AND-ACCESSIBILITY.md) | Canonical URLs, robots, contrast, what is measured | Partially implemented |
| [`06-BUILD-AND-DEPLOYMENT.md`](./06-BUILD-AND-DEPLOYMENT.md) | The build, its checks, and how it reaches staging | Implemented |
| [`07-LAUNCH-CHECKLIST.md`](./07-LAUNCH-CHECKLIST.md) | What has to be true before a public launch | Partially implemented |

## Related

- [`../FRONTEND/`](../FRONTEND/) — the console, which shares nothing with this.
- [`../API/`](../API/) — the contract the reference is generated from.
- [`../UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`](../UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md)
- [`../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`](../UI-UX/21-CONTENT-AND-COPY-STRATEGY.md)

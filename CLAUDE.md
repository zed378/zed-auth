# CLAUDE.md

Guidance for Claude Code when working in this repository. This project builds a centralized **Auth Service** (SSO + REST API + multi-tenant RBAC/ABAC) plus its management console and public site, following the plan in `docs/PLAN/`, `docs/UI-UX/`, and `docs/SECURITY/`.

**Read this file in full before writing any code.** It is shorter than the plan itself and tells you which plan document to open for any given task — don't skip straight to coding from assumptions.

## What This Repository Is

An identity and access management platform (Zitadel-style): centralized authentication (OIDC/OAuth 2.1, SAML later), a full REST Management API, multi-tenant RBAC with cross-organization delegation (Project Grants), optional ABAC, a management console, and a public marketing/docs site. Nothing has been implemented yet unless a `backend/`, `console/`, or `public-site/` directory already exists with code in it — check before assuming.

## Documentation Map — Read Before You Build

Every technical decision in this project has already been made and written down. **Do not re-derive architecture, data models, or API shapes from scratch — find the answer in the plan first.**

| If you're working on... | Read first |
|---|---|
| Anything at all, first time in this repo | `docs/PLAN/00-PROJECT-CONTEXT.md`, `docs/PLAN/01-PRODUCT-SCOPE.md` |
| Overall system design | `docs/PLAN/03-ARCHITECTURE.md` |
| Database schema / migrations | `docs/PLAN/04-DATA-MODEL.md` |
| Any API endpoint (new or modified) | `docs/PLAN/05-API-CONTRACT.md` |
| The management console (frontend) | `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` + the entire `docs/UI-UX/` folder, starting at `docs/UI-UX/00-DESIGN-DIRECTION.md` |
| The public site (landing/docs/about) | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` + `docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` + `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| Backend service internals | `docs/PLAN/07-BACKEND-ARCHITECTURE.md` |
| **How** to write the code — conventions, layer templates, error/logging rules, the gate, the review checklist | `docs/ENGINEERING/`, starting at `docs/ENGINEERING/00-ENGINEERING-CONTEXT.md` |
| **How** the console is actually engineered — structure, tokens, components, data layer, routing, forms, testing | `docs/FRONTEND/`, starting at `docs/FRONTEND/00-FRONTEND-CONTEXT.md` |
| The public site as built — content governance, the generated API reference, launch | `docs/WEBSITE/` |
| Roles, permissions, RBAC, Project Grants, ABAC | `docs/PLAN/08-AUTHORIZATION.md` (this is the single source of truth for all authorization logic — RBAC, delegation, and ABAC are documented together because they're layered, not separate systems) |
| Anything security-related | `docs/PLAN/09-SECURITY.md` for baseline controls, then the full `docs/SECURITY/` folder starting at `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md` for the detailed threat model |
| Writing or reviewing tests | `docs/PLAN/11-TESTING.md` |
| Anything touching latency/scale | `docs/PLAN/12-PERFORMANCE.md` |
| Deployment, environments, CI/CD | `docs/PLAN/14-DEPLOYMENT.md` |
| What phase we're in / what's in scope right now | `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` |
| Whether a feature is "done" | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` and, for console UI work, `docs/UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` |

If a question isn't answered in these documents, that's a real gap — flag it to the user rather than guessing and silently deciding.

## Mandatory Workflow for Any New Feature

Before writing implementation code for a non-trivial feature, work through **`docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`** — business objective, actors, functional/non-functional requirements, dependencies, DB changes, API contract, frontend/backend changes, authorization rules, validation, error handling, edge cases, **abuse cases** (cross-reference `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`), logging/audit requirements, security controls, testing strategy, acceptance criteria, implementation sequence, rollback strategy, and technical risks.

For small, clearly-scoped changes (a bug fix, a copy tweak, a config change), this full template is overkill — use judgment, but err toward writing it out for anything that touches authentication, authorization, or data model changes, since those are exactly the categories where skipped analysis becomes a security incident later.

## Non-Negotiable Constraints

These come directly from the plan and must not be silently violated for convenience:

- **API-first**: every capability exposed in the console must also be exposed via the REST API (`docs/PLAN/02-REQUIREMENTS.md` FR-14). Never build a console-only shortcut that bypasses the documented API contract.
- **Authorization checks are server-side, always.** The console may hide UI elements based on role claims for UX purposes, but the API must independently enforce every permission check — a hidden button is not a security control (`docs/PLAN/08-AUTHORIZATION.md`, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2/§3).
- **Project Grant role assignment must be validated server-side as a subset of `granted_role_keys`** on every single request, not just at grant-creation time (`docs/PLAN/08-AUTHORIZATION.md` Part C, `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`'s worked example).
- **Never build a Phase N+1 feature while Phase N is incomplete**, per `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`'s incremental approach — check the roadmap before starting work on multi-org delegation, ABAC, or SAML if the earlier phases aren't done.
- **Never hand-write API reference documentation.** The public site's `/docs/api-reference` is generated from the OpenAPI spec (`docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) — if you change an endpoint, update the OpenAPI spec, and the docs follow automatically.
- **Never log tokens, passwords, or raw resource attributes** sent to `/v1/authz/check` (`docs/PLAN/13-OBSERVABILITY.md`, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` Information Disclosure).
- **Never mark a security-sensitive feature (auth, authz, sessions, grants) as done without a corresponding abuse-case test** from `docs/PLAN/11-TESTING.md` and, where relevant, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`.
- **Marketing/docs copy never describes a capability that isn't actually shipped** in the current roadmap phase (`docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`'s governance rule).
- **`color-danger` in the console design system is reserved exclusively for destructive/irreversible actions** — don't reuse it for anything else, since its meaning must stay reliable (`docs/UI-UX/06-VISUAL-LANGUAGE.md`).

## Assumed Tech Stack (Per the Plan — Confirm Against `docs/PLAN/07` and `docs/PLAN/06` Before Deviating)

| Layer | Choice |
|---|---|
| Backend language | Go |
| Backend HTTP | `chi` or `net/http` + middleware |
| OAuth2/OIDC | `ory/fosite` or `zitadel/oidc` |
| Policy engine | OPA (embedded, Go library) for ABAC |
| Database | PostgreSQL |
| Cache/session | Redis |
| Console frontend | React + TypeScript, Tailwind-based design tokens, TanStack Query |
| Public site | Static-generated marketing pages + Docusaurus-style docs, deployed separately from the console |
| Containerization | Docker, Kubernetes for production |

## Working Conventions

- **Directory layout** (propose this if the repo is empty; keep consistent if it already exists):
  ```
  backend/         → Go service (auth, authz, management API, OIDC provider)
  console/         → React management console
  public-site/     → Landing, docs, about, changelog
  docs/            → Reference documentation. Amended deliberately, never as a
                     side effect of feature work — .github/CODEOWNERS enforces it
    PLAN/          → Engineering plan
    UI-UX/         → Design plan
    SECURITY/      → Threat model & security plan
  ```
- **Migrations**: additive/backward-compatible by default (`docs/PLAN/14-DEPLOYMENT.md` expand/contract pattern) — never a migration that breaks a running previous-version instance mid-rollout.
- **Commits/PRs**: keep scoped to one feature-spec unit where possible; reference the relevant `docs/PLAN/`/`docs/UI-UX/`/`docs/SECURITY/` document(s) in the PR description so reviewers can check the implementation against the actual spec, not just against the diff.
- **When the plan and a practical constraint conflict** (e.g. a chosen library doesn't support something `docs/PLAN/07-BACKEND-ARCHITECTURE.md` assumed), stop and surface the conflict rather than quietly picking a different approach — the plan documents are supposed to stay the source of truth, so a deviation should be a visible decision, not a silent one.

## Testing Expectations

Follow the pyramid in `docs/PLAN/11-TESTING.md`: unit tests for pure logic (hashing, JWT, RBAC/ABAC decisions), integration tests against real Postgres/Redis (testcontainers), E2E tests for full login/console flows (Playwright), and dedicated security tests for every abuse scenario listed against the feature (`docs/PLAN/10-THREAT-MODEL.md`, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`). Run the full test suite before considering any feature-spec item complete.

## When You're Unsure

Prefer asking the user or leaving a clearly marked `TODO`/open question over inventing an architectural decision that contradicts or extends the plan. The plan is large and detailed specifically so implementation doesn't require guessing — if you find yourself guessing, that's a sign to re-check `docs/PLAN/`, `docs/UI-UX/`, or `docs/SECURITY/` more carefully first.

<!-- RTK_TOKEN_SAVER_START -->
# RTK Token Saver Rule (claude)

## RTK (Rust Token Killer) Optimization Rule
Prefix every shell/terminal execution command with `rtk`:
- `rtk git status`, `rtk git diff`, `rtk git log -n 5`
- `rtk npm test`, `rtk npm run build`, `rtk pnpm test`, `rtk yarn test`
- `rtk cargo check`, `rtk cargo test`, `rtk cargo build`
- `rtk pytest`, `rtk vitest`
- `rtk ls src/`, `rtk tree -L 2`, `rtk rg "pattern"`

Keep the prefix inside chains: `rtk git add . && rtk git commit -m "msg"`.
Commands RTK has no filter for run as-is, so the prefix is always safe.

### Output & Token Optimization
Command output is condensed to save 60-90% LLM context window tokens while preserving every critical signal, error, and exit code.
- `rtk gain` / `rtk gain --history` - View token savings scoreboard.
- `rtk proxy <cmd>` - Run a command unfiltered if raw output is strictly needed.
- `RTK_DISABLED=1 <cmd>` - Skip RTK for one command.

## 3-Tier Hybrid Web Research & Scraping Policy (Upstream: fastcrw/crw & lightpanda-io/browser)
When your IDE agent receives a task requiring web research or extraction, it follows this strict 3-tier routing logic:

1. **Discovery (Tier 1 - Default Search)**:
   - The agent uses its built-in search tool (`search_web` / native search) to find target URLs or documentation links.
2. **Fast Extraction (Tier 2 - fastCRW)**:
   - The agent attempts to fetch the URL using fastCRW (`crw scrape <url>`, `crw_scrape`, or `http://127.0.0.1:3000`).
   - If the page is server-rendered (like a blog, GitHub README, or docs site), fastCRW instantly grabs clean Markdown/text, saving 70-90% context tokens without JS engine overhead.
3. **Deep Dynamic Rendering (Tier 3 - Lightpanda Local via MCP)**:
   - If fastCRW returns empty content, a rendering error, or hits a client-side JavaScript wall (e.g., a React/Vue SPA app or login-gated dashboard), the agent automatically falls back to Lightpanda Local via MCP (`lightpanda mcp`).
   - Lightpanda executes the JS via V8 and feeds a clean Accessibility Tree or Markdown back into the IDE context window using ~10x less memory than headless Chrome.


## Mandatory Semantic Knowledge Graph First Policy (Token Saver Enforcement)
Autonomous AI agents in ANY IDE MUST ALWAYS query the pre-indexed semantic knowledge graphs to provide answers BEFORE falling back to raw file reads that waste prompt tokens:

### 1. Code-Based Files -> CodeGraphContext (Upstream: CodeGraphContext/CodeGraphContext)
- **Code Query Engine:** For all code architecture, symbol definitions, function signatures, call hierarchies (callers/callees), class inheritance, and module dependencies, AI agents MUST query the CodeGraphContext index (/codegraph-query, `cgc query`, `cgc find`, or CodeGraphContext MCP tools) FIRST.
- **Zero-Waste Fallback:** NEVER dump or read entire source code files or directory trees into LLM prompt context when answering code questions. Only fall back to localized file reads (and then only targeted slices) if the knowledge graph query does not provide sufficient detail.
- **Live Synchronization:** The project code graph is continuously kept updated via background incremental sync / `cgc watch` / on-save triggers.

### 2. Non-Code Files -> JevGraph (Upstream: chenmingtang830/jevgraph)
- **Document Query Engine ONLY:** JevGraph is strictly designated for non-code files (documentation, Markdown specs, PRDs, PDFs, Word docs `.docx`, slides `.pptx`, text `.txt`).
- **Bounded Evidence:** When ingesting large specs, PRDs, PDFs, or architectural documentation, build or query bounded evidence-backed knowledge graphs (/jevgraph-build, /jevgraph-query) with character-level page maps instead of loading raw long documents into prompt context.
- **Strict Separation:** NEVER use JevGraph for source code AST indexing; code files are strictly handled by CodeGraphContext.


## Headroom Context Compression (Upstream: headroomlabs-ai/headroom)
- Utilize Headroom context compression and Compress-Cache-Retrieve (CCR) for heavy JSON structures, file reads, and tool payloads to minimize prompt tokens.


## Anti-Slop Filter & Quality Rules (Upstream: miqdadbadjuber/anti-slop)
Follow Anti-Slop rules to prevent generic AI slop in generated UI, copy, and code comments (Mode: DURING):
- **Filter, Not Style Guide:** Run purpose tests before aesthetic choices (reject ungrounded neon glows, fake metrics, generic sparkle badges).
- **Liveliness & Grounding:** Rely on `DESIGN.md` or explicit brief for design soul; enforce WCAG AA human accessibility.
- **Dedicated Skills:** Read `skills/antislop/SKILL.md` (core), `skills/antislop-ui/SKILL.md`, `skills/antislop-copywriting/SKILL.md`, `skills/antislop-human/SKILL.md`, `skills/antislop-layoutmobile/SKILL.md`, `skills/antislop-code/SKILL.md` when performing relevant tasks.


## Output & Generation Token Saver Rule (Ponytail Mode: FULL)
- **YAGNI & Shortest Diff:** Only write code that must exist. Reach for standard library before custom code or new dependencies. Shortest working diff wins.
- **Terse Responses:** Code first. At most 3 short lines of explanation: what was skipped, when to add it. No essays, no unsolicited design tours, no feature walkthroughs.

## Terse Agent Directives
- Eliminate pleasantries, greetings, and conversational filler. Provide direct answers and actionable code.
- Avoid reprinting unchanged code blocks. Use targeted search/replace blocks or concise snippets.

## Context Optimization (Compact Diffs & Outlines)
- Inspect git changes using compact single-line diffs: `rtk git diff -U1` instead of wide multi-line context.
- When exploring codebases, inspect function signatures / AST outlines (/rtk-outline) before reading entire files into context.

## ECC Context Management & Compaction Protocol (Upstream: affaan-m/ECC)
Autonomous AI agents MUST actively manage conversation context window load to prevent token bloat and degraded reasoning:
1. **Strategic Compaction at Milestones:** At major phase completions or after 50+ tool calls, invoke strategic compaction (/strategic-compact) to summarize completed steps, pruned raw outputs, and focus active memory strictly on pending requirements.
2. **Context Keeper (`ck`) Snapshots:** Before major context purges, architectural refactors, or switching tasks, persist durable checkpoint files (`.agents/ck/context.json` and `.agents/ck/CONTEXT.md` or invoke /ck:save) so state is never lost across conversation boundaries.
3. **Anti-Loop Council & Agent Evaluation:** If stuck in repeated failed edits, build verification errors, or circular debug loops (>3 failed attempts), invoke /council or /agent-evaluator to obtain root-cause guidance and stop wasting tokens on trial-and-error repetition.

## Upstream Engine Integration & Latency Guardrails Standards
Whenever a new upstream engine or external tool (e.g. `fastcrw/crw`, `CodeGraphContext`, `JevGraph`) is introduced or updated:
1. **Holistic Dashboard Integration (8 Surfaces Required)**:
   - Header action toggle button (`Turn <Tool> ON / OFF`)
   - Setup alert banner (missing chips + AI install prompt generator)
   - Dedicated dashboard section & Quick Action triggers
   - Multi-channel savings breakdown navigation tabs & distribution weights
   - System Health diagnostics row with status, AI install prompt, sync, and uninstall actions
   - Upstream versions dropdown listing
   - Upstream Sync Picker (`showSyncPicker`) and Uninstall Picker (`showUninstallPicker`)
   - Footer upstream reference links
2. **Zero-Noise Latency Benchmarks**: "Test Upstream Latencies" must selectively ping and display only **installed** upstream components, silently omitting/ignoring uninstalled tools so users never see failed pings or clutter for optional tools.
3. **Footer Version Number Invariant**: The current plugin release version must always be presented beside "Token Saver" in front of "• GitHub".

<!-- RTK_TOKEN_SAVER_END -->

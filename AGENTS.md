# AGENTS.md

Instructions for any AI coding agent (Claude Code, Codex, or otherwise) working in this repository. This file follows the [agents.md](https://agents.md) convention and should stay consistent with `CLAUDE.md` — if you update one, update the other.

## Project Summary

This repository implements a centralized **Auth Service**: SSO (OIDC/OAuth 2.1, SAML later), a full REST Management API, multi-tenant RBAC with cross-organization delegation, optional ABAC, a management console, and a public marketing/docs site. The complete engineering, design, and security plan already exists under **`docs/`** — **`docs/PLAN/`**, **`docs/UI-UX/`**, **`docs/SECURITY/`**, with [`docs/README.md`](docs/README.md) as the way in — and is the authoritative source for architecture, data model, API shape, authorization rules, and UI behavior. Do not re-derive these from scratch; look them up.

## Setup Commands

> Fill in once the actual toolchain is initialized (`docs/PLAN/07-BACKEND-ARCHITECTURE.md`, `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`). Placeholder conventions below — update this section as soon as the repo has real build tooling, and keep it accurate; a stale setup section is worse than none.

```bash
# Backend (Go)
cd backend && go mod download

# Console (React + TypeScript)
cd console && npm install

# Public site
cd public-site && npm install
```

## Build & Test Commands

```bash
# Backend
cd backend && go build ./...
cd backend && go test ./...
cd backend && gosec ./...          # SAST, per docs/PLAN/09-SECURITY.md / docs/PLAN/11-TESTING.md

# Console
cd console && npm run build
cd console && npm run test         # component tests
cd console && npm run test:e2e     # Playwright, per docs/PLAN/11-TESTING.md / docs/PLAN/06-FRONTEND-ARCHITECTURE.md

# Public site
cd public-site && npm run build
```

Run the relevant test command(s) before considering any change complete. A change touching authentication, authorization, or the data model requires the full backend test suite, not just the package you edited — these areas have cross-cutting invariants (`docs/PLAN/08-AUTHORIZATION.md`) that a narrow test run can miss.

## Where to Find the Spec for Any Task

| Task | Primary document(s) |
|---|---|
| First time in the repo | `docs/PLAN/00-PROJECT-CONTEXT.md`, `docs/PLAN/01-PRODUCT-SCOPE.md` |
| System architecture | `docs/PLAN/03-ARCHITECTURE.md` |
| Database/migrations | `docs/PLAN/04-DATA-MODEL.md` |
| API endpoints | `docs/PLAN/05-API-CONTRACT.md` |
| Console (frontend) | `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`, `docs/UI-UX/00` through `docs/UI-UX/19` |
| Public site (landing/docs/about) | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| Backend internals | `docs/PLAN/07-BACKEND-ARCHITECTURE.md` |
| Roles/RBAC/Project Grants/ABAC | `docs/PLAN/08-AUTHORIZATION.md` |
| Security controls & threat model | `docs/PLAN/09-SECURITY.md`, then `docs/SECURITY/00` through `docs/SECURITY/05` |
| Testing | `docs/PLAN/11-TESTING.md` |
| Performance targets | `docs/PLAN/12-PERFORMANCE.md` |
| Deployment/CI/CD | `docs/PLAN/14-DEPLOYMENT.md` |
| Current phase / what's actually in scope | `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` |
| Definition of done | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md`, `docs/UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` |

## Feature Workflow

For any non-trivial feature, produce (or follow, if one already exists) a spec using **`docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`** — covering business objective, actors, requirements, dependencies, DB/API/frontend/backend changes, authorization rules, validation, error handling, edge cases, **abuse cases** (cross-check `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`), audit logging, security controls, tests, acceptance criteria, implementation sequence, rollback strategy, and risks. Skip the full template only for trivial, clearly-scoped changes (typo fixes, config tweaks) — anything touching auth, authorization, or the data model should go through it.

## Code Style

- **Go**: idiomatic Go, standard project layout, no framework "magic" in security-critical paths (`docs/PLAN/07-BACKEND-ARCHITECTURE.md`'s stated preference for `chi`/`net/http` over heavier frameworks). Parameterized queries only — never string-concatenated SQL (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §8).
- **TypeScript/React**: functional components, TanStack Query for server state rather than a global store for cached API data (`docs/PLAN/06-FRONTEND-ARCHITECTURE.md`), Tailwind utility classes mapped to the design tokens in `docs/UI-UX/05-DESIGN-SYSTEM.md` — no ad hoc colors/spacing outside the token set.
- **Commits**: scoped to one logical change; reference the relevant plan document(s) in the message/PR body.

## Hard Rules (Do Not Violate)

1. Every capability in the console must also exist in the REST API (`docs/PLAN/02-REQUIREMENTS.md` FR-14) — no console-only shortcuts.
2. Authorization is enforced server-side on every request, independent of any UI-level hiding (`docs/PLAN/08-AUTHORIZATION.md`, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2/§3).
3. Project Grant role assignments are validated server-side as a subset of `granted_role_keys` on every request, not just at creation (`docs/PLAN/08-AUTHORIZATION.md` Part C).
4. Do not implement a later roadmap phase's feature while an earlier phase is incomplete (`docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`).
5. The public API reference is generated from the OpenAPI spec, never hand-maintained separately (`docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`).
6. Never log tokens, passwords, or raw `/v1/authz/check` resource attributes (`docs/PLAN/13-OBSERVABILITY.md`).
7. Every security-sensitive feature ships with a corresponding abuse-case test (`docs/PLAN/11-TESTING.md`, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`).
8. Marketing/docs copy never claims a capability beyond what's actually shipped in the current phase (`docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`).
9. `docs/PLAN/`, `docs/UI-UX/`, and `docs/SECURITY/` are reference documentation, not implementation output — don't edit them as a side effect of feature work; if the plan itself needs to change, that's a deliberate, separate action the user should be aware of.

## PR / Change Instructions

- Link the PR description to the specific `docs/PLAN/`/`docs/UI-UX/`/`docs/SECURITY/` sections the change implements, so reviewers can check against spec rather than only against the diff.
- Include which tests were added/run, referencing `docs/PLAN/11-TESTING.md`'s pyramid layer(s).
- If the change deviates from the plan (a library limitation, a discovered ambiguity), state the deviation explicitly in the PR rather than silently diverging — the plan should stay accurate, or the deviation should be flagged for the plan to be updated.

## When Uncertain

Ask rather than assume. The plan is detailed specifically so implementation doesn't require guessing; if an answer isn't in `docs/PLAN/`, `docs/UI-UX/`, or `docs/SECURITY/`, that's a genuine gap to raise, not a decision to make silently.

<!-- RTK_TOKEN_SAVER_START -->
# RTK Token Saver Rule (agents)

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

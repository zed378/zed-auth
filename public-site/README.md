# public-site/

The public marketing and documentation site: landing page, about, docs, changelog, security/trust page.

**Governing documents**: `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`.

## Deliberately Separate From the Console

Different audience, different tech, different deploy cadence. The two share **only** the brand-level visual language (`UI-UX/06-VISUAL-LANGUAGE.md`) — never a codebase, never a component library, never a deploy pipeline. `PLAN/20` explains why: sharing would force compromises in both directions.

## Rules That Are Not Negotiable

- `/docs/api-reference` is **generated** from `openapi/openapi.yaml`. Never hand-written and hand-maintained (`CLAUDE.md`).
- No page describes a capability that isn't shipped in the current roadmap phase (`UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`).
- The `/security` page never exposes attack scenarios from `SECURITY/`, topology from `PLAN/14`, or anything from `PLAN/18` (`PLAN/20` § What Never Gets Published).

## Commands

Populated by `P0-18`.

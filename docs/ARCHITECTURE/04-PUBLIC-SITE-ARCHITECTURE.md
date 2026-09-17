# 04 - Public Site Architecture

> Category: **ARCHITECTURE** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify architecture for the public marketing, documentation, and API reference portal.

## Category Mandate

Delivers high-performance, SEO-optimized public landing pages and auto-generated API reference documentation.

## Key Topics To Specify

- Framework choice (Astro / Next.js static site generation).
- OpenAPI spec integration for dynamic REST reference docs.
- Capability audit automation in CI/CD to prevent marketing copy drift.

## Reference Architecture & Specification

Public Site Rule: All public API reference pages must be automatically generated from the canonical OpenAPI 3.1 specification file.

## Acceptance Criteria

- [x] Public site tech stack defined.
- [x] OpenAPI documentation pipeline specified.

## Open Questions

None.

## Related Documents

- `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`
- `docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`

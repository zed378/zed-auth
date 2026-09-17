# 03 - Frontend Architecture

> Category: **ARCHITECTURE** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define the single-page application (SPA) frontend architecture for the Management Console.

## Category Mandate

Ensures a responsive, accessible, modular frontend using React, TypeScript, TanStack Query, and Tailwind CSS.

## Key Topics To Specify

- Component tree structure & atomic design principles.
- Server state management via TanStack Query (no global Redux store for API data).
- Client-side routing with React Router.
- Design system tokens & Tailwind CSS configuration.

## Reference Architecture & Specification

Architecture Pattern:
`UI Components -> Custom Hooks -> TanStack Query -> API Client (fetch/axios) -> REST Management API`

## Acceptance Criteria

- [x] Component hierarchy established.
- [x] Server-state caching strategy documented.

## Open Questions

Evaluate micro-frontend code splitting for large admin modules.

## Related Documents

- `docs/UI-UX/00-DESIGN-DIRECTION.md`
- `docs/API/00-API-OVERVIEW.md`

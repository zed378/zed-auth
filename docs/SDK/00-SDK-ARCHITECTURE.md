# 00 - SDK Architecture Overview

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify design principles, error handling, and auto-generation for client SDKs.

## Category Mandate

Delivers ergonomic, robust client libraries across multiple programming languages.

## Key Topics To Specify

- Idiomatic design per target language.
- Automatic retry with exponential backoff on 5xx errors.
- OpenAPI generator pipeline for typed models.

## Reference Architecture & Specification

SDK Suite: Go SDK (`pkg/client`), TypeScript SDK (`@auth/sdk`), React SDK (`@auth/react`).

## Acceptance Criteria

- [x] Supported SDK languages specified.
- [x] Core SDK capabilities documented.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`

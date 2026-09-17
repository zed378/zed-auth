# 03 - Custom Domains & Tenant Routing

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify custom domain mapping (e.g. `auth.acme.com`), DNS TXT verification, TLS certificate issuance, and request routing.

## Category Mandate

Allows tenant organizations to use branded custom domain endpoints.

## Key Topics To Specify

- DNS verification via TXT record (`_auth-challenge.acme.com`).
- Automated TLS certificate provisioning via Let's Encrypt / ACME HTTP-01 challenge.
- HTTP Host header matching to lookup Organization context.

## Reference Architecture & Specification

Routing Pipeline:
`Request -> Host Header (auth.acme.com) -> Lookup Org ID -> Set app.current_org_id -> Serve Tenant Page`

## Acceptance Criteria

- [x] Custom domain verification workflow specified.
- [x] TLS cert automation documented.

## Open Questions

None.

## Related Documents

- `docs/API/10-ORGANIZATION-API.md`

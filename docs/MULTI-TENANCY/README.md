# Category: MULTI-TENANCY

Organization & Project hierarchy, row-level security isolation, custom domain routing, and tenant configuration.

## Category Mandate

The `MULTI-TENANCY/` directory specifies **tenant isolation architecture**. It guarantees strict data partitioning across organizations and projects via database-enforced Row-Level Security (RLS) and secure domain routing.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-MULTI-TENANCY-ARCHITECTURE.md` | Multi-Tenancy Architecture | Hierarchy model (Instance -> Org -> Project -> App). |
| `01-ORGANIZATION-AND-PROJECT-HIERARCHY.md` | Hierarchy Specification | Entity relationships, organization settings, tenant scoping. |
| `02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md` | PostgreSQL RLS Isolation | Database session variables (`app.current_org_id`), RLS policies. |
| `03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md` | Custom Domain Routing | Domain verification (CNAME/TXT), TLS cert management, routing. |

# 00 - DevOps Architecture Overview

> Category: **DEVOPS** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify deployment topology, container orchestration, and infrastructure automation.

## Category Mandate

Establishes cloud-native, reproducible deployment practices adhering to 12-Factor App principles.

## Key Topics To Specify

- Kubernetes (EKS/GKE) container orchestration.
- Infrastructure as Code (Terraform / Helm).
- Zero-downtime rolling updates (`maxSurge: 25%`, `maxUnavailable: 0`).

## Reference Architecture & Specification

Deployment Model:
`Code Commit -> GitHub Actions CI -> Build Docker Image -> Scan Vulns -> Helm Upgrade -> K8s Rolling Update`

## Acceptance Criteria

- [x] Infrastructure stack specified.
- [x] Deployment strategy defined.

## Open Questions

None.

## Related Documents

- `docs/DEVOPS/02-CONTAINERIZATION-AND-KUBERNETES.md`

# DevOps

How this system is configured, built, checked, deployed and recovered. `docs/PLAN/14-DEPLOYMENT.md` and `15-DISASTER-RECOVERY.md` hold the intent; these documents describe what exists today — Docker Compose everywhere, a private staging VM, manual pull-based deployment, and CI that gates every merge.

Architecture is [`../ARCHITECTURE/`](../ARCHITECTURE/); what the gates actually run is [`../TESTING/`](../TESTING/).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-DEVOPS-OVERVIEW.md`](./00-DEVOPS-OVERVIEW.md) | The operational picture and the release order | Implemented |
| [`01-ENVIRONMENTS-AND-CONFIGURATION.md`](./01-ENVIRONMENTS-AND-CONFIGURATION.md) | Environments, and every configuration key's default | Implemented |
| [`02-CONTAINERIZATION-AND-KUBERNETES.md`](./02-CONTAINERIZATION-AND-KUBERNETES.md) | Images and compose; Kubernetes as the unbuilt target | Partially implemented |
| [`03-CICD-PIPELINE-SPECIFICATION.md`](./03-CICD-PIPELINE-SPECIFICATION.md) | CI jobs, the local gate, and why delivery is manual | Partially implemented |
| [`04-SECRET-MANAGEMENT.md`](./04-SECRET-MANAGEMENT.md) | What is secret, where it lives, how it rotates | Implemented |
| [`05-BACKUP-AND-DISASTER-RECOVERY.md`](./05-BACKUP-AND-DISASTER-RECOVERY.md) | Verified backups, retention, and what recovery still lacks | Partially implemented |

## Related

- `deploy/` — compose files, VM units, runbooks, `SECRETS.md`.
- `scripts/check.sh`, `scripts/e2e-up.sh`, `scripts/acceptance-phase*.sh`.
- `.github/workflows/ci.yml`.

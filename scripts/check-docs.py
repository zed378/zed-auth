#!/usr/bin/env python3
"""Flag claims in `docs/` that this repository does not support.

The domain categories under `docs/` describe the system **as built**, and each
document carries a status line and cites the code behind its claims. That is
only worth something if the citations are real: a scaffold left behind, an
endpoint that does not exist, or a path that was renamed turns reference
documentation into confident fiction.

`docs/PLAN/`, `docs/UI-UX/` and `docs/SECURITY/00`–`05` are design intent,
written before the code, and are deliberately not checked here.

Checks:
  1. scaffold markers left in a document that is not a draft
  2. a missing or unrecognised Status in the header
  3. `/v1` or `/oauth` paths that are in neither the contract nor the router
  4. prefixed id examples (`usr_…`, `org_…`) — this service uses UUIDs (PG-23)
  5. repository paths that do not exist

Usage: python scripts/check-docs.py    (exit 1 if anything is flagged)
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
SKIP_DIRS = {"PLAN", "UI-UX"}
SKIP_FILES = {
    "SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md",
    "SECURITY/01-THREAT-ACTOR-PROFILES.md",
    "SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md",
    "SECURITY/03-DETECTION-AND-MONITORING.md",
    "SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md",
    "SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md",
    "README.md",
}
STATUSES = ("Implemented", "Partially implemented", "Draft specification")
SCAFFOLD_MARKERS = ("Final specification", "Owner: TBD", "## Category Mandate", "Key Topics To Specify")
FIELD_SUFFIXES = {"id", "ids", "key", "keys", "type", "name", "names", "grants", "acme"}


def known_paths() -> set[str]:
    spec = (ROOT / "openapi/openapi.yaml").read_text(encoding="utf-8")
    server = (ROOT / "backend/internal/httpserver/server.go").read_text(encoding="utf-8")
    return (
        set(re.findall(r"^  (/[^\s:]+):", spec, re.M))
        | set(re.findall(r'"(/[a-z0-9\-/._{}]+)"', server))
        | {"/login", "/login/mfa", "/logout", "/account/passkeys",
           "/auth/callback", "/auth/silent", "/healthz", "/readyz", "/v1"}
    )


def main() -> int:
    known = known_paths()
    problems: list[tuple[str, str]] = []
    files = sorted(DOCS.rglob("*.md"))

    for path in files:
        rel = path.relative_to(DOCS).as_posix()
        if rel.split("/")[0] in SKIP_DIRS or rel in SKIP_FILES:
            continue
        text = path.read_text(encoding="utf-8")
        head = "\n".join(text.splitlines()[:6])

        for marker in SCAFFOLD_MARKERS:
            if marker in text and "Draft specification" not in head:
                problems.append((rel, f"scaffold marker left in place: {marker!r}"))

        if not rel.endswith("README.md"):
            if "Status:" not in head:
                problems.append((rel, "no Status in the header"))
            elif not any(f"Status: {s}" in head for s in STATUSES):
                problems.append((rel, "unrecognised Status value"))

        for bad in re.findall(r"\b(?:org|proj|usr|app)_[0-9a-z]{3,}\b", text):
            if bad.split("_", 1)[1] in FIELD_SUFFIXES:
                continue  # a field name, not an identifier example
            line = next((l for l in text.splitlines() if bad in l), "")
            if "PG-23" in line or "is wrong" in line or "reserved" in line:
                continue  # quoted precisely to say it is wrong
            problems.append((rel, f"prefixed id example {bad!r} — this service uses UUIDs (PG-23)"))

        for endpoint in sorted(set(re.findall(r"`((?:/v1|/oauth)/[a-zA-Z0-9\-/_{}.]*)`", text))):
            probe = endpoint.rstrip("/")
            if probe in known or probe.endswith(("*", "…")):
                continue
            if any(probe.startswith(k) and k != "/v1" for k in known):
                continue
            problems.append((rel, f"endpoint is in neither the contract nor the router: {endpoint}"))

        pattern = r"`((?:backend|console|public-site|deploy|scripts|openapi|TASKS|MEMORY|docs)/[A-Za-z0-9\-/_.]+)`"
        for ref in sorted(set(re.findall(pattern, text))):
            if (ROOT / ref).exists() or "*" in ref:
                continue
            if re.fullmatch(r"docs/[A-Z-]+/\d\d", ref):
                continue  # the repository cites a document by number routinely
            problems.append((rel, f"path does not exist: {ref}"))

    for rel, problem in problems:
        print(f"{rel}: {problem}")
    print(f"\n{len(problems)} problem(s) across {len(files)} documents")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())

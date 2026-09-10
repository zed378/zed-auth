#!/usr/bin/env python3
"""Fail if the API contract documents an endpoint that has not shipped.

`/docs/api-reference` on the public site renders directly from
`openapi/openapi.yaml`, so an endpoint documented there is a public claim that
it exists. `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`'s governance rule and
`CLAUDE.md` both forbid that: marketing and documentation never describe a
capability beyond the current roadmap phase.

`docs/PLAN/05-API-CONTRACT.md` Part B lists the whole v1 surface, and writing it all
out now as a design exercise is tempting and would put that claim on the public
site for every unbuilt endpoint at once.

When an endpoint ships, add it to SHIPPED in the same commit that adds it to
the spec. The duplication is the point: it makes publishing an endpoint a
deliberate two-line act rather than a side effect of editing YAML.
"""

import re
import sys
from pathlib import Path

# Endpoints that exist in the running service today.
#
# Phase 0 ships the operational probes. The identity and authorization
# endpoints arrive in Phase 1 (P1-05 onward).
SHIPPED = {
    # P1-06. The authorization endpoint is served and real; discovery
    # advertises it from the same commit, which is the only way the document
    # stays true.
    "/oauth/authorize",
    # P1-07. With this the flow closes: a consumer can complete a login.
    "/oauth/token",
    # P1-08. Served and real; discovery advertises it from the same commit.
    "/oauth/userinfo",
    # P1-09. Both advertised by discovery in the same commit that serves them.
    "/oauth/introspect",
    "/oauth/revoke",
    "/healthz",
    "/readyz",
    # P1-04. Both are served and both are real; the discovery document itself
    # lists only the endpoints that exist, so publishing it does not claim the
    # OIDC endpoints that arrive in P1-06 and P1-07.
    "/.well-known/openid-configuration",
    "/.well-known/jwks.json",
}

SPEC = Path(__file__).resolve().parent.parent / "openapi" / "openapi.yaml"


def documented_paths(text: str) -> list[str]:
    """Path keys under the top-level `paths:` mapping.

    Parsed with a regex rather than a YAML library so this runs on any machine
    with a bare Python, which is what CI and the pre-commit hook can assume.
    The anchoring is deliberately strict — exactly two spaces of indentation
    inside the `paths:` block — so a `/...` string appearing in a description
    is not mistaken for an endpoint.
    """
    head, _, rest = text.partition("\npaths:")
    if not rest:
        raise SystemExit(f"{SPEC}: no top-level `paths:` block found")

    body, _, _ = rest.partition("\ncomponents:")
    return re.findall(r"^  (/[^\s:]*):", body, re.M)


def main() -> int:
    text = SPEC.read_text(encoding="utf-8")
    paths = documented_paths(text)

    if not paths:
        print(f"{SPEC}: no endpoints found — the regex or the file shape changed", file=sys.stderr)
        return 1

    unshipped = sorted(p for p in paths if p not in SHIPPED)
    if unshipped:
        print("The API contract documents endpoints that have not shipped:", file=sys.stderr)
        for p in unshipped:
            print(f"  {p}", file=sys.stderr)
        print(
            "\nThe public API reference renders from this file, so documenting an\n"
            "endpoint publishes a claim that it exists. Add it to SHIPPED in\n"
            f"{Path(__file__).name} in the same commit that ships it.",
            file=sys.stderr,
        )
        return 1

    # Also flag the reverse: something in SHIPPED that the spec no longer
    # documents. That means an endpoint was removed from the contract without
    # this list being updated, and the next addition would then slip through.
    missing = sorted(SHIPPED - set(paths))
    if missing:
        print("SHIPPED lists endpoints the spec no longer documents:", file=sys.stderr)
        for p in missing:
            print(f"  {p}", file=sys.stderr)
        print(
            "\nRemove them from SHIPPED. A stale allowlist quietly widens over time.",
            file=sys.stderr,
        )
        return 1

    print(f"{len(paths)} documented endpoint(s), all shipped.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

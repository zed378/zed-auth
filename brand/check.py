#!/usr/bin/env python3
"""Verifies the brand assets against their generator and their design source.

Two checks, and they catch different failures:

1. The committed SVGs match what `build.py` emits right now. Without this, an
   edit to a generated file survives until the next regeneration silently
   reverts it — the same staleness problem the repo already guards for the
   OpenAPI spec and its generated clients.

2. `build.py`'s geometry still matches the concept document it was transcribed
   from. This is the one that matters more. The mesh is twenty edges and nine
   circles; a transposed coordinate would look plausible in every render and
   would be wrong in every asset forever. Parsing the reference rather than
   trusting the transcription is what makes the copy verifiable.

Neither check can pass vacuously: check 2 fails if the reference is missing or
parses to zero edges, rather than reporting success over an empty comparison.

    python3 brand/check.py
"""

from __future__ import annotations

import re
import sys
import types
from pathlib import Path

HERE = Path(__file__).resolve().parent
REFERENCE = HERE / "reference" / "zed-auth-brand-v3.html"

GENERATED = [
    "zed-auth-mark.svg",
    "zed-auth-mark-dark.svg",
    "zed-auth-mark-mono.svg",
    "zed-auth-app-icon.svg",
    "favicon.svg",
]

problems: list[str] = []


def load_build():
    """Loads build.py from its SOURCE, never from cached bytecode.

    importlib's file loader will happily execute a stale .pyc, and it did:
    this check once reported a coordinate that had already been reverted on
    disk, because it was validating yesterday's bytecode. A verifier reading a
    cache instead of the file it claims to verify is the same failure as a
    test that never ran, so the cache is stepped around entirely rather than
    invalidated and hoped about.
    """
    source = (HERE / "build.py").read_text(encoding="utf-8")
    module = types.ModuleType("brand_build")
    module.__file__ = str(HERE / "build.py")
    exec(compile(source, str(HERE / "build.py"), "exec"), module.__dict__)
    return module


# --- 1. the committed SVGs match the generator -------------------------------


def check_generated_files_are_current(build) -> None:
    """Regenerates into a scratch directory and compares byte for byte."""
    import tempfile

    with tempfile.TemporaryDirectory() as tmp:
        original = build.HERE
        build.HERE = Path(tmp)
        try:
            # Silence the generator's progress output; this is a check, not a build.
            import io as _io
            import contextlib

            with contextlib.redirect_stdout(_io.StringIO()):
                build.main()
        finally:
            build.HERE = original

        for name in GENERATED:
            committed = HERE / name
            fresh = Path(tmp) / name

            if not fresh.exists():
                problems.append(f"{name}: build.py no longer emits this file")
                continue
            if not committed.exists():
                problems.append(f"{name}: emitted by build.py but not committed")
                continue

            if committed.read_text(encoding="utf-8") != fresh.read_text(encoding="utf-8"):
                problems.append(
                    f"{name} is stale — it differs from what build.py emits. "
                    f"Run: python3 brand/build.py"
                )


# --- 2. the generator matches the concept document ---------------------------

NUM = r"-?\d+(?:\.\d+)?"


def parse_reference() -> tuple[set, set, tuple]:
    if not REFERENCE.exists():
        problems.append(
            f"the concept document is missing at {REFERENCE.relative_to(HERE.parent)}. "
            f"Without it the geometry cannot be verified against anything, and a "
            f"transcription error in build.py would be undetectable."
        )
        return set(), set(), ()

    html = REFERENCE.read_text(encoding="utf-8")

    edges = {
        (float(a), float(b), float(c), float(d))
        for a, b, c, d in re.findall(
            rf'<line x1="({NUM})" y1="({NUM})" x2="({NUM})" y2="({NUM})"/>', html
        )
    }

    circles = [
        (float(x), float(y), float(r))
        for x, y, r in re.findall(
            rf'<circle cx="({NUM})" cy="({NUM})" r="({NUM})"', html
        )
    ]

    # The hub is the circle at the centre; the rest are the application nodes.
    hub = next((c for c in circles if (c[0], c[1]) == (100.0, 100.0)), ())
    nodes = {c for c in circles if (c[0], c[1]) != (100.0, 100.0)}

    if not edges or not nodes:
        problems.append(
            "the concept document parsed to no edges or no nodes. The check "
            "would pass against anything in this state, so it fails instead."
        )

    return edges, nodes, hub


def check_geometry_matches_reference(build) -> None:
    ref_edges, ref_nodes, ref_hub = parse_reference()
    if not ref_edges or not ref_nodes:
        return

    # Edges are undirected: the reference and the generator may name the same
    # edge from either end, and that is not a difference.
    def canonical(edge):
        (x1, y1), (x2, y2) = edge
        a, b = (float(x1), float(y1)), (float(x2), float(y2))
        return (a + b) if a <= b else (b + a)

    ours = {canonical(e) for e in build.EDGES}
    theirs = {canonical(((a, b), (c, d))) for a, b, c, d in ref_edges}

    if len(build.EDGES) != len(ours):
        problems.append(
            f"build.py lists {len(build.EDGES)} edges but only {len(ours)} are "
            f"distinct — an edge is duplicated."
        )

    for missing in sorted(theirs - ours):
        problems.append(f"edge in the concept but not in build.py: {missing}")
    for extra in sorted(ours - theirs):
        problems.append(f"edge in build.py but not in the concept: {extra}")

    our_nodes = {(float(x), float(y), float(r)) for x, y, r in build.NODES}
    for missing in sorted(ref_nodes - our_nodes):
        problems.append(f"node in the concept but not in build.py: {missing}")
    for extra in sorted(our_nodes - ref_nodes):
        problems.append(f"node in build.py but not in the concept: {extra}")

    if ref_hub and tuple(float(v) for v in build.HUB) != ref_hub:
        problems.append(f"hub is {build.HUB} in build.py, {ref_hub} in the concept")


# --- 3. the deployed copies still match their source -------------------------

# Each surface keeps its own copy of the assets it uses. The duplication is
# deliberate: PLAN/20 allows the console and the public site to share the
# visual language and nothing else, so neither reaches into the other's tree.
#
# Duplication on purpose and duplication by accident look identical six months
# later, and the failure is quiet — one surface's logo updates and the other's
# does not, and nobody notices until someone opens both at once. This is the
# same guard check-brand-tokens.mjs applies to the colour values.
COPIES = {
    "console/public/favicon.svg": "favicon.svg",
    "console/public/zed-auth-mark.svg": "zed-auth-mark.svg",
    "public-site/static/img/favicon.svg": "favicon.svg",
    "public-site/static/img/logo.svg": "zed-auth-mark.svg",
    "public-site/static/img/logo-dark.svg": "zed-auth-mark-dark.svg",
}


def check_deployed_copies(_build) -> None:
    repo = HERE.parent

    for relative, source in COPIES.items():
        copy = repo / relative
        canonical = HERE / source

        if not canonical.exists():
            problems.append(f"{relative}: its source brand/{source} is missing")
            continue
        if not copy.exists():
            problems.append(
                f"{relative} is missing — it should be a copy of brand/{source}"
            )
            continue

        if copy.read_text(encoding="utf-8") != canonical.read_text(encoding="utf-8"):
            problems.append(
                f"{relative} has drifted from brand/{source}. Copy it again: "
                f"the surfaces duplicate assets on purpose, and this is what "
                f"keeps the duplication honest."
            )


def main() -> int:
    build = load_build()

    check_generated_files_are_current(build)
    check_geometry_matches_reference(build)
    check_deployed_copies(build)

    if problems:
        print("brand assets FAILED:\n")
        for problem in problems:
            print(f"  - {problem}")
        return 1

    print(
        f"brand assets OK — {len(GENERATED)} files current, "
        f"{len(build.EDGES)} edges and {len(build.NODES)} nodes match the concept, "
        f"{len(COPIES)} deployed copies in step"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Emits the Zed Auth brand SVGs from one definition of the mesh.

The mark is eight outer nodes, one hub and twenty edges. Hand-maintaining that
across five files is twenty chances per file to transpose a coordinate, and a
transposed coordinate in a logo is the kind of error that ships and then sits
in every favicon, letterhead and app store listing for a year. So the geometry
is written once, here, and every variant is generated from it.

**This is an extraction, not a design.** Every coordinate, colour, radius and
proportion comes from `reference/zed-auth-brand-v3.html`. Where the concept's
own choices have measurable consequences — and two of them do — those are
reported in BRAND.md rather than corrected here.

The committed SVGs are the artifact; this is their source. `check.py` verifies
that the two are in step and that this file still matches the reference.

    python3 brand/build.py
"""

from __future__ import annotations

import textwrap
from pathlib import Path

HERE = Path(__file__).resolve().parent

# --- the mesh ---------------------------------------------------------------

# Eight application nodes. The two on the vertical axis are slightly smaller,
# which is what stops the ring reading as a mechanical octagon.
NODES: list[tuple[int, int, float]] = [
    (52, 40, 7.5),
    (100, 34, 7.0),
    (150, 44, 7.5),
    (34, 100, 7.5),
    (166, 96, 7.5),
    (52, 156, 7.5),
    (100, 166, 7.0),
    (150, 158, 7.5),
]

# Twenty edges. Hand-placed in the concept, not generated: every one terminates
# on a real node, which is the difference between a network and a texture.
#
# The first six are the Z — top bar, diagonal through the hub, bottom bar. The
# concept describes the letterform as a path through the graph rather than a
# shape laid over it, and this ordering preserves that reading.
EDGES: list[tuple[tuple[int, int], tuple[int, int]]] = [
    # The Z: top bar.
    ((52, 40), (100, 34)),
    ((100, 34), (150, 44)),
    # The Z: diagonal, through the hub.
    ((150, 44), (100, 100)),
    ((100, 100), (52, 156)),
    # The Z: bottom bar.
    ((52, 156), (100, 166)),
    ((100, 166), (150, 158)),
    # The outer ring, closing the silhouette down each side.
    ((52, 40), (34, 100)),
    ((34, 100), (52, 156)),
    ((150, 44), (166, 96)),
    ((166, 96), (150, 158)),
    # Spokes into the hub. These are what make it a hub rather than a junction.
    ((52, 40), (100, 100)),
    ((100, 34), (100, 100)),
    ((100, 100), (150, 158)),
    ((100, 100), (100, 166)),
    ((34, 100), (100, 100)),
    ((100, 100), (166, 96)),
    # Cross-braces: the triangulation that reads as a network of trust rather
    # than as a wheel.
    ((34, 100), (100, 34)),
    ((100, 34), (166, 96)),
    ((34, 100), (100, 166)),
    ((100, 166), (166, 96)),
]

HUB = (100, 100, 14.0)

EDGE_WIDTH = 3.2
HUB_RING_WIDTH = 2.5

# --- palette, exactly as specified ------------------------------------------

# One ramp, running top-right to bottom-left. The concept applies it unchanged
# on light and dark surfaces alike.
RAMP = [("0%", "#6BA3FA"), ("45%", "#2E4FD1"), ("100%", "#0B1220")]

HUB_FILL = "#16224A"
HUB_RING_LIGHT = "#4655F5"
HUB_RING_DARK = "#7B85FA"

TILE = "#0B1220"
TILE_RADIUS = 44  # 22% of 200.
TILE_MARK_FILL = 0.64  # The concept's `.icon-tile .zmark { width: 64% }`.

# --- emitters ---------------------------------------------------------------


def gradient(gid: str) -> str:
    stops = "\n".join(
        f'      <stop offset="{offset}" stop-color="{color}"/>' for offset, color in RAMP
    )
    # userSpaceOnUse with the concept's coordinates, so the ramp runs corner to
    # corner across the mesh rather than across each element's own bounding box.
    return (
        f"  <defs>\n"
        f'    <linearGradient id="{gid}" gradientUnits="userSpaceOnUse"\n'
        f'                    x1="178" y1="18" x2="30" y2="182">\n'
        f"{stops}\n"
        f"    </linearGradient>\n"
        f"  </defs>\n"
    )


def edges(paint: str, indent: str = "  ") -> str:
    lines = "\n".join(
        f'{indent}  <line x1="{a[0]}" y1="{a[1]}" x2="{b[0]}" y2="{b[1]}"/>'
        for a, b in EDGES
    )
    return (
        f'{indent}<g stroke="{paint}" stroke-width="{EDGE_WIDTH}" '
        f'stroke-linecap="round" fill="none">\n{lines}\n{indent}</g>\n'
    )


def nodes(paint: str, indent: str = "  ") -> str:
    circles = "\n".join(
        f'{indent}  <circle cx="{x}" cy="{y}" r="{r}"/>' for x, y, r in NODES
    )
    return f'{indent}<g fill="{paint}">\n{circles}\n{indent}</g>\n'


def hub(fill: str, ring: str | None, indent: str = "  ") -> str:
    x, y, r = HUB
    if ring is None:
        return f'{indent}<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}"/>\n'
    return (
        f'{indent}<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}"\n'
        f'{indent}        stroke="{ring}" stroke-width="{HUB_RING_WIDTH}"/>\n'
    )


def mark(gid: str, ring: str) -> str:
    return (
        gradient(gid)
        + "\n"
        + edges(f"url(#{gid})")
        + "\n"
        + nodes(f"url(#{gid})")
        + "\n"
        + hub(HUB_FILL, ring)
    )


def tiled(gid: str) -> str:
    """The mark on the icon tile, at the concept's 64%."""
    offset = 100 * (1 - TILE_MARK_FILL)
    return (
        f'  <rect width="200" height="200" rx="{TILE_RADIUS}" fill="{TILE}"/>\n\n'
        + gradient(gid)
        + "\n"
        + f'  <g transform="translate({offset:g} {offset:g}) scale({TILE_MARK_FILL:g})">\n'
        + edges(f"url(#{gid})", indent="    ")
        + "\n"
        + nodes(f"url(#{gid})", indent="    ")
        + "\n"
        + hub(HUB_FILL, HUB_RING_DARK, indent="    ")
        + "  </g>\n"
    )


def document(name: str, comment: str, body: str) -> str:
    tid = f"{name}-title"
    banner = textwrap.indent(textwrap.dedent(comment).strip("\n"), "    ")
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"\n'
        f'     role="img" aria-labelledby="{tid}">\n'
        f'  <title id="{tid}">Zed Auth</title>\n'
        f"  <!--\n{banner}\n\n"
        "    Extracted from brand/reference/zed-auth-brand-v3.html and\n"
        "    generated by brand/build.py — edit the generator, not this file.\n"
        "    brand/check.py fails if the two drift, and re-verifies the\n"
        "    geometry against the reference.\n"
        "  -->\n\n"
        f"{body}"
        "</svg>\n"
    )


def write(filename: str, content: str) -> None:
    (HERE / filename).write_text(content, encoding="utf-8", newline="\n")
    print(f"  wrote brand/{filename}")


def main() -> None:
    write(
        "zed-auth-mark.svg",
        document(
            "zed-auth-mark",
            """
            The Zed Auth identity mark, for LIGHT surfaces.

            A triangulated identity graph: eight application nodes on a
            light-blue-to-navy ramp, one darker hub ringed in indigo at the
            centre. Twenty edges, every one terminating on a real node. The Z
            is a path through the graph — top bar, diagonal through the hub,
            bottom bar — rather than a shape sitting on top of it.

            The drawn content spans 34-166 of the 200 box, so the mark occupies
            66% of whatever size it is set to. That margin is the concept's own
            framing: at the lockup it puts a 72px mark beside a 34px wordmark.
            """,
            mark("zedMesh", HUB_RING_LIGHT),
        ),
    )

    write(
        "zed-auth-mark-dark.svg",
        document(
            "zed-auth-mark-dark",
            """
            The Zed Auth identity mark, for DARK surfaces.

            Identical to zed-auth-mark.svg except for the hub ring, which the
            concept lightens to #7B85FA on dark. The mesh ramp is unchanged,
            exactly as the concept applies it to both panels.

            The ramp's deep stop is #0B1220, which is also the concept's dark
            panel and icon tile, so the bottom-left of the mesh has nothing to
            sit against there. That is the concept's own behaviour, preserved
            rather than corrected. BRAND.md measures it.
            """,
            mark("zedMesh", HUB_RING_DARK),
        ),
    )

    write(
        "zed-auth-mark-mono.svg",
        document(
            "zed-auth-mark-mono",
            """
            Single-colour mark, per the concept's own mono symbol.

            Everything is currentColor, so this file must be INLINED in the
            DOM. Referenced through <img src> or as a CSS background it has no
            colour to inherit and renders black.

            The hub loses its ring here and becomes a solid disc, as it does in
            the concept's mono symbol: without the gradient the ring would be
            the same colour as its fill, so the hierarchy falls back to size —
            r=14 against the nodes' 7.5.
            """,
            edges("currentColor")
            + "\n"
            + nodes("currentColor")
            + "\n"
            + hub("currentColor", None),
        ),
    )

    # The concept presents ONE asset under the heading "App icon & favicon" and
    # renders it at 512, 128, 48 and 16. Two files are emitted because
    # consumers reference them by different names; they are the same mark on
    # the same tile at the same 64%, deliberately not two designs.
    write(
        "zed-auth-app-icon.svg",
        document(
            "zed-auth-app-icon",
            """
            App icon: the mark on the ink-navy tile, at the concept's 64% and
            22% corner radius.

            Identical in content to favicon.svg. The concept presents them as
            one asset; they are kept as two files only because consumers
            reference them by different names.
            """,
            tiled("zedMesh"),
        ),
    )

    write(
        "favicon.svg",
        document(
            "zed-auth-favicon",
            """
            Favicon: the same mark on the same tile as zed-auth-app-icon.svg.

            The concept treats the app icon and the favicon as one asset and
            renders the full mesh down to 16px, so that is what ships. What the
            mesh does at that size is measured in BRAND.md rather than designed
            around here.
            """,
            tiled("zedMesh"),
        ),
    )


if __name__ == "__main__":
    main()

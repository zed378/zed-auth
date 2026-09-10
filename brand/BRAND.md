# Brand assets

The Zed Auth identity mark, extracted from the concept document in
[`reference/`](./reference/zed-auth-brand-v3.html).

Everything here is an **extraction**. Coordinates, colours, radii and
proportions are the concept's. Where a choice in the concept has a measurable
consequence it is written down below rather than corrected in the files —
those are design decisions and they are not ours to make silently.

## The files

| File | For | Notes |
|---|---|---|
| `zed-auth-mark.svg` | Light surfaces | The primary asset |
| `zed-auth-mark-dark.svg` | Dark surfaces | Identical but for the hub ring, which the concept lightens to `#7B85FA` |
| `zed-auth-mark-mono.svg` | Print, embossing, inline single-colour | `currentColor` throughout — **must be inlined**, see below |
| `zed-auth-app-icon.svg` | App icon | The mark on the navy tile at 64%, 22% corner radius |
| `favicon.svg` | Favicon | Identical in content to the app icon; the concept presents them as one asset |

`zed-auth-mark-mono.svg` paints with `currentColor`, so referencing it through
`<img src>` or as a CSS background gives it no colour to inherit and it renders
black. Inline it in the DOM, or use one of the two-colour variants.

The other files carry a gradient with an `id`. **Two inlined copies on one page
collide on that id** and the second silently renders with the first's gradient,
so prefer `<img>` for those unless you namespace the ids yourself.

## Regenerating

The SVGs are generated. `build.py` holds the geometry once; hand-maintaining
twenty edges across five files is twenty chances per file to transpose a
coordinate, and a transposed coordinate in a logo is wrong everywhere, forever.

```
python3 brand/build.py     # regenerate the SVGs
python3 brand/check.py     # verify everything below is still true
```

`check.py` asserts three things, and they fail for different reasons:

1. **The committed SVGs match what `build.py` emits.** Otherwise a hand-edit to
   a generated file survives until the next regeneration silently reverts it.
2. **`build.py`'s geometry matches the reference document.** The transcription
   is verified against its original rather than trusted. A wrong coordinate
   would look plausible in every render.
3. **The deployed copies match their source.** The console and the public site
   each keep their own copy — `docs/PLAN/20` allows them to share the visual
   language and no code — and this is what stops one updating without the
   other. The same guard `check-brand-tokens.mjs` applies to the colour values.

None of the three can pass vacuously: a missing reference is a failure, not a
skipped comparison.

## Where the copies live

| Copy | Source |
|---|---|
| `console/public/favicon.svg` | `favicon.svg` |
| `console/public/zed-auth-mark.svg` | `zed-auth-mark.svg` |
| `public-site/static/img/favicon.svg` | `favicon.svg` |
| `public-site/static/img/logo.svg` | `zed-auth-mark.svg` |
| `public-site/static/img/logo-dark.svg` | `zed-auth-mark-dark.svg` |

Change a source, run `build.py`, copy, run `check.py`.

## Two things the concept does that are worth knowing

Neither is a defect. Both are measured, and both are recorded here so that a
future reader knows they were seen rather than missed.

### The ramp's deep stop is the dark surface

The mesh gradient runs `#6BA3FA` → `#2E4FD1` → `#0B1220`, and the concept
applies it unchanged to the light panel, the dark panel and the icon tile. The
dark panel and the tile are also `#0B1220`.

Contrast of each stop against the navy panel:

| Stop | | Against `#0B1220` |
|---|---|---|
| `#6BA3FA` | 0% — top right | 7.35 |
| `#2E4FD1` | 45% | 2.81 |
| `#0B1220` | 100% — bottom left | **1.00** |

So on dark the mesh fades out toward its bottom-left corner and the last of it
has nothing to sit against at all. On light the opposite end is the quiet one:
`#6BA3FA` measures 2.46 against paper.

WCAG 1.4.11 exempts logotypes from its 3:1 requirement, so neither is a
compliance problem, and a gradient that fades is a gradient doing its job. It
is worth knowing because it means **the mark cannot be recoloured by swapping
the surface underneath it** — a dark surface that is not `#0B1220` changes how
much of the mesh survives.

If a fully-legible dark variant is ever wanted, the fix is a lifted ramp
(`#9CC2FF` → `#6BA3FA` → `#4A86EA`, whose deepest stop measures 5.25 on the
navy), and it is a brand decision rather than a bug fix.

### The mesh has no small-size reduction

Twenty edges at `stroke-width` 3.2 in a 200 box are 1.6% of the width each. At
16px that is about a quarter of a pixel, so the favicon renders as a blur; 32px
is marginal and 48px is where it starts to hold.

The concept specifies no simplified form and renders the full mesh from 512px
down to 16px, so that is what ships. Anything else would be a new design.

If a small-size mark is ever wanted, the reduction the concept's own wording
points at is the Z — it calls the letterform "a path through the graph ...
along the top bar, diagonal, and bottom bar", so those six edges plus the hub
are what would survive. Two cautions from having tried it: drawn at the weight
a small size needs, the Z inherits the mesh's deliberately uneven rows and
reads as a wobble unless the nodes it passes through stay visible to explain
the bend; and below about 24px those nodes merge into the stroke, so a
16px-legible version ends up being a different drawing again.

## Palette

| | | |
|---|---|---|
| Mesh, light | `#6BA3FA` | 0% of the ramp |
| Mesh, mid | `#2E4FD1` | 45% |
| Mesh, deep / ink | `#0B1220` | 100%, and the tile |
| Hub fill | `#16224A` | |
| Hub ring, light | `#4655F5` | |
| Hub ring, dark | `#7B85FA` | |

**These are not the product's colours.** The console and the public site both
run on `--color-accent: #1d4ed8`, kept in step by
`public-site/scripts/check-brand-tokens.mjs`. The concept's indigo is `#4655F5`.
Nothing here changes the product tokens, and whether it should is an open
question — see `TASKS/BACKLOG.md`.

## Typography

The concept sets the wordmark in Space Grotesk. Neither surface loads a
webfont: `docs/UI-UX/00` rules one out for the console specifically — "a webfont's
flash of unstyled text on a tool people open fifty times a day is a poor trade
for a little personality" — and the public site follows the same system stack.

So the wordmark ships as live text in each surface's own font, and only the
symbol is a shared asset. That matches how the concept document itself is
built: its lockup is an `<svg>` beside HTML text, not a single image.

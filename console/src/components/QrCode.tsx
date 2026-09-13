/**
 * A QR code drawn from a module grid (P3-10).
 *
 * The API returns rows of `1`s and `0`s rather than an image or markup, so the
 * console needs no QR library and nothing from the network is ever inserted as
 * HTML. Each dark module is a `rect`; the quiet zone is the wrapper's padding.
 *
 * **Not the accessible form of the secret.** `role="img"` with a label tells a
 * screen reader what this is; the text alternative — the secret itself, which
 * an authenticator app also accepts typed — is always shown beside it by the
 * caller. A QR code alone would be an enrolment only sighted users can finish
 * (`docs/UI-UX/13`, and the card's own Definition of Done).
 *
 * Dark modules on the surface colour: scanners expect dark-on-light, and the
 * console has one light theme, so the two tokens give the contrast a camera
 * needs without a raw colour.
 */
export function QrCode({ rows, label }: { rows: string[]; label: string }) {
  const size = rows.length;
  if (size === 0) return null;

  const modules: { x: number; y: number }[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) {
      if (row[x] === "1") modules.push({ x, y });
    }
  });

  return (
    <div className="inline-block rounded border border-border bg-bg-surface p-4">
      <svg
        role="img"
        aria-label={label}
        viewBox={`0 0 ${size} ${size}`}
        className="block h-48 w-48 text-text-primary"
        shapeRendering="crispEdges"
      >
        {modules.map(({ x, y }) => (
          <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill="currentColor" />
        ))}
      </svg>
    </div>
  );
}

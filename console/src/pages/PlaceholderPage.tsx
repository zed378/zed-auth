/**
 * A destination that exists in the information architecture but not yet in the
 * product.
 *
 * Saying so is the point. A route that renders nothing looks broken, and one
 * that renders a convincing empty state claims a capability that has not
 * shipped — the honesty rule `UI-UX/21` applies to marketing copy, applied to
 * a screen.
 */
export function PlaceholderPage({ title, phase }: { title: string; phase: string }) {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-heading-1 font-bold text-text-primary">{title}</h1>

      <p className="max-w-prose text-body text-text-secondary">
        This screen is specified in <code>UI-UX/08-PAGE-SPECIFICATIONS.md</code> and is
        built in phase {phase}. The route exists now because the navigation structure is a
        decision already made; the screen behind it is not built yet.
      </p>
    </div>
  );
}

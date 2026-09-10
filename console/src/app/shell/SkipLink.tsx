/**
 * Skip to the main content.
 *
 * docs/UI-UX/13 § Keyboard Navigation. Without it, a keyboard or screen reader user
 * tabs through the whole navigation on every page load before reaching the
 * thing they came for — on a console someone opens fifty times a day that is
 * not a minor inconvenience.
 *
 * Visually hidden until focused rather than `display: none`, which would take
 * it out of the tab order and make it useless. `sr-only` keeps it in the
 * accessibility tree and in the tab order; the first Tab on any page reveals
 * it.
 */
export function SkipLink() {
  return (
    <a
      href="#main-content"
      className="
        sr-only
        focus:not-sr-only
        focus:absolute focus:left-4 focus:top-4 focus:z-50
        focus:rounded focus:bg-bg-surface focus:px-4 focus:py-2
        focus:text-body focus:font-medium focus:text-accent
        focus:shadow-raised
      "
    >
      Skip to main content
    </a>
  );
}

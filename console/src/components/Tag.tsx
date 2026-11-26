/** Non-interactive labels for categories and metadata, rather than status. */
export function Tag({ children }: { children: string }) {
  if (!children.trim()) throw new Error("A tag requires visible text.");
  return <span className="inline-flex rounded border border-border bg-bg-surface px-2 py-0.5 text-small text-text-primary">{children}</span>;
}

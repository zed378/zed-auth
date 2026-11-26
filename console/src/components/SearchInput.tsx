import { useEffect, useId, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

export interface SearchInputProps {
  label: string;
  query: string;
  onQueryChange: (query: string) => void;
  onSearch: (query: string) => void;
  variant?: "global" | "scoped";
  placeholder?: string;
  loading?: boolean;
  /** Counts must belong to this query; stale responses are never announced. */
  results?: { query: string; count: number };
  debounceMs?: number;
  loadingDelayMs?: number;
}

/** Global shortcut: Ctrl+K / Command+K while this input is mounted in the shell. */
export function SearchInput({ label, query, onQueryChange, onSearch, variant = "scoped", placeholder,
  loading = false, results, debounceMs = 250, loadingDelayMs = 200 }: SearchInputProps) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const callback = useRef(onSearch);
  const [composing, setComposing] = useState(false);
  const [slowQuery, setSlowQuery] = useState<string | null>(null);

  useEffect(() => { callback.current = onSearch; }, [onSearch]);
  useEffect(() => {
    if (composing) return;
    const timer = window.setTimeout(() => callback.current(query), debounceMs);
    return () => window.clearTimeout(timer);
  }, [query, composing, debounceMs]);

  useEffect(() => {
    if (!loading) return;
    const timer = window.setTimeout(() => setSlowQuery(query), loadingDelayMs);
    return () => { window.clearTimeout(timer); setSlowQuery(null); };
  }, [loading, query, loadingDelayMs]);

  useEffect(() => {
    if (variant !== "global") return;
    function focusSearch(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === "k" && !event.isComposing) {
        event.preventDefault();
        input.current?.focus();
        input.current?.select();
      }
    }
    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, [variant]);

  const searching = loading && slowQuery === query;
  const currentResults = !loading && !composing && results?.query === query;
  return <div className="grid gap-1.5">
    <label htmlFor={id} className="text-small font-medium text-text-primary">{label}</label>
    <div className="flex items-center gap-2">
      <input ref={input} id={id} type="search" value={query} placeholder={placeholder}
        aria-keyshortcuts={variant === "global" ? "Control+k Meta+k" : undefined}
        aria-describedby={variant === "global" ? `${id}-shortcut` : undefined}
        onChange={event => onQueryChange(event.target.value)}
        onCompositionStart={() => setComposing(true)} onCompositionEnd={() => setComposing(false)}
        className="w-full rounded-md border border-border bg-bg-surface px-3 py-2 text-body text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent" />
      {variant === "global" && <span id={`${id}-shortcut`} className="shrink-0 text-small text-text-secondary">Ctrl / ⌘ K</span>}
    </div>
    <div role="status" aria-live="polite" aria-atomic="true" className="min-h-5 text-small text-text-secondary">
      {searching ? "Searching…" : currentResults ? `${results.count} ${results.count === 1 ? "result" : "results"}` : ""}
    </div>
  </div>;
}

/** Keeps unrelated URL filters intact and avoids a history entry per keystroke. */
export function ScopedSearchInput({ queryParam = "q", ...props }: Omit<SearchInputProps, "query" | "onQueryChange" | "variant"> & { queryParam?: string }) {
  const [params, setParams] = useSearchParams();
  return <SearchInput {...props} variant="scoped" query={params.get(queryParam) ?? ""}
    onQueryChange={query => setParams(previous => {
      const next = new URLSearchParams(previous);
      if (query) next.set(queryParam, query);
      else next.delete(queryParam);
      return next;
    }, { replace: true })} />;
}

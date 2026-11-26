import { useEffect, useId, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Button } from "./Button";

const focus = "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent";
const card = "rounded border border-border bg-bg-surface p-4";

export interface TabItem { key: string; label: string; content: ReactNode; disabled?: boolean }

/** Mount inside a router. Each instance needs its own query parameter. */
export function Tabs({ label, items, parameter = "tab" }: { label: string; items: TabItem[]; parameter?: string }) {
  const [params, setParams] = useSearchParams();
  const id = useId();
  const buttons = useRef<Array<HTMLButtonElement | null>>([]);
  const enabled = items.filter((item) => !item.disabled);
  const selected = enabled.find((item) => item.key === params.get(parameter)) ?? enabled[0];
  function select(key: string) {
    setParams((current) => { const next = new URLSearchParams(current); next.set(parameter, key); return next; }, { preventScrollReset: true });
  }
  function navigate(event: KeyboardEvent<HTMLButtonElement>, key: string) {
    const index = enabled.findIndex((item) => item.key === key);
    const next = event.key === "Home" ? enabled[0] : event.key === "End" ? enabled.at(-1)
      : event.key === "ArrowRight" ? enabled[(index + 1) % enabled.length]
      : event.key === "ArrowLeft" ? enabled[(index + enabled.length - 1) % enabled.length] : undefined;
    if (!next) return;
    event.preventDefault();
    select(next.key);
    buttons.current[items.indexOf(next)]?.focus();
  }
  return <div>
    <div role="tablist" aria-label={label} className="flex gap-2 border-b border-border">
      {items.map((item, index) => <button key={item.key} ref={(element) => { buttons.current[index] = element; }} type="button" role="tab"
        id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={selected?.key === item.key}
        tabIndex={selected?.key === item.key ? 0 : -1} disabled={item.disabled}
        onKeyDown={(event) => navigate(event, item.key)} onClick={() => select(item.key)}
        className={`border-b-2 px-3 py-2 ${focus} ${selected?.key === item.key ? "border-accent text-accent" : "border-transparent text-text-secondary"}`}>
        {item.label}
      </button>)}
    </div>
    {items.map((item, index) => <div key={item.key} role="tabpanel" id={`${id}-panel-${index}`} aria-labelledby={`${id}-tab-${index}`}
      hidden={selected?.key !== item.key} tabIndex={0} className={`py-4 ${focus}`}>{item.content}</div>)}
  </div>;
}

/** The flow owns draft data and validation; this indicator never discards a draft. */
export function StepIndicator({ steps, current, onBack }: { steps: string[]; current: number; onBack?: (step: number) => void }) {
  return <nav aria-label="Progress" className="space-y-2">
    <p className="text-body font-medium">Step {current + 1} of {steps.length}</p>
    <ol className="flex flex-wrap gap-3">{steps.map((step, index) => <li key={step} aria-current={index === current ? "step" : undefined}
      className={index === current ? "font-medium text-text-primary" : "text-text-secondary"}>
      {index < current && onBack ? <button type="button" onClick={() => onBack(index)} className={`underline ${focus}`}>{index + 1}. {step}</button> : <span>{index + 1}. {step}</span>}
    </li>)}</ol>
  </nav>;
}

export function InlineSuccess({ children }: { children: ReactNode }) {
  return <p role="status" className="flex items-center gap-2 text-body text-success"><span aria-hidden="true">✓</span>{children}</p>;
}

/** Ten seconds of reading time; hover or keyboard focus pauses dismissal. */
export function Toast({ children, onDismiss, duration = 10000 }: { children: ReactNode; onDismiss: () => void; duration?: number }) {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  useEffect(() => {
    if (hovered || focused || duration <= 0) return;
    const timer = window.setTimeout(onDismiss, duration);
    return () => window.clearTimeout(timer);
  }, [hovered, focused, duration, onDismiss]);
  return <div className={`${card} motion-toast-enter flex items-center gap-3`}
    onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
    onFocus={() => setFocused(true)} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}>
    <InlineSuccess>{children}</InlineSuccess>
    <Button onClick={onDismiss} aria-label="Dismiss notification">Dismiss</Button>
  </div>;
}

export function CopyButton({ value, label = "Copy" }: { value: string; label?: string }) {
  const [result, setResult] = useState<{ value: string; state: "idle" | "copying" | "copied" | "error" }>({ value, state: "idle" });
  const state = result.value === value ? result.state : "idle";
  const version = useRef(0);
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { version.current += 1; }, [value]);
  async function copy() {
    const request = version.current;
    setResult({ value, state: "copying" });
    try {
      await navigator.clipboard.writeText(value);
      if (mounted.current && request === version.current) setResult({ value, state: "copied" });
    } catch {
      if (mounted.current && request === version.current) setResult({ value, state: "error" });
    }
  }
  return <div className="space-y-2">
    <Button onClick={() => { void copy(); }} loading={state === "copying"}>{state === "copied" ? "✓ Copied" : label}</Button>
    <span role="status" className={state === "error" ? "text-warning text-body" : "sr-only"}>
      {state === "copied" ? "Copied to clipboard." : state === "error" ? "Could not copy. Select the value and copy it manually, or try again." : ""}
    </span>
  </div>;
}

export function KpiCard({ label, value, description, to }: { label: string; value: ReactNode; description?: string; to?: string }) {
  const content = <><span className="text-body text-text-secondary">{label}</span><strong className="block text-heading-1">{value}</strong>
    {description && <span className="text-body text-text-secondary">{description}</span>}{to && <span className="block text-body text-accent underline">View {label.toLowerCase()} →</span>}</>;
  return to ? <Link to={to} className={`${card} block ${focus} hover:bg-bg-base`}>{content}</Link> : <div className={card}>{content}</div>;
}

export function PaginationFooter({ previousCursor, nextCursor, onPage, loading = false, count }: {
  previousCursor?: string | null; nextCursor?: string | null; onPage: (cursor: string) => void; loading?: boolean; count?: number;
}) {
  return <nav aria-label="Pagination" className="flex flex-wrap items-center justify-between gap-3 border-t border-border py-3">
    <span role="status" className="text-body text-text-secondary">{loading ? "Loading page…" : count === undefined ? "" : `${count} ${count === 1 ? "result" : "results"} on this page`}</span>
    <div className="flex gap-2">
      <Button disabled={loading || previousCursor == null} disabledReason={loading ? "A page is loading." : "No previous page."}
        onClick={() => { if (previousCursor != null) onPage(previousCursor); }}>Previous</Button>
      <Button disabled={loading || nextCursor == null} disabledReason={loading ? "A page is loading." : "No next page."}
        onClick={() => { if (nextCursor != null) onPage(nextCursor); }}>Next</Button>
    </div>
  </nav>;
}

/** Retry only the failed query; the caller supplies its refetch callback. */
export function InlineErrorBanner({ message, onRetry, retrying = false }: { message: string; onRetry: () => void; retrying?: boolean }) {
  return <div className={`${card} flex flex-wrap items-center justify-between gap-3`}>
    <p role="alert" className="text-body text-text-primary"><span aria-hidden="true">! </span>{message}</p>
    <Button onClick={onRetry} loading={retrying}>Try again</Button>
  </div>;
}

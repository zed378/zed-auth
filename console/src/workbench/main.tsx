import { useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { applyBranding, validateBranding } from "../branding/branding";
import { catalog, pendingEntries } from "./catalog";
import "../styles/tokens.css";

function Workbench() {
  const [accent, setAccent] = useState("");
  const branded = useRef<HTMLDivElement>(null);
  const [rejections, setRejections] = useState<string[]>([]);

  function changeAccent(value: string) {
    setAccent(value);
    const target = branded.current;
    if (!target) return;
    target.style.removeProperty("--color-accent");
    const surface = getComputedStyle(target).getPropertyValue("--color-bg-surface").trim();
    const result = validateBranding({ accentColor: value || undefined }, surface);
    applyBranding(target, result);
    setRejections(result.rejected.map((item) => item.reason));
  }

  return (
    <main className="min-h-screen bg-bg-base p-6 text-text-primary">
      <h1 className="text-heading-1 font-semibold">Component workbench</h1>
      <p className="mt-2 text-body text-text-secondary">Development fixtures only. These examples make no API requests.</p>
      <label className="mt-4 block text-small" htmlFor="brand-accent">Organization accent (hex)</label>
      <input id="brand-accent" value={accent} onChange={(event) => changeAccent(event.currentTarget.value)} className="mt-1 rounded border border-border bg-bg-surface px-3 py-2 text-body focus-visible:outline-accent" />
      <p role="status" className="mt-2 text-small text-text-secondary">{rejections.length ? `Accent rejected: ${rejections.join(", ")}. Default retained.` : "Accent meets the branding contrast check; empty uses the default."}</p>
      <div className="mt-6 grid gap-6 desktop:grid-cols-2">
        {["Default theme", "Organization theme"].map((theme, index) => (
          <div key={theme} ref={index === 1 ? branded : undefined}>
            <h2 className="text-heading-2 font-semibold">{theme}</h2>
            {catalog.map((entry) => (
              <section key={entry.name} className="mt-4 rounded border border-border bg-bg-surface p-4">
                <h3 className="text-heading-3 font-medium">{entry.name}</h3>
                <p className="mt-2 text-small text-text-secondary">{entry.rule}</p>
                {entry.examples.map((example) => (
                  <div key={example.name} className="mt-4">
                    <h4 className="mb-2 text-small font-medium">{example.name}</h4>
                    {example.render(`example-${index}-${entry.name}-${example.name}`)}
                  </div>
                ))}
              </section>
            ))}
          </div>
        ))}
      </div>
      <p className="mt-6 text-body text-text-secondary">Entries still pending: {pendingEntries.join(", ")}.</p>
    </main>
  );
}

if (!import.meta.env.DEV) throw new Error("The workbench is development-only.");
const root = document.getElementById("root");
if (!root) throw new Error("workbench: missing root");
createRoot(root).render(<Workbench />);

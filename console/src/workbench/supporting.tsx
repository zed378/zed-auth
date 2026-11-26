import { useState } from "react";
import { MemoryRouter, useSearchParams } from "react-router-dom";
import { Button } from "../components/Button";
import { CopyButton, InlineErrorBanner, InlineSuccess, KpiCard, PaginationFooter, StepIndicator, Tabs, Toast } from "../components/Supporting";
import type { ComponentEntry } from "./catalog";

function StepsExample() {
  const [params, setParams] = useSearchParams();
  const [name, setName] = useState("");
  const current = params.get("step") === "review" ? 1 : 0;
  const change = (step: number) => setParams({ step: step === 1 ? "review" : "details" });
  return <div className="space-y-4">
    <StepIndicator steps={["Details", "Review"]} current={current} onBack={change} />
    {current === 0 ? <><label className="block text-body">Display name <input value={name} onChange={(event) => setName(event.target.value)} className="rounded border border-border p-2" /></label>
      <Button onClick={() => change(1)}>Review</Button></> : <p className="text-body">Summary: display name is {name || "not provided"}. Going back retains this draft.</p>}
  </div>;
}

function ToastExample() {
  const [visible, setVisible] = useState(true);
  return visible ? <Toast onDismiss={() => setVisible(false)}>Display preferences saved.</Toast> : <Button onClick={() => setVisible(true)}>Show notification</Button>;
}

function RetryExample() {
  const [retried, setRetried] = useState(false);
  return retried ? <InlineSuccess>Example request retried.</InlineSuccess> : <InlineErrorBanner message="Could not load this section." onRetry={() => setRetried(true)} />;
}

function PaginationExample() {
  const [cursor, setCursor] = useState("first");
  return <PaginationFooter count={cursor === "first" ? 20 : 7} previousCursor={cursor === "first" ? null : "first"} nextCursor={cursor === "first" ? "second" : null} onPage={setCursor} />;
}

export const supportingEntries: ComponentEntry[] = [
  { name: "Tabs", rule: "Arrow keys, Home and End select tabs; the URL retains the active tab. Inactive panels stay mounted to preserve drafts.", requiredStates: ["ready", "disabled"], examples: [
    { name: "ready", render: () => <MemoryRouter><Tabs label="Project details" items={[{ key: "overview", label: "Overview", content: "Project overview" }, { key: "roles", label: "Roles", content: "Project roles" }]} /></MemoryRouter> },
    { name: "disabled", render: () => <MemoryRouter><Tabs label="Example tabs" items={[{ key: "one", label: "Available", content: "Available panel" }, { key: "two", label: "Unavailable", content: "", disabled: true }]} /></MemoryRouter> },
  ] },
  { name: "Step indicator", rule: "Keep draft state in the flow owner. URL-reflected review and details steps retain data on back navigation; final review shows a complete summary.", requiredStates: ["details-review"], examples: [{ name: "details-review", render: () => <MemoryRouter><StepsExample /></MemoryRouter> }] },
  { name: "Toast / inline success", rule: "Announce success with text and a mark. Toasts allow ten seconds and pause while hovered or focused; high-risk confirmations use persistent success instead.", requiredStates: ["toast", "inline"], examples: [{ name: "toast", render: () => <ToastExample /> }, { name: "inline", render: () => <InlineSuccess>Changes saved.</InlineSuccess> }] },
  { name: "Copy to clipboard", rule: "Report actual clipboard success or failure. Never display the copied value in feedback.", requiredStates: ["ready", "copied-error"], examples: [{ name: "ready", render: () => <CopyButton value="example-public-client-id" /> }, { name: "copied-error", render: () => <div className="space-y-2"><CopyButton value="another-example-client-id" label="Copy example ID" /><p className="text-body">Activate Copy to see Copied or an explicit failure when clipboard access is unavailable.</p></div> }] },
  { name: "KPI card", rule: "Only cards with a real destination have link or hover affordances. Values here are illustrative component data.", requiredStates: ["static", "linked"], examples: [{ name: "static", render: () => <KpiCard label="Example users" value={12} /> }, { name: "linked", render: () => <MemoryRouter><KpiCard label="Example projects" value={3} to="/projects" /></MemoryRouter> }] },
  { name: "Pagination footer", rule: "Pass opaque server cursors through unchanged; disable paging during a request. The first cursor may be an empty string.", requiredStates: ["ready", "loading", "empty"], examples: [{ name: "ready", render: () => <PaginationExample /> }, { name: "loading", render: () => <PaginationFooter loading onPage={() => {}} /> }, { name: "empty", render: () => <PaginationFooter count={0} onPage={() => {}} /> }] },
  { name: "Inline error banner", rule: "Retry the failed query only, preserving the rest of the page and scroll position. A retry is unavailable while its request is running.", requiredStates: ["error", "retrying"], examples: [{ name: "error", render: () => <RetryExample /> }, { name: "retrying", render: () => <InlineErrorBanner message="Could not load this section." retrying onRetry={() => {}} /> }] },
];

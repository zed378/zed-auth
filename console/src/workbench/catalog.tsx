import type { ReactNode } from "react";
import { Badge, Button, RoleSourceBadge, Table } from "../components";

export interface Example {
  name: string;
  render: (id: string) => ReactNode;
}

export interface ComponentEntry {
  name: string;
  rule: string;
  requiredStates: readonly string[];
  examples: Example[];
}

const columns = [{ key: "name", header: "Project", cell: (row: { name: string }) => row.name }];

export const catalog: ComponentEntry[] = [
  {
    name: "Button",
    rule: "Use one primary action per section. Reserve destructive weight for rare, irreversible confirmations. Explain disabled actions nearby. Tab to inspect focus and point to inspect hover.",
    requiredStates: ["default", "hover", "focus", "disabled", "loading"],
    examples: [
      { name: "default", render: () => <div className="flex flex-wrap gap-3"><Button variant="primary">Create project</Button><Button>Cancel</Button><Button variant="destructive">Revoke grant</Button></div> },
      { name: "hover", render: () => <Button>Point here to inspect hover</Button> },
      { name: "focus", render: () => <Button>Tab here to inspect focus</Button> },
      { name: "disabled", render: (id) => <><Button disabled aria-describedby={id}>Save changes</Button><p id={id} className="mt-2 text-small text-text-secondary">Change a value before saving.</p></> },
      { name: "loading", render: () => <Button loading>Save changes</Button> },
    ],
  },
  {
    name: "Badge",
    rule: "Always include text. Color alone never communicates status or the source of access.",
    requiredStates: ["neutral", "positive", "attention", "muted", "direct", "delegated"],
    examples: [
      ...(["neutral", "positive", "attention", "muted"] as const).map((tone) => ({ name: tone, render: () => <Badge tone={tone}>{tone}</Badge> })),
      { name: "direct", render: () => <RoleSourceBadge roleKey="project.viewer" /> },
      { name: "delegated", render: () => <RoleSourceBadge roleKey="project.viewer" delegatedFrom="Example organization" /> },
    ],
  },
  {
    name: "Table",
    rule: "Use skeleton rows during loading and distinguish a first empty result from a filter with no matches. Tables displaying access must show role-source badges.",
    requiredStates: ["default", "loading", "empty", "filtered-empty", "error"],
    examples: ["default", "loading", "empty", "filtered-empty", "error"].map((state) => ({
      name: state,
      render: () => <Table caption="Example projects" columns={columns} rows={state === "default" ? [{ name: "Example project" }] : []} rowKey={(row) => row.name} status={state === "loading" ? "loading" : state === "error" ? "error" : "ready"} filtered={state === "filtered-empty"} what="projects" />,
    })),
  },
];

// These are visible gaps, not placeholder implementations of later tasks.
export const pendingEntries = ["Form Field", "Modal / Side Panel", "Confirmation Dialog", "Breadcrumb", "Search Input"];

import { useState, type ReactNode } from "react";
import { Button, ChoiceField, MultiValueField, RadioGroupField, SelectField, Table, TextAreaField, TextField, ToggleField } from "../components";
import { badgeEntry } from "./badges";
import { breadcrumbEntry } from "./breadcrumbs";
import { dialogEntry } from "./dialogs";
import { searchEntry } from "./search";
import { confirmationEntry } from "./confirmations";
import { supportingEntries } from "./supporting";
import { stateEntry } from "./states";
import { formEntry } from "./forms";
import { responsiveEntry } from "./responsive";

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
        { name: "disabled", render: (id) => <><Button disabled disabledReason="Change a value before saving." aria-describedby={id}>Save changes</Button><p id={id} className="mt-2 text-small text-text-secondary">Change a value before saving.</p></> },
      { name: "loading", render: () => <Button loading>Save changes</Button> },
    ],
  },
  badgeEntry,
  breadcrumbEntry,
  dialogEntry,
  searchEntry,
  confirmationEntry,
  stateEntry,
  formEntry,
  responsiveEntry,
  ...supportingEntries,
  {
    name: "Table",
    rule: "Use skeleton rows during loading and distinguish a first empty result from a filter with no matches. Tables displaying access must show role-source badges.",
    requiredStates: ["default", "loading", "empty", "filtered-empty", "error"],
    examples: ["default", "loading", "empty", "filtered-empty", "error"].map((state) => ({
      name: state,
        render: () => <Table caption="Example projects" columns={columns} rows={state === "default" || state === "error" ? [{ name: "Example project" }] : []} rowKey={(row) => row.name} status={state === "loading" ? "loading" : state === "error" ? "error" : "ready"} filtered={state === "filtered-empty"} what="projects" onRetry={() => undefined} />,
    })),
  },
    {
      name: "Form Field",
      rule: "Keep labels visible. Errors replace helper text. Removing draft values offers inline undo; switches apply immediately without a Save action.",
      requiredStates: ["default", "focus", "error", "disabled", "validating", "input-types"],
      examples: [
        { name: "default", render: (id) => <TextField id={id} label="Application name" helperText="A name your team recognizes." /> },
        { name: "focus", render: (id) => <TextField id={id} label="Tab here to inspect focus" /> },
        { name: "error", render: (id) => <TextField id={id} label="Application name" helperText="A name your team recognizes." error="Enter an application name." /> },
        { name: "disabled", render: (id) => <TextField id={id} label="Application name" disabled helperText="You need permission to edit this application." /> },
        { name: "validating", render: (id) => <TextField id={id} label="Application name" validating /> },
        { name: "input-types", render: (id) => <FieldExamples id={id} /> },
      ],
    },
];

function FieldExamples({ id }: { id: string }) {
  const [enabled, setEnabled] = useState(false);
  const [role, setRole] = useState("viewer");
  const [values, setValues] = useState(["https://app.example.com/callback"]);
  return <div className="grid gap-4">
    <TextAreaField id={`${id}-description`} label="Description" />
    <SelectField id={`${id}-region`} label="Region"><option>Europe</option><option>Asia</option></SelectField>
    <ChoiceField id={`${id}-notify`} label="Notify by email" />
    <RadioGroupField id={`${id}-role`} name={`${id}-role`} label="Role" value={role} onChange={setRole} options={[{ value: "viewer", label: "Viewer" }, { value: "editor", label: "Editor" }]} />
    <ToggleField id={`${id}-enabled`} label="Notifications" checked={enabled} onChange={setEnabled} />
    <MultiValueField id={`${id}-redirects`} label="Redirect URIs" values={values} onChange={setValues} />
  </div>;
}

// These are visible gaps, not placeholder implementations of later tasks.
export const pendingEntries: string[] = [];

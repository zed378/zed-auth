import { useState } from "react";
import { Button, Modal, SidePanel } from "../components";
import type { ComponentEntry } from "./catalog";

function DialogExample({ panel = false, locked = false }: { panel?: boolean; locked?: boolean }) {
  const [open, setOpen] = useState(false);
  const Component = panel ? SidePanel : Modal;
  return <><Button onClick={() => setOpen(true)}>Open {panel ? "side panel" : "modal"}{locked ? " with acknowledgement" : ""}</Button>
    <Component open={open} title={panel ? "Invite user" : "Project details"} onClose={() => setOpen(false)}
      {...(!panel ? { dismissible: !locked } : { progress: "Step 1 of 2" })}
      footer={<Button onClick={() => setOpen(false)}>{locked ? "I have read this" : "Done"}</Button>}>
      <p>Use Tab and Shift+Tab to navigate. Closing restores focus to the opener.</p>
      <label className="mt-4 block">Name<input className="mt-1 block w-full rounded border border-border bg-bg-base px-3 py-2" /></label>
    </Component>
  </>;
}

export const dialogEntry: ComponentEntry = {
  name: "Modal / Side Panel",
  rule: "Use a modal for focused actions and a side panel for detail-heavy flows. Destructive confirmations always use an instant modal. Panels take over the screen below desktop width.",
  requiredStates: ["modal", "side-panel", "acknowledgement"],
  examples: [
    { name: "modal", render: () => <DialogExample /> },
    { name: "side-panel", render: () => <DialogExample panel /> },
    { name: "acknowledgement", render: () => <DialogExample locked /> },
  ],
};

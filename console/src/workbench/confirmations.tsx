import { useEffect, useState } from "react";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import type { ComponentEntry } from "./catalog";

function ConfirmationExample({ state }: { state: string }) {
  const [open, setOpen] = useState(false);
  const [success, setSuccess] = useState<string>();
  const [loading, setLoading] = useState(state === "loading");
  const [failed, setFailed] = useState(state === "error");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!open || !busy) return;
    const timer = setTimeout(() => {
      setBusy(false);
      setSuccess("Example grant revoked. The partner's project access has ended.");
    }, 1500);
    return () => clearTimeout(timer);
  }, [open, busy]);
  useEffect(() => {
    if (!open || !loading) return;
    const timer = setTimeout(() => setLoading(false), 1500);
    return () => clearTimeout(timer);
  }, [open, loading]);
  return <>
    <Button onClick={() => { setSuccess(undefined); setBusy(state === "busy"); setLoading(state === "loading"); setFailed(state === "error"); setOpen(true); }}>Preview {state} confirmation</Button>
    <ConfirmDialog open={open} title="Revoke project grant?" verb="Revoke grant"
      consequence="In this example, the partner's 14 users lose every role provided by this grant. Their accounts stay active."
      typeToConfirm={state === "typed" ? "Acme" : undefined}
      consequenceLoading={loading} consequenceError={failed ? "Affected users could not be loaded." : undefined}
      onRetryConsequence={() => setFailed(false)}
      success={success} busy={busy}
      onCancel={() => setOpen(false)}
      onConfirm={() => setSuccess("Example grant revoked. The partner's project access has ended.")} />
  </>;
}

export const confirmationEntry: ComponentEntry = {
  name: "Confirmation Dialog",
  rule: "State actual consequences before an action. Typed confirmation is reserved for grant revocation with dependents and organization deletion. Counts here are demonstration fixtures.",
  requiredStates: ["default", "typed", "loading", "error", "busy", "success"],
  examples: ["default", "typed", "loading", "error", "busy", "success"].map(state => ({
    name: state,
    render: () => state === "success" ? <SuccessExample /> : <ConfirmationExample state={state} />,
  })),
};

function SuccessExample() {
  const [open, setOpen] = useState(false);
  return <><Button onClick={() => setOpen(true)}>Preview success acknowledgement</Button>
    <ConfirmDialog open={open} title="Project grant revoked" verb="Revoke grant" consequence="Partner access ends."
      success="Example grant revoked. Partner access has ended." onConfirm={() => undefined} onCancel={() => setOpen(false)} />
  </>;
}

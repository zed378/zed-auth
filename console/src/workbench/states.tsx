import { useState } from "react";
import { Button } from "../components/Button";
import { CollectionStateBoundary, PermissionDeniedState, Skeleton } from "../components/states";
import type { ComponentEntry } from "./catalog";

function StateExample({ state }: { state: string }) {
  const [recovered, setRecovered] = useState(false);
  const [filtered, setFiltered] = useState(true);
  if (state === "permission-denied") return <PermissionDeniedState />;
  const stale = state === "stale-error";
  return <CollectionStateBoundary
    count={recovered || stale ? 1 : 0} loading={state === "loading"}
    failed={!recovered && (state === "error" || stale)}
    what="projects" loadingLabel="Loading projects"
    filtered={state === "filtered-empty" && filtered}
    onClearFilter={() => setFiltered(false)}
    emptyAction={<Button variant="primary" onClick={() => setRecovered(true)}>Create example project</Button>}
    onRetry={() => setRecovered(true)}
    skeleton={<div className="space-y-3"><Skeleton className="h-6 w-full" /><Skeleton className="h-6 w-full" /></div>}
  ><p className="text-body text-text-primary">Example project</p></CollectionStateBoundary>;
}

export const stateEntry: ComponentEntry = {
  name: "Collection states",
  rule: "Loading precedes empty; definitive errors precede loading. Keep last-known data on refresh failure, hide it on permission denial, and clear filters instead of offering creation for filtered results. Retry only the failed request.",
  requiredStates: ["loading", "empty", "filtered-empty", "error", "stale-error", "permission-denied"],
  examples: ["loading", "empty", "filtered-empty", "error", "stale-error", "permission-denied"].map((state) => ({
    name: state, render: () => <StateExample state={state} />,
  })),
};

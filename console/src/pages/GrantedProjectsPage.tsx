import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { RoleList, RoleSourceBadge } from "../components/RoleSourceBadge";
import { SidePanel } from "../components/SidePanel";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import type { components } from "../lib/api/schema.gen";
import {
  useDelegatedUserGrants,
  useOrgId,
  useReceivedGrants,
  useUsers,
} from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Received = components["schemas"]["ReceivedGrant"];
type Delegated = components["schemas"]["Grant"];
type User = { id: string; email: string; display_name?: string | null };

/**
 * Granted Projects — the receiving side of delegation (P4-06,
 * `docs/UI-UX/04` Flow 3, `docs/UI-UX/08` § Granted Projects list).
 *
 * The implementation chain (`docs/UI-UX/19`) is committed beside the code at
 * `console/docs/implementation-chain-P4-06.md`.
 *
 * Three things decide whether this screen is safe.
 *
 * **A role the grant does not delegate is never rendered** — not shown
 * disabled, not shown greyed, not in the DOM (`docs/UI-UX/08`). The granting
 * organization's other roles are not this administrator's business, and a
 * disabled control invites a support ticket asking to have it enabled.
 *
 * **Every role here is marked as delegated, naming the organization it came
 * from** (`docs/UI-UX/06`). "Where did this role come from" has two answers
 * with completely different remedies, and on this screen the answer is always
 * the same one: another organization lent it, and can take it back.
 *
 * **A revoked grant stays listed and says so.** Access is already gone when
 * the status arrives; a row that vanished would leave an administrator
 * wondering what they had done, and any assignments left behind would be
 * invisible rather than merely inert.
 */
export function GrantedProjectsPage() {
  const orgId = useOrgId();
  const grants = useReceivedGrants(orgId);
  const { hasRole } = useAuth();

  const [managing, setManaging] = useState<Received | null>(null);

  const rows = useMemo(() => grants.data ?? [], [grants.data]);

  // UX only. The API enforces ORG_ADMIN over this organization independently on
  // every request (`CLAUDE.md`, `docs/PLAN/08`).
  const mayManage = hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<Received>[] = [
    {
      key: "project",
      header: "Project",
      cell: (grant) => (
        <span className="flex flex-col">
          <span className="text-text-primary">{grant.project_name}</span>
          <code className="font-mono text-small text-text-secondary">{grant.project_id}</code>
        </span>
      ),
    },
    {
      key: "from",
      header: "From",
      cell: (grant) => (
        <span className="flex flex-col">
          <span className="text-text-primary">{grant.granting_org_name}</span>
          <code className="font-mono text-small text-text-secondary">{grant.granting_org_id}</code>
        </span>
      ),
    },
    {
      key: "roles",
      header: "Roles you can assign",
      // Named, and marked as delegated with the source organization: this is
      // the one screen where every role has an owner elsewhere.
      cell: (grant) => (
        <RoleList roleKeys={grant.granted_role_keys} delegatedFrom={grant.granting_org_name} />
      ),
    },
    {
      key: "holders",
      header: "Assigned to",
      cell: (grant) => (
        <span className={grant.holder_count === 0 ? "text-text-secondary" : undefined}>
          {holders(grant.holder_count)}
        </span>
      ),
    },
    {
      key: "status",
      header: "Status",
      // Text, never colour alone (`docs/UI-UX/07` § Badge rule).
      cell: (grant) =>
        grant.status === "active" ? (
          <Badge tone="positive">Active</Badge>
        ) : (
          <Badge tone="muted">Ended</Badge>
        ),
      secondary: true,
    },
    {
      key: "created",
      header: "Granted",
      cell: (grant) => (
        <span className="flex flex-col">
          <span>{formatDate(grant.created_at)}</span>
          {grant.revoked_at !== null && grant.revoked_at !== undefined ? (
            <span className="text-small text-text-secondary">
              Ended {formatDate(grant.revoked_at)}
            </span>
          ) : null}
        </span>
      ),
      secondary: true,
    },
  ];

  return (
    <>
      <h1 className="text-heading-1 font-medium text-text-primary">Granted Projects</h1>

      {/*
        Always present, not only when the list is empty. Delegation is the most
        unfamiliar idea in the console (`docs/UI-UX/09`), and the person
        removing an assignment needs the model as much as the person making one.
      */}
      <p className="mt-2 max-w-prose text-body text-text-secondary">
        Another organization can share some of its project&apos;s roles with yours. You can give
        those roles — and only those — to your own people. The roles stay that organization&apos;s:
        it chooses which ones are shared, and it can end the arrangement at any time.
      </p>

      <div className="mt-4">
        <Table<Received>
          caption="Projects granted to this organization"
          columns={columns}
          rows={rows}
          rowKey={(grant) => grant.id}
          status={grants.isPending ? "loading" : grants.isError ? "error" : "ready"}
          errorKind={kindOf(grants.error)}
          onRetry={() => void grants.refetch()}
          what="granted projects"
          emptyAction={
            // The empty state's own sentence: this list is not something this
            // organization can add to, and "create the first one" would be a
            // dead end (`docs/UI-UX/14`).
            <p className="max-w-prose text-body text-text-secondary">
              Only another organization can grant a project to yours. When one does, it appears
              here with the roles you may assign.
            </p>
          }
          actions={
            mayManage
              ? (grant) =>
                  grant.status === "active" ? (
                    <span className="flex justify-end">
                      <Button onClick={() => setManaging(grant)}>Assign roles</Button>
                    </span>
                  ) : (
                    <span className="flex items-center justify-end gap-2">
                      <span className="text-small text-text-secondary">
                        {grant.granting_org_name} ended this
                      </span>
                      {/*
                        No assignment control on an ended grant. Clearing up is
                        a different action, and it is only offered when there is
                        something left to clear.
                      */}
                      {grant.holder_count > 0 ? (
                        <Button onClick={() => setManaging(grant)}>Review</Button>
                      ) : null}
                    </span>
                  )
              : undefined
          }
        />
      </div>

      <GrantPanel grant={managing} orgId={orgId} onClose={() => setManaging(null)} />
    </>
  );
}

/**
 * One grant: who here holds its roles, and — while it is active — assigning
 * them to somebody else.
 *
 * A side panel rather than a modal (`docs/UI-UX/04` Flow 3): the list behind it
 * stays readable, which matters when an administrator is working through
 * several grants from the same partner.
 */
function GrantPanel({
  grant,
  orgId,
  onClose,
}: {
  grant: Received | null;
  orgId: string | null;
  onClose: () => void;
}) {
  const grantId = grant?.id ?? null;
  const assignments = useDelegatedUserGrants(orgId, grantId);
  const users = useUsers(orgId, "");

  const [chosenUser, setChosenUser] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [problem, setProblem] = useState<string | null>(null);
  const [removing, setRemoving] = useState<Delegated | null>(null);

  const queryClient = useQueryClient();
  const ended = grant !== null && grant.status !== "active";

  function reset() {
    setChosenUser("");
    setSelected(new Set());
    setProblem(null);
  }

  function dismiss() {
    reset();
    onClose();
  }

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: [...queryKeys.delegatedGrants, orgId, grantId] });
    // The holder count on the row behind the panel is now stale.
    void queryClient.invalidateQueries({ queryKey: [...queryKeys.receivedGrants, orgId] });
  }

  const assign = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST(
        "/v1/organizations/{org_id}/project-grants/{grant_id}/user-grants",
        {
          params: { path: { org_id: orgId as string, grant_id: grantId as string } },
          body: { user_id: chosenUser, role_keys: [...selected] },
        },
      );
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      reset();
      refresh();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  const remove = useMutation({
    mutationFn: async (userId: string) => {
      const { error } = await api.DELETE(
        "/v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}",
        {
          params: {
            path: { org_id: orgId as string, grant_id: grantId as string, user_id: userId },
          },
        },
      );
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      setRemoving(null);
      refresh();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  const members = useMemo(() => (users.data?.items ?? []) as User[], [users.data]);
  const held = useMemo(() => assignments.data ?? [], [assignments.data]);
  const nameFor = (userId: string) =>
    members.find((member) => member.id === userId)?.email ?? userId;

  function toggle(key: string) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  const blocked = chosenUser === "" || selected.size === 0;

  return (
    <>
      <SidePanel
        open={grant !== null}
        title={grant === null ? "" : `${grant.project_name} from ${grant.granting_org_name}`}
        onClose={dismiss}
        footer={
          ended ? (
            <Button onClick={dismiss}>Close</Button>
          ) : (
            <>
              <Button onClick={dismiss}>Cancel</Button>
              <Button
                variant="primary"
                disabled={blocked || assign.isPending}
                onClick={() => assign.mutate()}
              >
                {assign.isPending ? "Assigning…" : "Assign roles"}
              </Button>
            </>
          )
        }
      >
        {grant === null ? null : (
          <>
            {ended ? (
              /*
                F-7: a revocation that happened elsewhere is a state, not a
                failure on the next click. The access is already gone — saying
                so is the difference between "nothing works" and "this ended".
              */
              <p role="status" className="max-w-prose text-body text-text-primary">
                <strong className="font-medium">{grant.granting_org_name} ended this grant</strong>
                {grant.revoked_at !== null && grant.revoked_at !== undefined
                  ? ` on ${formatDate(grant.revoked_at)}`
                  : ""}
                . The roles below no longer give anyone access, and no new ones can be assigned.
                You can remove what is left.
              </p>
            ) : (
              <p className="max-w-prose text-body text-text-secondary">
                These roles belong to {grant.granting_org_name} and apply inside its{" "}
                {grant.project_name} project. Assigning one gives that person access to{" "}
                {grant.granting_org_name}&apos;s project — not to anything here.
              </p>
            )}

            <section className="mt-5">
              <h3 className="text-heading-3 font-medium text-text-primary">Who has these roles</h3>
              {assignments.isPending ? (
                <p className="mt-2 text-body text-text-secondary">Loading…</p>
              ) : assignments.isError ? (
                <p role="alert" className="mt-2 text-body text-danger">
                  Those assignments could not be loaded.
                </p>
              ) : held.length === 0 ? (
                <p className="mt-2 text-body text-text-secondary">
                  Nobody in this organization holds a role through this grant yet.
                </p>
              ) : (
                <ul className="mt-2 flex flex-col gap-2">
                  {held.map((assignment) => (
                    <li
                      key={assignment.user_id}
                      className="flex flex-wrap items-center justify-between gap-2 rounded border border-border p-3"
                    >
                      <span className="flex flex-col gap-1">
                        <span className="text-text-primary">{nameFor(assignment.user_id)}</span>
                        <RoleList
                          roleKeys={assignment.role_keys}
                          delegatedFrom={grant.granting_org_name}
                        />
                      </span>
                      <Button variant="danger-text" onClick={() => setRemoving(assignment)}>
                        Remove
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            {ended ? null : (
              <section className="mt-6">
                <h3 className="text-heading-3 font-medium text-text-primary">
                  Give these roles to someone
                </h3>

                <label className="mt-3 flex flex-col gap-1 text-small text-text-secondary">
                  Person
                  <select
                    className="rounded border border-border bg-bg-surface p-2 text-body text-text-primary"
                    value={chosenUser}
                    onChange={(event) => setChosenUser(event.target.value)}
                  >
                    {/*
                      A-2: this organization's own members and nobody else. The
                      server refuses a user outside it independently (P4-02).
                    */}
                    <option value="">Choose a person…</option>
                    {members.map((member) => (
                      <option key={member.id} value={member.id}>
                        {member.display_name !== null && member.display_name !== undefined
                          ? `${member.display_name} — ${member.email}`
                          : member.email}
                      </option>
                    ))}
                  </select>
                </label>

                <fieldset className="mt-4 border-0 p-0">
                  <legend className="text-small text-text-secondary">
                    Roles {grant.granting_org_name} has shared
                  </legend>
                  {/*
                    F-5: rendered from `granted_role_keys` and nothing else. The
                    project's other roles are not listed, not disabled, not in
                    the DOM.
                  */}
                  <div className="mt-2 flex flex-col gap-2">
                    {grant.granted_role_keys.map((key) => (
                      <label key={key} className="flex items-center gap-2 text-body">
                        <input
                          type="checkbox"
                          checked={selected.has(key)}
                          onChange={() => toggle(key)}
                        />
                        <RoleSourceBadge roleKey={key} delegatedFrom={grant.granting_org_name} />
                      </label>
                    ))}
                  </div>
                </fieldset>
              </section>
            )}

            {problem !== null ? (
              <p role="alert" className="mt-4 text-small text-danger">
                {problem}
              </p>
            ) : null}
          </>
        )}
      </SidePanel>

      <ConfirmDialog
        open={removing !== null}
        title="Remove these roles?"
        verb="Remove roles"
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => remove.mutate(removing?.user_id ?? "")}
        consequence={
          <p>
            {nameFor(removing?.user_id ?? "")} will lose{" "}
            <span className="font-mono text-small">{removing?.role_keys.join(", ")}</span> in{" "}
            {grant?.granting_org_name}&apos;s {grant?.project_name} project. You can assign them
            again while the grant is active.
          </p>
        }
      />
    </>
  );
}

// --- helpers -----------------------------------------------------------------

function holders(count: number): string {
  if (count === 0) return "Nobody";
  return `${count} user${count === 1 ? "" : "s"}`;
}

function formatDate(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? iso : at.toLocaleDateString();
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

function messageFor(error: unknown): string {
  const envelope = error as { error?: { message?: string; details?: { issue?: string }[] } };
  return (
    envelope?.error?.details?.[0]?.issue ??
    envelope?.error?.message ??
    "That could not be saved. Please try again."
  );
}

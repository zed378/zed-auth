import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Modal } from "../components/Modal";
import { ProjectNav } from "../components/ProjectNav";
import { RoleList } from "../components/RoleSourceBadge";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import type { components } from "../lib/api/schema.gen";
import { useOrgId, useProjectGrants, useProjects, useRoles } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Grant = components["schemas"]["ProjectGrant"];
type Role = { key: string; display_name: string };

/**
 * The Project Grants tab (P4-05, `docs/UI-UX/18` § Project Grants Tab,
 * `docs/UI-UX/04` Flow 2).
 *
 * The implementation chain (`docs/UI-UX/19`) is committed beside the code at
 * `console/docs/implementation-chain-P4-05.md`.
 *
 * Three decisions here are load-bearing.
 *
 * **What is not shared is shown as plainly as what is.** A delegation is a
 * subset, and the mistake it invites is sharing one role too many. The table
 * names the roles left out of each grant, and the form lists every project role
 * with its state in words, so the omission is visible before and after.
 *
 * **The partner is named by ID** (ADR-026). No API lists other organizations,
 * and a search would let any administrator enumerate every customer on the
 * instance. The name appears once the grant exists.
 *
 * **Revoking states the number of people it affects, and asks for the partner's
 * name when that number is not zero** (`docs/UI-UX/18`). The count comes from
 * the server (`holder_count`); the console cannot see the partner's users.
 */
export function ProjectGrantsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const orgId = useOrgId();
  const projects = useProjects(orgId);
  const grants = useProjectGrants(orgId, projectId ?? null);
  const roles = useRoles(orgId, projectId ?? null);
  const { hasRole } = useAuth();

  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<Grant | null>(null);

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  const rows = useMemo(() => grants.data ?? [], [grants.data]);
  const projectRoles = useMemo(() => (roles.data ?? []) as Role[], [roles.data]);

  // UX only. The API refuses independently on every request (`docs/PLAN/08`,
  // `CLAUDE.md`); these are the roles that satisfy PROJECT_OWNER at project
  // scope, the same set the route is guarded by.
  const mayManage = hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<Grant>[] = [
    {
      key: "organization",
      header: "Organization",
      cell: (grant) => (
        <span className="flex flex-col">
          <span className="text-text-primary">{grant.granted_org_name}</span>
          {/* The ID too: it is what the partner gave, and what they will quote. */}
          <code className="font-mono text-small text-text-secondary">{grant.granted_org_id}</code>
        </span>
      ),
    },
    {
      key: "roles",
      header: "Shared roles",
      // Named, never counted (`docs/UI-UX/18`): which roles is the entire
      // point of this screen. Wraps rather than truncating at tablet width.
      cell: (grant) => <SharedRoles grant={grant} projectRoles={projectRoles} />,
    },
    {
      key: "holders",
      header: "Held by",
      cell: (grant) =>
        grant.status === "revoked" ? (
          <span className="text-text-secondary">—</span>
        ) : (
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
          <Badge tone="muted">Revoked</Badge>
        ),
    },
    {
      key: "created",
      header: "Created",
      cell: (grant) => (
        <span className="flex flex-col">
          <span>{formatDate(grant.created_at)}</span>
          {grant.revoked_at !== null ? (
            <span className="text-small text-text-secondary">
              Revoked {formatDate(grant.revoked_at)}
            </span>
          ) : null}
        </span>
      ),
    },
  ];

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-small text-text-secondary">
        <Link to="/projects" className="underline">
          Projects
        </Link>{" "}
        /{" "}
        <Link to={`/projects/${projectId ?? ""}`} className="underline">
          {project?.name ?? "…"}
        </Link>{" "}
        / <span aria-current="page">Project Grants</span>
      </nav>

      <ProjectNav projectId={projectId ?? ""} />

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Project Grants</h1>
        {mayManage ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            Create grant
          </Button>
        ) : null}
      </div>

      {/*
        Always shown, not only when the table is empty: this is one of the more
        advanced concepts in the console (`docs/UI-UX/09` § progressive
        disclosure), and the person revoking needs the model as much as the
        person creating.
      */}
      <p className="mt-2 max-w-prose text-body text-text-secondary">
        A Project Grant shares some of this project&apos;s roles with another organization, so its
        administrators can give those roles to their own people. Only the roles you choose are
        shared. A grant cannot be widened later; to share more, revoke it and create another.
      </p>

      <div className="mt-4">
        <Table<Grant>
          caption={`Project Grants of ${project?.name ?? "this project"}`}
          columns={columns}
          rows={rows}
          rowKey={(grant) => grant.id}
          status={grants.isPending ? "loading" : grants.isError ? "error" : "ready"}
          errorKind={kindOf(grants.error)}
          onRetry={() => void grants.refetch()}
          what="Project Grants"
          emptyAction={
            mayManage ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Create the first grant
              </Button>
            ) : undefined
          }
          actions={
            mayManage
              ? (grant) =>
                  grant.status === "active" ? (
                    <span className="flex justify-end">
                      <Button variant="danger-text" onClick={() => setRevoking(grant)}>
                        Revoke
                      </Button>
                    </span>
                  ) : (
                    // Not a disabled button: a revoked grant has nothing left
                    // to do, and saying so is clearer than a dead control.
                    <span className="block text-right text-small text-text-secondary">
                      Ended
                    </span>
                  )
              : undefined
          }
        />
      </div>

      <CreateGrantModal
        open={creating}
        orgId={orgId}
        projectId={projectId ?? null}
        projectName={project?.name ?? "this project"}
        projectRoles={projectRoles}
        rolesStatus={roles.isPending ? "loading" : roles.isError ? "error" : "ready"}
        onClose={() => setCreating(false)}
      />

      <RevokeGrantDialog
        grant={revoking}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setRevoking(null)}
      />
    </>
  );
}

/**
 * The roles a grant shares, then the ones it does not.
 *
 * "Not shared" is computed against the project's current roles. A revoked
 * grant may name a role deleted since; it is still listed as shared, because
 * that is what the grant carried.
 */
function SharedRoles({ grant, projectRoles }: { grant: Grant; projectRoles: Role[] }) {
  const shared = new Set(grant.granted_role_keys);
  const withheld = projectRoles.map((role) => role.key).filter((key) => !shared.has(key));

  return (
    <span className="flex flex-col gap-1">
      <RoleList roleKeys={grant.granted_role_keys} none="None" />
      {withheld.length > 0 ? (
        <span className="text-small text-text-secondary">Not shared: {withheld.join(", ")}</span>
      ) : projectRoles.length > 0 ? (
        <span className="text-small text-text-secondary">Every role in the project</span>
      ) : null}
    </span>
  );
}

/**
 * Creation: choose, then read back, then confirm (`docs/UI-UX/04` Flow 2).
 *
 * Two steps inside one modal. The summary is live on the first step as roles
 * are ticked (`docs/UI-UX/11`), and the second step repeats it as the thing
 * being confirmed, because this is the highest-stakes flow in the console and
 * both directions of it get a consequence preview before the final action.
 */
function CreateGrantModal({
  open,
  orgId,
  projectId,
  projectName,
  projectRoles,
  rolesStatus,
  onClose,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  projectName: string;
  projectRoles: Role[];
  rolesStatus: "loading" | "error" | "ready";
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [partner, setPartner] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reviewing, setReviewing] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  function reset() {
    setPartner("");
    setSelected(new Set());
    setReviewing(false);
    setProblem(null);
  }

  function dismiss() {
    reset();
    onClose();
  }

  const partnerId = partner.trim().toLowerCase();
  const chosen = projectRoles.filter((role) => selected.has(role.key));
  const withheld = projectRoles.filter((role) => !selected.has(role.key));

  const create = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST(
        "/v1/organizations/{org_id}/projects/{project_id}/grants",
        {
          params: { path: { org_id: orgId as string, project_id: projectId as string } },
          body: { granted_org_id: partnerId, role_keys: chosen.map((role) => role.key) },
        },
      );
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: [...queryKeys.projectGrants, orgId, projectId],
      });
      dismiss();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  const partnerProblem = partnerIdProblem(partnerId, orgId);
  const partnerInvalid = partnerProblem !== null && partner.trim() !== "";
  const blocked = partnerProblem !== null || chosen.length === 0;

  function toggle(key: string) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  return (
    <Modal
      open={open}
      title={reviewing ? "Confirm the grant" : "Create a Project Grant"}
      onClose={dismiss}
      footer={
        reviewing ? (
          <>
            <Button
              onClick={() => {
                setProblem(null);
                setReviewing(false);
              }}
            >
              Back
            </Button>
            <Button variant="primary" loading={create.isPending} onClick={() => create.mutate()}>
              Create grant
            </Button>
          </>
        ) : (
          <>
            <Button onClick={dismiss}>Cancel</Button>
            <Button variant="primary" disabled={blocked} onClick={() => setReviewing(true)}>
              Review
            </Button>
          </>
        )
      }
    >
      {reviewing ? (
        <>
          <p className="text-body text-text-primary">
            <Summary partnerId={partnerId} chosen={chosen} projectName={projectName} />
          </p>
          <WithheldList withheld={withheld} />
          <p className="mt-3 text-small text-text-secondary">
            The roles cannot be changed after this. To share more or fewer, revoke the grant and
            create another.
          </p>
          {problem !== null ? (
            <p role="alert" className="mt-3 text-small text-danger">
              {problem}
            </p>
          ) : null}
        </>
      ) : (
        <>
          <label htmlFor="grant-partner" className="block text-small font-medium text-text-secondary">
            Organization ID
          </label>
          <input
            id="grant-partner"
            value={partner}
            onChange={(event) => setPartner(event.currentTarget.value)}
            aria-invalid={partnerInvalid || undefined}
            aria-describedby={partnerInvalid ? "grant-partner-error" : "grant-partner-help"}
            autoComplete="off"
            spellCheck={false}
            placeholder="00000000-0000-0000-0000-000000000000"
            className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-body text-text-primary"
          />
          {partnerInvalid ? (
            <p id="grant-partner-error" className="mt-1 text-small text-danger">
              {partnerProblem}
            </p>
          ) : (
            <p id="grant-partner-help" className="mt-1 text-small text-text-secondary">
              Ask the other organization for its ID. For privacy, organizations cannot be searched
              by name, and their name is shown here once the grant exists.
            </p>
          )}

          <fieldset className="mt-4">
            <legend className="text-small font-medium text-text-secondary">Roles to share</legend>
            {rolesStatus === "loading" ? (
              <p className="mt-2 text-small text-text-secondary">Loading this project&apos;s roles…</p>
            ) : rolesStatus === "error" ? (
              <p role="alert" className="mt-2 text-small text-danger">
                This project&apos;s roles could not be loaded. Close this and try again.
              </p>
            ) : projectRoles.length === 0 ? (
              <p className="mt-2 text-small text-text-secondary">
                This project has no roles yet. Define one on the Roles tab first; a grant shares
                roles, so there is nothing to share.
              </p>
            ) : (
              // Every role listed, and each one's state in words beside it —
              // unselected roles are "Not shared", not hidden (Flow 2).
              <ul className="mt-2 divide-y divide-border rounded border border-border">
                {projectRoles.map((role) => {
                  const on = selected.has(role.key);
                  return (
                    <li key={role.key} className="flex items-center justify-between gap-3 px-3 py-2">
                      <label className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          checked={on}
                          onChange={() => toggle(role.key)}
                          className="h-4 w-4"
                        />
                        <span>
                          <code className="font-mono text-small text-text-primary">{role.key}</code>{" "}
                          <span className="text-small text-text-secondary">{role.display_name}</span>
                        </span>
                      </label>
                      <span
                        aria-hidden="true"
                        className={`text-small ${on ? "font-medium text-text-primary" : "text-text-secondary"}`}
                      >
                        {on ? "Shared" : "Not shared"}
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
          </fieldset>

          {projectRoles.length > 0 ? (
            // Live, as the boxes are ticked (`docs/UI-UX/11`). A polite live
            // region so a screen-reader user hears the sentence change too.
            <div aria-live="polite" className="mt-4">
              {chosen.length === 0 ? (
                // `docs/UI-UX/05`'s own example of a warning: a caution about
                // an incomplete state, not a destructive action.
                <p className="rounded border border-warning bg-bg-base p-3 text-small text-warning">
                  This Project Grant has no roles selected yet.
                </p>
              ) : (
                <p className="rounded border border-border bg-bg-base p-3 text-small text-text-primary">
                  <Summary partnerId={partnerId} chosen={chosen} projectName={projectName} />
                </p>
              )}
            </div>
          ) : null}
        </>
      )}
    </Modal>
  );
}

function Summary({
  partnerId,
  chosen,
  projectName,
}: {
  partnerId: string;
  chosen: Role[];
  projectName: string;
}) {
  return (
    <>
      Organization{" "}
      <code className="font-mono">{partnerId === "" ? "(not entered yet)" : partnerId}</code> will be
      able to give {chosen.length === 1 ? "this role" : "these roles"} in {projectName} to its own
      users: <strong className="font-medium">{chosen.map((role) => role.key).join(", ")}</strong>.
    </>
  );
}

function WithheldList({ withheld }: { withheld: Role[] }) {
  if (withheld.length === 0) {
    return (
      <p className="mt-2 text-body text-text-primary">
        That is <strong className="font-medium">every role</strong> in the project.
      </p>
    );
  }
  return (
    <p className="mt-2 text-body text-text-primary">
      Not shared: {withheld.map((role) => role.key).join(", ")}.
    </p>
  );
}

/**
 * Revocation, with the number of people it affects.
 *
 * `color-danger` lives here and on the row's own Revoke control, and nowhere
 * else on this screen (`docs/UI-UX/06`, `CLAUDE.md`). Typed confirmation is
 * asked for only when someone holds a role through the grant: that is the
 * high-impact case `docs/UI-UX/18` names, and friction on every revocation
 * would teach people to type without reading.
 */
function RevokeGrantDialog({
  grant,
  orgId,
  projectId,
  onClose,
}: {
  grant: Grant | null;
  orgId: string | null;
  projectId: string | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [problem, setProblem] = useState<string | null>(null);

  const revoke = useMutation({
    mutationFn: async () => {
      const { error } = await api.DELETE(
        "/v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}",
        {
          params: {
            path: {
              org_id: orgId as string,
              project_id: projectId as string,
              grant_id: (grant as Grant).id,
            },
          },
        },
      );
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: [...queryKeys.projectGrants, orgId, projectId],
      });
      setProblem(null);
      onClose();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  const affected = grant?.holder_count ?? 0;
  const name = grant?.granted_org_name ?? "";

  return (
    <ConfirmDialog
      open={grant !== null}
      title={`Revoke the grant to ${name || "this organization"}?`}
      verb="Revoke grant"
      busy={revoke.isPending}
      typeToConfirm={affected > 0 ? name || grant?.granted_org_id : undefined}
      onCancel={() => {
        setProblem(null);
        onClose();
      }}
      onConfirm={() => revoke.mutate()}
      consequence={
        <>
          {affected > 0 ? (
            <p>
              <strong className="font-medium">
                {holders(affected)} in {name} currently hold{affected === 1 ? "s" : ""} a role
                through this grant, and will lose{" "}
                {affected === 1 ? "it" : "those roles"}.
              </strong>
            </p>
          ) : (
            <p>Nobody currently holds a role through this grant.</p>
          )}
          <p className="mt-2">
            {name} will no longer be able to give{" "}
            <span className="font-mono text-small">{grant?.granted_role_keys.join(", ")}</span> to
            its users. A revoked grant cannot be restored; sharing again means creating a new one.
          </p>
          {problem !== null ? (
            <p role="alert" className="mt-2 text-small text-danger">
              {problem}
            </p>
          ) : null}
        </>
      }
    />
  );
}

// --- helpers -----------------------------------------------------------------

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/**
 * The shape check the server's `format: uuid` makes, and the self-grant it
 * refuses. Whether the ID names a live organization is the server's to say,
 * and it says it identically for "no such organization" and "not permitted".
 */
function partnerIdProblem(value: string, ownOrgId: string | null): string | null {
  if (value === "") return "An organization ID is required.";
  if (!UUID.test(value)) {
    return "An organization ID looks like 3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45.";
  }
  if (ownOrgId !== null && value === ownOrgId.toLowerCase()) {
    return "That is this organization's own ID. A grant shares roles with a different organization.";
  }
  return null;
}

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

import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge, statusTone } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ProjectNav } from "../components/ProjectNav";
import { RoleList } from "../components/RoleSourceBadge";
import { ErrorState, Skeleton } from "../components/states";
import { api, queryKeys } from "../lib/api/client";
import { useOrgId, useProjects, useRoles, useUserGrants, useUsers } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";
import { useDebounced } from "../lib/useDebounced";

type User = { id: string; email: string; status: string; display_name?: string | null };
type Grant = { user_id: string; project_id: string; role_keys: string[] };
type Role = { id: string; key: string; display_name: string; permission_keys: string[] };

/**
 * The Authorizations tab (P2-12, `docs/UI-UX/08` § Project detail —
 * Authorizations tab: "Search user, assign/revoke role").
 *
 * The implementation chain (`docs/UI-UX/19`) is committed beside the code at
 * `console/docs/implementation-chain-P2-12.md`.
 *
 * **Search-then-act, rather than a roster.** That is the API's shape, not a
 * design preference: grants are stored and exposed one user at a time
 * (`/users/{user_id}/grants`), and no endpoint lists everyone with access to a
 * project. The screen is honest about that — it answers "what access does this
 * person have here", which is the question an administrator arrives with, and
 * the missing roster is recorded as `PG-36` rather than faked by fanning out a
 * request per search result.
 */
export function AuthorizationsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const orgId = useOrgId();
  const projects = useProjects(orgId);
  const roles = useRoles(orgId, projectId ?? null);
  const { hasRole } = useAuth();

  const [typed, setTyped] = useState("");
  const [selected, setSelected] = useState<User | null>(null);

  // The input is controlled by the raw value so it never lags behind the
  // keyboard; only the request waits (`P2-12` step 2).
  const search = useDebounced(typed);
  const users = useUsers(orgId, search);

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  const results = (users.data ?? []) as User[];

  const mayManage = hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

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
        / <span aria-current="page">Authorizations</span>
      </nav>

      <ProjectNav projectId={projectId ?? ""} />

      <h1 className="mt-4 text-heading-1 font-medium text-text-primary">Authorizations</h1>

      <p className="mt-2 max-w-prose text-body text-text-secondary">
        Who can do what in {project?.name ?? "this project"}. Access is granted per user per
        project — a user with no grant here has no access at all, which is the normal state for a
        new account.
      </p>

      <div className="mt-4 max-w-md">
        <label htmlFor="user-search" className="block text-small font-medium text-text-secondary">
          Find a user
        </label>
        <input
          id="user-search"
          type="search"
          value={typed}
          onChange={(event) => {
            setTyped(event.currentTarget.value);
            setSelected(null);
          }}
          placeholder="Email address or name"
          aria-describedby="user-search-help"
          className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
        />
        <p id="user-search-help" className="mt-1 text-small text-text-secondary">
          Search the organization's users, then grant or revoke their roles in this project.
        </p>
      </div>

      {/*
        A fixed-height region, so the results appearing does not push the rest
        of the page down (`P2-12` step 2: a loading state that does not shift
        layout). The skeletons have the geometry of the rows they become.
      */}
      <div className="mt-4 min-h-48">
        {selected !== null ? (
          <UserAccessPanel
            user={selected}
            orgId={orgId}
            projectId={projectId ?? null}
            projectName={project?.name ?? "this project"}
            roles={(roles.data ?? []) as Role[]}
            rolesPending={roles.isPending}
            mayManage={mayManage}
            onClear={() => setSelected(null)}
          />
        ) : (
          <SearchResults
            typed={typed}
            settled={search}
            users={results}
            pending={users.isPending || search !== typed}
            failed={users.isError}
            error={users.error}
            onRetry={() => void users.refetch()}
            onPick={setSelected}
          />
        )}
      </div>
    </>
  );
}

/**
 * The search results, in all four of their states.
 *
 * Deliberately not a `Table`: these are choices, not rows of data, and the one
 * thing each does is select. A table with a single action column would imply
 * columns worth scanning.
 */
function SearchResults({
  typed,
  settled,
  users,
  pending,
  failed,
  error,
  onRetry,
  onPick,
}: {
  typed: string;
  settled: string;
  users: User[];
  pending: boolean;
  failed: boolean;
  error: unknown;
  onRetry: () => void;
  onPick: (user: User) => void;
}) {
  if (failed) return <ErrorState kind={kindOf(error)} onRetry={onRetry} />;

  if (pending) {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="sr-only">Searching</span>
        {[0, 1, 2].map((index) => (
          <div key={index} className="border-b border-border py-3">
            <Skeleton className="h-4 w-64" />
          </div>
        ))}
      </div>
    );
  }

  if (users.length === 0) {
    // The two kinds of nothing, again (`docs/UI-UX/14`). One of them is not an
    // empty state at all — it is a screen waiting to be used.
    return (
      <p className="rounded border border-border bg-bg-surface p-5 text-body text-text-secondary">
        {settled.trim() === ""
          ? "Type an email address or name above to find someone."
          : `Nobody in this organization matches “${settled}”.`}
      </p>
    );
  }

  return (
    <>
      <p aria-live="polite" className="text-small text-text-secondary">
        {users.length} {users.length === 1 ? "person" : "people"} match
        {typed.trim() === "" ? "" : ` “${typed}”`}
      </p>
      <ul className="mt-2 divide-y divide-border rounded border border-border bg-bg-surface">
        {users.map((user) => (
          <li key={user.id}>
            <button
              type="button"
              onClick={() => onPick(user)}
              className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left hover:bg-bg-base focus-visible:outline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
            >
              <span>
                <span className="block text-body text-text-primary">{user.email}</span>
                {user.display_name != null && user.display_name !== "" ? (
                  <span className="block text-small text-text-secondary">{user.display_name}</span>
                ) : null}
              </span>
              <Badge tone={statusTone(user.status)}>{user.status}</Badge>
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

/**
 * One user's access to this project, and the controls that change it.
 */
function UserAccessPanel({
  user,
  orgId,
  projectId,
  projectName,
  roles,
  rolesPending,
  mayManage,
  onClear,
}: {
  user: User;
  orgId: string | null;
  projectId: string | null;
  projectName: string;
  roles: Role[];
  rolesPending: boolean;
  mayManage: boolean;
  onClear: () => void;
}) {
  const grants = useUserGrants(orgId, user.id);
  const [editing, setEditing] = useState(false);
  const [revoking, setRevoking] = useState(false);

  const grant = ((grants.data ?? []) as Grant[]).find(
    (candidate) => candidate.project_id === projectId,
  );

  return (
    <section aria-label={`Access for ${user.email}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-heading-2 font-medium text-text-primary">{user.email}</h2>
          <p className="mt-1 text-small text-text-secondary">
            <Link to={`/users/${user.id}`} className="underline">
              Their full profile and every project
            </Link>
          </p>
        </div>
        <Button onClick={onClear}>Find someone else</Button>
      </div>

      <div className="mt-4 rounded border border-border bg-bg-surface p-5">
        <h3 className="text-heading-3 font-medium text-text-primary">Roles in {projectName}</h3>

        {grants.isPending ? (
          <Skeleton className="mt-3 h-6 w-48" />
        ) : grants.isError ? (
          <div className="mt-3">
            <ErrorState kind={kindOf(grants.error)} onRetry={() => void grants.refetch()} />
          </div>
        ) : grant === undefined ? (
          // Step 6: least privilege, stated. A blank here would be ambiguous
          // between "none" and "we did not load it".
          <p className="mt-2 max-w-prose text-body text-text-secondary">
            <strong className="font-medium text-text-primary">No access.</strong> They hold no
            roles in this project, so every authorization check for them here is denied. There is
            no implicit or default role.
          </p>
        ) : (
          <div className="mt-3">
            {/*
              Named, never counted — knowing exactly which roles is the point
              (`docs/UI-UX/18`). Each carries the role-source badge, which in
              Phase 2 always reads direct.
            */}
            <RoleList roleKeys={grant.role_keys} />
          </div>
        )}

        {mayManage ? (
          <div className="mt-4 flex flex-wrap gap-2 border-t border-border pt-4">
            <Button variant="primary" onClick={() => setEditing(true)}>
              {grant === undefined ? "Grant access" : "Change roles"}
            </Button>
            {grant !== undefined ? (
              <Button variant="danger-text" onClick={() => setRevoking(true)}>
                Revoke all access
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>

      <AssignRolesModal
        open={editing}
        orgId={orgId}
        projectId={projectId}
        projectName={projectName}
        user={user}
        roles={roles}
        rolesPending={rolesPending}
        existing={grant?.role_keys ?? []}
        onClose={() => setEditing(false)}
      />

      <RevokeDialog
        open={revoking}
        orgId={orgId}
        projectId={projectId}
        projectName={projectName}
        user={user}
        onClose={() => setRevoking(false)}
      />
    </section>
  );
}

/**
 * Assignment, with the permission keys visible.
 *
 * `P2-12` step 3 asks for the project's roles "with their permission keys
 * visible, so an admin can see what they are granting". A list of role names
 * is a list of words somebody has to already know the meaning of — and the
 * whole risk of this screen is granting more than was intended.
 */
function AssignRolesModal({
  open,
  orgId,
  projectId,
  projectName,
  user,
  roles,
  rolesPending,
  existing,
  onClose,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  projectName: string;
  user: User;
  roles: Role[];
  rolesPending: boolean;
  existing: string[];
  onClose: () => void;
}) {
  const [chosen, setChosen] = useState<string[]>(existing);
  const [problem, setProblem] = useState<string | null>(null);
  const [seeded, setSeeded] = useState(false);
  const queryClient = useQueryClient();

  // Seed from the current grant each time the dialog opens, so reopening after
  // a cancel does not show the abandoned selection.
  if (open && !seeded) {
    setSeeded(true);
    setChosen(existing);
    setProblem(null);
  }
  if (!open && seeded) setSeeded(false);

  const save = useMutation({
    mutationFn: async () => {
      if (existing.length > 0) {
        // PATCH replaces the set entirely — the API is explicit that a partial
        // update of an array is ambiguous, and the audit event needs a
        // complete before and after.
        const { error } = await api.PATCH(
          "/v1/organizations/{org_id}/users/{user_id}/grants/{project_id}",
          {
            params: {
              path: {
                org_id: orgId as string,
                user_id: user.id,
                project_id: projectId as string,
              },
            },
            body: { role_keys: chosen },
          },
        );
        if (error !== undefined) throw error;
        return;
      }

      const { error } = await api.POST("/v1/organizations/{org_id}/users/{user_id}/grants", {
        params: { path: { org_id: orgId as string, user_id: user.id } },
        body: { project_id: projectId as string, role_keys: chosen },
      });
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.grants, orgId, user.id] });
      onClose();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  function toggle(key: string) {
    setChosen((current) =>
      current.includes(key) ? current.filter((entry) => entry !== key) : [...current, key],
    );
  }

  const changed = useMemo(
    () => chosen.slice().sort().join(" ") !== existing.slice().sort().join(" "),
    [chosen, existing],
  );

  // An empty set is refused by the API — "a grant with no roles grants nothing
  // and should not exist". The button says which action removes access, rather
  // than letting somebody discover the refusal.
  const emptied = chosen.length === 0;

  return (
    <ConfirmDialog
      open={open}
      title={`Roles for ${user.email} in ${projectName}`}
      verb={save.isPending ? "Saving…" : "Save roles"}
      busy={save.isPending}
      onCancel={onClose}
      onConfirm={() => {
        if (!emptied && changed) save.mutate();
      }}
      consequence={
        <>
          {rolesPending ? (
            <Skeleton className="h-24 w-full" />
          ) : roles.length === 0 ? (
            <p className="text-text-secondary">
              This project has no roles yet, so there is nothing to grant. Define one on the{" "}
              <Link to={`/projects/${projectId ?? ""}/roles`} className="underline">
                Roles tab
              </Link>{" "}
              first.
            </p>
          ) : (
            <fieldset>
              <legend className="sr-only">Roles in {projectName}</legend>
              <ul className="flex flex-col gap-3">
                {roles.map((role) => (
                  <li key={role.id}>
                    <label className="flex gap-3">
                      <input
                        type="checkbox"
                        checked={chosen.includes(role.key)}
                        onChange={() => toggle(role.key)}
                        className="mt-1 shrink-0"
                      />
                      <span>
                        <span className="block text-body text-text-primary">
                          {role.display_name}{" "}
                          <code className="font-mono text-small text-text-secondary">
                            {role.key}
                          </code>
                        </span>
                        {/*
                          What the role actually carries — step 3. Without it
                          this is a list of names somebody has to already know.
                        */}
                        <span className="mt-0.5 block font-mono text-small text-text-secondary">
                          {role.permission_keys.length === 0
                            ? "No permissions — a label only"
                            : role.permission_keys.join(", ")}
                        </span>
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
            </fieldset>
          )}

          {emptied && roles.length > 0 ? (
            <p className="mt-3 text-small text-text-secondary">
              Selecting none is not the way to remove access — a grant with no roles grants nothing
              and is refused. Close this and use “Revoke all access”.
            </p>
          ) : null}

          {problem !== null ? (
            <p role="alert" className="mt-3 text-small text-danger">
              {problem}
            </p>
          ) : null}
        </>
      }
    />
  );
}

/**
 * Revocation, stating plainly that it takes effect immediately (step 5) — and
 * equally plainly what it does not reach.
 */
function RevokeDialog({
  open,
  orgId,
  projectId,
  projectName,
  user,
  onClose,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  projectName: string;
  user: User;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [problem, setProblem] = useState<string | null>(null);

  const revoke = useMutation({
    mutationFn: async () => {
      const { error } = await api.DELETE(
        "/v1/organizations/{org_id}/users/{user_id}/grants/{project_id}",
        {
          params: {
            path: { org_id: orgId as string, user_id: user.id, project_id: projectId as string },
          },
        },
      );
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.grants, orgId, user.id] });
      setProblem(null);
      onClose();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  return (
    <ConfirmDialog
      open={open}
      title={`Revoke ${user.email}'s access to ${projectName}?`}
      verb="Revoke access"
      busy={revoke.isPending}
      onCancel={() => {
        setProblem(null);
        onClose();
      }}
      onConfirm={() => revoke.mutate()}
      consequence={
        <>
          <p>
            Every role they hold in {projectName} is removed. Their access to other projects is
            unaffected, and their account keeps working.
          </p>
          <p className="mt-2">
            <strong className="font-medium">This takes effect immediately.</strong> The grant is
            deleted rather than flagged, so every authorization check from this moment is denied.
          </p>
          <p className="mt-2 text-text-secondary">
            An access token already issued to them keeps the roles it was minted with until it
            expires — which is what real-time authorization checks exist for. An application that
            needs certainty asks the service rather than trusting the token's claims.
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

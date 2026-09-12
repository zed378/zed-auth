import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Modal } from "../components/Modal";
import { ProjectNav } from "../components/ProjectNav";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import {
  PERMISSION_KEY_MAX_LENGTH,
  ROLE_KEY_MAX_LENGTH,
  isValidPermissionKey,
  isValidRoleKey,
} from "../lib/api/patterns.gen";
import { useOrgId, useProjects, useRoles } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Role = {
  id: string;
  project_id: string;
  key: string;
  display_name: string;
  permission_keys: string[];
  is_builtin: boolean;
  grant_count: number;
};

/**
 * The Roles tab (P2-11, `docs/UI-UX/08` § Project detail — Roles tab).
 *
 * The implementation chain (`docs/UI-UX/19`) is committed beside the code at
 * `console/docs/implementation-chain-P2-11.md`.
 *
 * Two things here are load-bearing rather than cosmetic.
 *
 * **The validation rules are the server's own.** `isValidRoleKey` and
 * `isValidPermissionKey` come from `patterns.gen.ts`, generated from the same
 * OpenAPI schemas the backend's `pattern.gen.go` is generated from, with
 * `scripts/check.sh` failing on drift. A client rule that is merely *similar*
 * produces the worst kind of form: one that accepts what the server refuses,
 * or refuses what the server would have taken — and in both cases the person
 * filling it in has no way to tell which rule is the real one.
 *
 * **Deletion states the affected-grant count first.** The service refuses a
 * role any grant still references (`P2-02`), and an administrator who cannot
 * see the count discovers that by trying, then has to work out why.
 */
export function RolesPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const orgId = useOrgId();
  const projects = useProjects(orgId);
  const roles = useRoles(orgId, projectId ?? null);
  const { hasRole } = useAuth();

  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [search, setSearch] = useState("");

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  // Memoised because `roles.data ?? []` is a fresh array on every render, and
  // an unstable input makes the filter below recompute whether or not anything
  // changed.
  const all = useMemo(() => (roles.data ?? []) as Role[], [roles.data]);

  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (needle === "") return all;
    return all.filter(
      (role) =>
        role.key.toLowerCase().includes(needle) ||
        role.display_name.toLowerCase().includes(needle),
    );
  }, [all, search]);

  // UX only. The API refuses independently on every request, and a hidden
  // button has never been a security control (`docs/PLAN/08`, `CLAUDE.md`).
  const mayManage = hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<Role>[] = [
    {
      key: "key",
      header: "Key",
      // The key is what a grant references and what arrives inside a token
      // claim (`docs/PLAN/08` Part A), so it is the identifier an integrator
      // works from. Shown in full, in the mono face, like the client id on the
      // Applications tab.
      cell: (role) => <code className="font-mono text-small text-text-primary">{role.key}</code>,
    },
    { key: "display_name", header: "Name", cell: (role) => role.display_name },
    {
      key: "permissions",
      header: "Permissions",
      cell: (role) => (
        // A count, with the keys themselves available on hover and to a screen
        // reader. The list can run to 256 entries, and a table cell is the
        // wrong place for it — but "4" with no way to learn which four is a
        // number nobody can act on.
        <span title={role.permission_keys.join(", ")}>
          {role.permission_keys.length === 0 ? (
            <span className="text-text-secondary">None — a label only</span>
          ) : (
            <>
              {role.permission_keys.length}
              <span className="sr-only">{`: ${role.permission_keys.join(", ")}`}</span>
            </>
          )}
        </span>
      ),
    },
    {
      key: "grants",
      header: "Assigned to",
      cell: (role) => (
        <span className={role.grant_count === 0 ? "text-text-secondary" : undefined}>
          {role.grant_count === 0
            ? "Nobody"
            : `${role.grant_count} user${role.grant_count === 1 ? "" : "s"}`}
        </span>
      ),
    },
    {
      key: "kind",
      header: "Origin",
      secondary: true,
      // Text, never a colour alone (`docs/UI-UX/07` § Badge rule).
      cell: (role) => <Badge tone="muted">{role.is_builtin ? "Built-in" : "Defined here"}</Badge>,
    },
  ];

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-small text-text-secondary">
        {/* Every segment clickable, reflecting the real data hierarchy
            (docs/UI-UX/07 § Breadcrumb). */}
        <Link to="/projects" className="underline">
          Projects
        </Link>{" "}
        /{" "}
        <Link to={`/projects/${projectId ?? ""}`} className="underline">
          {project?.name ?? "…"}
        </Link>{" "}
        / <span aria-current="page">Roles</span>
      </nav>

      <ProjectNav projectId={projectId ?? ""} />

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Roles</h1>
        {mayManage ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            Define role
          </Button>
        ) : null}
      </div>

      <p className="mt-2 max-w-prose text-body text-text-secondary">
        A role is a named set of permissions inside this project. Roles are scoped per project —{" "}
        <code className="font-mono text-small">admin</code> here is unrelated to{" "}
        <code className="font-mono text-small">admin</code> in any other project.
      </p>

      <div className="mt-4">
        <label htmlFor="role-search" className="block text-small font-medium text-text-secondary">
          Search
        </label>
        <input
          id="role-search"
          type="search"
          value={search}
          onChange={(event) => setSearch(event.currentTarget.value)}
          placeholder="Key or name"
          className="mt-1 w-full max-w-sm rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
        />
      </div>

      <div className="mt-4">
        <Table<Role>
          caption={`Roles in ${project?.name ?? "this project"}`}
          columns={columns}
          rows={rows}
          rowKey={(role) => role.id}
          status={roles.isPending ? "loading" : roles.isError ? "error" : "ready"}
          errorKind={kindOf(roles.error)}
          onRetry={() => void roles.refetch()}
          what="roles"
          // "No roles defined yet" and "no roles match the search" are
          // different situations wanting different recoveries — a create
          // action versus a cleared search (`docs/UI-UX/14`, step 6).
          filtered={search.trim() !== "" && all.length > 0}
          onClearFilter={() => setSearch("")}
          emptyAction={
            mayManage ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Define the first role
              </Button>
            ) : undefined
          }
          actions={
            mayManage
              ? (role) => (
                  <span className="flex justify-end gap-2">
                    <Button onClick={() => setEditing(role)}>Edit</Button>
                    {role.is_builtin ? (
                      // NOT a disabled button. A control that cannot be used
                      // and does not say why is exactly what step 5 forbids,
                      // and a disabled button is skipped by keyboard
                      // navigation — so a screen-reader user never reaches the
                      // explanation at all (`docs/UI-UX/15`, `docs/UI-UX/13`).
                      <span className="self-center text-small text-text-secondary">
                        Built-in — cannot be deleted
                      </span>
                    ) : (
                      <Button variant="danger-text" onClick={() => setDeleting(role)}>
                        Delete
                      </Button>
                    )}
                  </span>
                )
              : undefined
          }
        />
      </div>

      <RoleFormModal
        open={creating}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setCreating(false)}
      />

      <RoleFormModal
        open={editing !== null}
        orgId={orgId}
        projectId={projectId ?? null}
        existing={editing ?? undefined}
        onClose={() => setEditing(null)}
      />

      <DeleteRoleDialog
        role={deleting}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}

/**
 * The create and edit form.
 *
 * One component for both. They differ in exactly one field — whether the key
 * can be typed — and two components would be two places for the validation to
 * drift apart.
 */
function RoleFormModal({
  open,
  orgId,
  projectId,
  existing,
  onClose,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  existing?: Role;
  onClose: () => void;
}) {
  const editingExisting = existing !== undefined;

  const [key, setKey] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [permissions, setPermissions] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const queryClient = useQueryClient();

  // Seed the fields from the role being edited, keyed on its id so reopening
  // with a different role does not show the previous one's values.
  const [seeded, setSeeded] = useState<string | null>(null);
  if (open && editingExisting && seeded !== existing.id) {
    setSeeded(existing.id);
    setKey(existing.key);
    setDisplayName(existing.display_name);
    setPermissions(existing.permission_keys.join("\n"));
    setProblem(null);
  }
  if (!open && seeded !== null) setSeeded(null);

  function reset() {
    setKey("");
    setDisplayName("");
    setPermissions("");
    setProblem(null);
  }

  const save = useMutation({
    mutationFn: async () => {
      const permissionKeys = splitPermissions(permissions);

      if (editingExisting) {
        const { data, error } = await api.PATCH(
          "/v1/organizations/{org_id}/projects/{project_id}/roles/{role_id}",
          {
            params: {
              path: {
                org_id: orgId as string,
                project_id: projectId as string,
                role_id: existing.id,
              },
            },
            // `permission_keys` replaces the set entirely — the API is
            // explicit that a partial update of an array is ambiguous.
            body: { display_name: displayName.trim(), permission_keys: permissionKeys },
          },
        );
        if (error !== undefined) throw error;
        return data;
      }

      const { data, error } = await api.POST(
        "/v1/organizations/{org_id}/projects/{project_id}/roles",
        {
          params: { path: { org_id: orgId as string, project_id: projectId as string } },
          body: {
            key: key.trim(),
            display_name: displayName.trim(),
            permission_keys: permissionKeys,
          },
        },
      );
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.roles, orgId, projectId] });
      reset();
      onClose();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  // Validated with the SERVER's rules, from the generated patterns.
  const keyProblem = editingExisting ? null : roleKeyProblem(key);
  const nameProblem = displayName.trim() === "" ? "A name is required." : null;
  const permissionProblem = permissionsProblem(permissions);
  const blocked = keyProblem !== null || nameProblem !== null || permissionProblem !== null;

  function dismiss() {
    reset();
    onClose();
  }

  const keyInvalid = keyProblem !== null && key !== "";

  return (
    <Modal
      open={open}
      title={editingExisting ? `Edit ${existing.display_name}` : "Define a role"}
      onClose={dismiss}
      footer={
        <>
          <Button onClick={dismiss}>Cancel</Button>
          <Button
            variant="primary"
            loading={save.isPending}
            disabled={blocked}
            onClick={() => save.mutate()}
          >
            {editingExisting ? "Save changes" : "Define role"}
          </Button>
        </>
      }
    >
      <label htmlFor="role-key" className="block text-small font-medium text-text-secondary">
        Key
      </label>
      <input
        id="role-key"
        value={key}
        onChange={(event) => setKey(event.currentTarget.value)}
        readOnly={editingExisting}
        aria-invalid={keyInvalid || undefined}
        aria-describedby={keyInvalid ? "role-key-error" : "role-key-help"}
        maxLength={ROLE_KEY_MAX_LENGTH}
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-body text-text-primary"
      />
      {/*
        Helper text and error text never appear together — the error REPLACES
        the help rather than stacking under it (docs/UI-UX/07 § Form Field).

        `readOnly` rather than `disabled` when editing: a read-only input is
        still focusable and still announced, so somebody arriving by keyboard
        reaches the field and hears the sentence explaining why it cannot
        change. A disabled input is skipped in silence, which is the failure
        step 5 names.
      */}
      {keyInvalid ? (
        <p id="role-key-error" className="mt-1 text-small text-danger">
          {keyProblem}
        </p>
      ) : (
        <p id="role-key-help" className="mt-1 text-small text-text-secondary">
          {editingExisting
            ? "A role's key cannot be changed, by anyone. Grants reference it by name with nothing to cascade, so re-keying would silently remove access from everyone holding it — a rename that revokes."
            : "Lower-case letters, digits, underscores and hyphens. This is the name that appears inside a token."}
        </p>
      )}

      <label htmlFor="role-name" className="mt-4 block text-small font-medium text-text-secondary">
        Name
      </label>
      <input
        id="role-name"
        value={displayName}
        onChange={(event) => setDisplayName(event.currentTarget.value)}
        aria-describedby="role-name-help"
        maxLength={128}
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
      />
      <p id="role-name-help" className="mt-1 text-small text-text-secondary">
        What administrators see in this list. It can be changed at any time.
      </p>

      <label
        htmlFor="role-permissions"
        className="mt-4 block text-small font-medium text-text-secondary"
      >
        Permissions
      </label>
      <textarea
        id="role-permissions"
        rows={6}
        value={permissions}
        onChange={(event) => setPermissions(event.currentTarget.value)}
        aria-invalid={permissionProblem !== null || undefined}
        aria-describedby={
          permissionProblem !== null ? "role-permissions-error" : "role-permissions-help"
        }
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-small text-text-primary"
      />
      {permissionProblem !== null ? (
        <p id="role-permissions-error" className="mt-1 text-small text-danger">
          {permissionProblem}
        </p>
      ) : (
        <p id="role-permissions-help" className="mt-1 text-small text-text-secondary">
          One per line, as <code className="font-mono">resource:action</code> — for example{" "}
          <code className="font-mono">billing.invoice:read</code>. A role may have none; a role with
          no permissions is a label, and labels are useful before the permissions exist.
          {editingExisting ? " Saving replaces the whole set." : ""}
        </p>
      )}

      {problem !== null ? (
        <p role="alert" className="mt-3 text-small text-danger">
          {problem}
        </p>
      ) : null}
    </Modal>
  );
}

/**
 * Deletion, with the count the service would refuse on.
 *
 * The destructive weight lives here and nowhere else on this screen: the row's
 * own control is `danger-text`, because `docs/UI-UX/07` is explicit that a
 * destructive button must not be the visually dominant action on a screen
 * where a non-destructive one is the expected path. Full-strength
 * `color-danger` is reserved for this dialog, where it is the only action
 * offered (`docs/UI-UX/06`, `CLAUDE.md`).
 */
function DeleteRoleDialog({
  role,
  orgId,
  projectId,
  onClose,
}: {
  role: Role | null;
  orgId: string | null;
  projectId: string | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [problem, setProblem] = useState<string | null>(null);

  const remove = useMutation({
    mutationFn: async () => {
      const { error } = await api.DELETE(
        "/v1/organizations/{org_id}/projects/{project_id}/roles/{role_id}",
        {
          params: {
            path: {
              org_id: orgId as string,
              project_id: projectId as string,
              role_id: (role as Role).id,
            },
          },
        },
      );
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.roles, orgId, projectId] });
      setProblem(null);
      onClose();
    },
    onError: (error: unknown) => setProblem(messageFor(error)),
  });

  const assigned = role?.grant_count ?? 0;

  return (
    <ConfirmDialog
      open={role !== null}
      // The action, plainly — not "Are you sure?" (docs/UI-UX/07).
      title={`Delete ${role?.display_name ?? "this role"}?`}
      verb="Delete role"
      busy={remove.isPending}
      onCancel={() => {
        setProblem(null);
        onClose();
      }}
      onConfirm={() => remove.mutate()}
      consequence={
        <>
          <p>
            <code className="font-mono text-small">{role?.key}</code> will be removed from this
            project. Nothing recreates it.
          </p>
          {assigned > 0 ? (
            // The consequence BEFORE the confirmation, and the specific
            // consequence rather than "this cannot be undone". The service
            // refuses a referenced role rather than cascading — a cascade
            // would remove access from everybody holding it in response to a
            // request that looks like tidying up.
            <p className="mt-2">
              <strong className="font-medium">
                {assigned} user{assigned === 1 ? "" : "s"} currently hold this role.
              </strong>{" "}
              The service will refuse the deletion until those assignments are removed, so nothing
              is lost by trying.
            </p>
          ) : (
            <p className="mt-2">Nobody currently holds this role.</p>
          )}
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

// --- validation, from the server's own generated rules -----------------------

function roleKeyProblem(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed === "") return "A key is required.";
  if (trimmed.length > ROLE_KEY_MAX_LENGTH) {
    return `A key can be at most ${ROLE_KEY_MAX_LENGTH} characters.`;
  }
  if (!isValidRoleKey(trimmed)) {
    return "Use lower-case letters, digits, underscores and hyphens, starting with a letter or digit.";
  }
  return null;
}

/**
 * Splits on newlines and commas both.
 *
 * People paste comma-separated lists into a box that asks for one per line,
 * and treating `billing:read, billing:write` as a single key would produce a
 * refusal naming something nobody typed.
 */
function splitPermissions(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((entry) => entry.trim())
    .filter((entry) => entry !== "");
}

function permissionsProblem(value: string): string | null {
  const entries = splitPermissions(value);

  if (entries.length > 256) return "A role can carry at most 256 permissions.";

  const seen = new Set<string>();
  for (const entry of entries) {
    if (entry.length > PERMISSION_KEY_MAX_LENGTH) {
      return `“${entry.slice(0, 24)}…” is longer than ${PERMISSION_KEY_MAX_LENGTH} characters.`;
    }
    if (!isValidPermissionKey(entry)) {
      // Names the offending entry. "One of your permissions is invalid" makes
      // somebody read six lines looking for it.
      return `“${entry}” is not a permission key — use resource:action, for example billing:read.`;
    }
    if (seen.has(entry)) {
      // The service refuses duplicates rather than folding them, so the form
      // must too: a client that quietly deduplicated would send something
      // other than what was typed.
      return `“${entry}” is listed twice. Duplicates are refused rather than merged.`;
    }
    seen.add(entry);
  }
  return null;
}

// --- error shaping -----------------------------------------------------------

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

function messageFor(error: unknown): string {
  const envelope = error as { error?: { message?: string; details?: { issue?: string }[] } };
  // The server's per-field detail where there is one — a reserved key comes
  // back with a sentence explaining why, which beats "invalid".
  return (
    envelope?.error?.details?.[0]?.issue ??
    envelope?.error?.message ??
    "That could not be saved. Please try again."
  );
}

import { useId, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { TextAreaField, TextField } from "../components/FormField";
import { FormSummary, FormSystem, useExplicitForm } from "../components/FormSystem";
import { Modal } from "../components/Modal";
import { ProjectNav } from "../components/ProjectNav";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { CollectionStateBoundary } from "../components/states";
import { api } from "../lib/api/client";
import { invalidateQueries, mutationInvalidations } from "../lib/api/queryPolicy";
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
  return <RolesInProject key={JSON.stringify([orgId, projectId])} orgId={orgId} projectId={projectId ?? null} />;
}

function RolesInProject({ orgId, projectId }: { orgId: string | null; projectId: string | null }) {
  const projects = useProjects(orgId);
  const roles = useRoles(orgId, projectId ?? null);
  const { hasRole } = useAuth();

  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Role | null>(null);
  const [editOpen, setEditOpen] = useState(false);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [search, setSearch] = useState("");

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  const accessDenied = (roles.isError && kindOf(roles.error) === "permission") ||
    (projects.isError && kindOf(projects.error) === "permission");
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
  const mayManage = !accessDenied && hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

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
          {accessDenied ? "Project" : project?.name ?? "…"}
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

      {!accessDenied ? <div className="mt-4">
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
      </div> : null}

      <div className="mt-4">
        <CollectionStateBoundary
          count={rows.length}
          loading={roles.isPending}
          failed={roles.isError}
          permitted={!accessDenied}
          errorKind={kindOf(roles.error) === "network" ? "network" : "server"}
          onRetry={() => void roles.refetch()}
          what="roles"
          loadingLabel="Loading roles"
          skeleton={<Table<Role> caption="Roles in this project" columns={columns}
            rows={[]} rowKey={(role) => role.id} status="loading" what="roles" />}
          filtered={search.trim() !== ""}
          onClearFilter={() => setSearch("")}
          emptyAction={mayManage ? (
            <Button variant="primary" onClick={() => setCreating(true)}>Define the first role</Button>
          ) : undefined}
        >
        <Table<Role>
          caption={`Roles in ${project?.name ?? "this project"}`}
          columns={columns}
          rows={rows}
          rowKey={(role) => role.id}
          status="ready"
          what="roles"
          actions={
            mayManage
              ? (role) => (
                  <span className="flex justify-end gap-2">
                    <Button onClick={() => { setEditing(role); setEditOpen(true); }}>Edit</Button>
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
        </CollectionStateBoundary>
      </div>

      <RoleFormModal
        open={!accessDenied && creating}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setCreating(false)}
        onSaved={() => setCreating(false)}
      />

      <RoleFormModal
        // A save replaces every permission, so an unsaved draft is kept only
        // while the role it started from is unchanged.
        key={editing ? JSON.stringify([editing.id, editing.display_name, editing.permission_keys]) : "edit"}
        open={!accessDenied && editOpen}
        orgId={orgId}
        projectId={projectId ?? null}
        existing={editing ?? undefined}
        onClose={() => setEditOpen(false)}
        onSaved={() => { setEditOpen(false); setEditing(null); }}
      />

      <DeleteRoleDialog
        role={accessDenied ? null : deleting}
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
  onSaved,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  existing?: Role;
  onClose: () => void;
  onSaved: () => void;
}) {
  const editingExisting = existing !== undefined;
  const formId = useId();
  const queryClient = useQueryClient();

  const save = useMutation({
    mutationFn: async (values: { key: string; display_name: string; permission_keys: string }) => {
      const permissionKeys = splitPermissions(values.permission_keys);

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
            body: { display_name: values.display_name.trim(), permission_keys: permissionKeys },
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
            key: values.key.trim(),
            display_name: values.display_name.trim(),
            permission_keys: permissionKeys,
          },
        },
      );
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void invalidateQueries(queryClient, mutationInvalidations.roles(orgId, projectId)).catch(() => undefined);
      form.reset();
      onSaved();
    },
  });

  const form = useExplicitForm({
    initialValues: {
      key: existing?.key ?? "",
      display_name: existing?.display_name ?? "",
      permission_keys: existing?.permission_keys.join("\n") ?? "",
    },
    fields: {
      key: { label: "Key", validate: (key) => editingExisting ? undefined : roleKeyProblem(key) ?? undefined },
      display_name: { label: "Name", validate: (name) => name.trim() === "" ? "A name is required." : undefined },
      permission_keys: { label: "Permissions", validate: (permissions) => permissionsProblem(permissions) ?? undefined },
    },
    onSave: async (values) => { await save.mutateAsync(values); },
  });

  return (
    <Modal
      open={open}
      title={editingExisting ? `Edit ${existing.display_name}` : "Define a role"}
      onClose={onClose}
      dismissible={!form.saving}
      footer={
        <>
          <Button onClick={onClose} disabled={form.saving} disabledReason="Wait for role saving to finish.">Cancel</Button>
          <Button
            variant="primary"
            loading={form.saving}
            type="submit"
            form={formId}
          >
            {editingExisting ? "Save changes" : "Define role"}
          </Button>
        </>
      }
    >
      <FormSystem {...form.formProps} id={formId} pattern="explicit" className="space-y-4">
        <TextField
          label="Key"
          {...form.field("key")}
          readOnly={editingExisting}
          maxLength={ROLE_KEY_MAX_LENGTH}
          helperText={editingExisting
            ? "A role's key cannot be changed, by anyone. Grants reference it by name with nothing to cascade, so re-keying would silently remove access from everyone holding it — a rename that revokes."
            : "Lower-case letters, digits, underscores and hyphens. This is the name that appears inside a token."}
        />
        <TextField
          label="Name"
          {...form.field("display_name")}
          maxLength={128}
          helperText="What administrators see in this list. It can be changed at any time."
        />
        <TextAreaField
          label="Permissions"
          {...form.field("permission_keys")}
          rows={6}
          helperText={`One per line, as resource:action — for example billing.invoice:read. A role may have none; a role with no permissions is a label, and labels are useful before the permissions exist.${editingExisting ? " Saving replaces the whole set." : ""}`}
        />
        <FormSummary message={form.summary} />
      </FormSystem>
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
      void invalidateQueries(queryClient, mutationInvalidations.roles(orgId, projectId));
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
  if (error instanceof TypeError) return "network";
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

import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { Button } from "../components/Button";
import { TextField } from "../components/FormField";
import { FormSummary, FormSystem, useExplicitForm } from "../components/FormSystem";
import { Modal } from "../components/Modal";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { CollectionStateBoundary } from "../components/states";
import { api } from "../lib/api/client";
import { invalidateQueries, mutationInvalidations } from "../lib/api/queryPolicy";
import { useOrgId, useProjects } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Project = { id: string; name: string; created_at: string };

/**
 * The project list (P1-22, `docs/UI-UX/08`).
 *
 * It uses the shared `Table` rather than a table of its own, which is
 * `docs/UI-UX/08` § Cross-Screen Requirements' rule and the reason every state
 * below — loading, error, empty, filtered-empty — is handled without this
 * screen deciding what any of them look like.
 */
export function ProjectsPage() {
  const orgId = useOrgId();
  const projects = useProjects(orgId);
  const [search, setSearch] = useState("");
  const [creatingOrg, setCreatingOrg] = useState<string | null>(null);
  const { hasRole } = useAuth();

  const all = (projects.data ?? []) as Project[];
  const rows = all.filter((project) => project.name.toLowerCase().includes(search.toLowerCase()));
  const accessDenied = projects.isError && kindOf(projects.error) === "permission";
  const mayCreate = !accessDenied && hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<Project>[] = [
    {
      key: "name",
      header: "Name",
      cell: (project) => (
        <Link to={`/projects/${project.id}`} className="text-accent underline">
          {project.name}
        </Link>
      ),
    },
    {
      key: "created",
      header: "Created",
      // Dropped at tablet width: it is the least load-bearing column here, and
      // eight columns at 768px is a horizontal scrollbar nobody uses.
      secondary: true,
      cell: (project) => (
        <time dateTime={project.created_at}>{project.created_at.slice(0, 10)}</time>
      ),
    },
  ];

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Projects</h1>
        {/*
          The create action is hidden without the role — a UI affordance, and
          the API refuses regardless (docs/PLAN/08). Hiding it spares somebody
          filling in a form that will be rejected; it is not what stops them.
        */}
        {mayCreate ? (
          <Button variant="primary" onClick={() => setCreatingOrg(orgId)}>
            New project
          </Button>
        ) : null}
      </div>

      {!accessDenied ? <div className="mt-4">
        <label htmlFor="project-search" className="sr-only">
          Search projects
        </label>
        <input
          id="project-search"
          type="search"
          value={search}
          onChange={(event) => setSearch(event.currentTarget.value)}
          placeholder="Search projects"
          className="w-full max-w-sm rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
        />
      </div> : null}

      <div className="mt-4">
        <CollectionStateBoundary
          count={rows.length}
          loading={projects.isPending}
          failed={projects.isError}
          permitted={!accessDenied}
          errorKind={kindOf(projects.error) === "network" ? "network" : "server"}
          onRetry={() => void projects.refetch()}
          what="projects"
          loadingLabel="Loading projects"
          skeleton={<Table<Project> caption="Projects in this organization" columns={columns}
            rows={[]} rowKey={(project) => project.id} status="loading" what="projects"
            actions={() => null} />}
          filtered={search !== ""}
          onClearFilter={() => setSearch("")}
          emptyAction={
            mayCreate ? (
              <Button variant="primary" onClick={() => setCreatingOrg(orgId)}>
                Create the first project
              </Button>
            ) : undefined
          }
        >
        <Table<Project>
          caption="Projects in this organization"
          columns={columns}
          rows={rows}
          rowKey={(project) => project.id}
          status="ready"
          what="projects"
          actions={(project) => (
            <Link to={`/projects/${project.id}`} className="text-accent underline">
              Applications
            </Link>
          )}
        />
        </CollectionStateBoundary>
      </div>

      <CreateProjectModal
        key={orgId}
        open={!accessDenied && orgId !== null && creatingOrg === orgId}
        orgId={orgId}
        onClose={() => setCreatingOrg((current) => current === orgId ? null : current)}
        existingNames={accessDenied ? [] : all.map((project) => project.name.toLowerCase())}
      />
    </>
  );
}

function CreateProjectModal({
  open,
  orgId,
  onClose,
  existingNames,
}: {
  open: boolean;
  orgId: string | null;
  onClose: () => void;
  existingNames: string[];
}) {
  const formId = useId();
  const queryClient = useQueryClient();

  const create = useMutation({
    mutationFn: async ({ name }: { name: string }) => {
      const { data, error } = await api.POST("/v1/organizations/{org_id}/projects", {
        params: { path: { org_id: orgId as string } },
        body: { name: name.trim() },
      });
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      // A refresh failure must not turn an already-created project into a failed save.
      void invalidateQueries(queryClient, mutationInvalidations.projects(orgId)).catch(() => undefined);
      form.reset();
      onClose();
    },
  });

  // Checked before the request as well as after it. The API is the authority
  // — uniqueness is enforced there (P1-17) — and telling somebody before they
  // submit is a kindness the server cannot offer.
  const form = useExplicitForm({
    initialValues: { name: "" },
    fields: { name: { label: "Name", validate: (name) => name.trim() === "" ? "Enter a project name."
      : existingNames.includes(name.trim().toLowerCase()) ? "A project with that name already exists in this organization." : undefined } },
    onSave: async (values) => { await create.mutateAsync(values); },
  });

  return (
    <Modal
      open={open}
      title="New project"
      onClose={onClose}
      dismissible={!form.saving}
      footer={
        <>
          <Button onClick={onClose} disabled={form.saving} disabledReason="Wait for project creation to finish.">Cancel</Button>
          <Button
            variant="primary"
            loading={form.saving}
            type="submit"
            form={formId}
          >
            Create project
          </Button>
        </>
      }
    >
      <FormSystem {...form.formProps} id={formId} pattern="explicit" className="space-y-3">
        <TextField label="Name" {...form.field("name")} helperText="Unique within this organization, case-insensitively." />
        <FormSummary message={form.summary} />
      </FormSystem>
    </Modal>
  );
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

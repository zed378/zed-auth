import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { Button } from "../components/Button";
import { Modal } from "../components/Modal";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
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
  const [creating, setCreating] = useState(false);
  const { hasRole } = useAuth();

  const all = (projects.data ?? []) as Project[];
  const rows = all.filter((project) => project.name.toLowerCase().includes(search.toLowerCase()));

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
        {hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            New project
          </Button>
        ) : null}
      </div>

      <div className="mt-4">
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
      </div>

      <div className="mt-4">
        <Table<Project>
          caption="Projects in this organization"
          columns={columns}
          rows={rows}
          rowKey={(project) => project.id}
          status={projects.isPending ? "loading" : projects.isError ? "error" : "ready"}
          errorKind={kindOf(projects.error)}
          onRetry={() => void projects.refetch()}
          what="projects"
          // The distinction docs/UI-UX/14 asks for: there ARE projects and the
          // search matched none, versus there are none at all. Different copy,
          // different recovery.
          filtered={search !== "" && all.length > 0}
          onClearFilter={() => setSearch("")}
          emptyAction={
            hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Create the first project
              </Button>
            ) : undefined
          }
          actions={(project) => (
            <Link to={`/projects/${project.id}`} className="text-accent underline">
              Applications
            </Link>
          )}
        />
      </div>

      <CreateProjectModal
        open={creating}
        orgId={orgId}
        onClose={() => setCreating(false)}
        existingNames={all.map((project) => project.name.toLowerCase())}
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
  const [name, setName] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const create = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/organizations/{org_id}/projects", {
        params: { path: { org_id: orgId as string } },
        body: { name: name.trim() },
      });
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.projects, orgId] });
      setName("");
      setProblem(null);
      onClose();
    },
    onError: (error: unknown) => {
      const envelope = error as { error?: { code?: string; message?: string } };
      setProblem(envelope.error?.message ?? "That could not be saved.");
    },
  });

  // Checked before the request as well as after it. The API is the authority
  // — uniqueness is enforced there (P1-17) — and telling somebody before they
  // submit is a kindness the server cannot offer.
  const duplicate = existingNames.includes(name.trim().toLowerCase());
  const invalid = name.trim() === "" || duplicate;

  return (
    <Modal
      open={open}
      title="New project"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            loading={create.isPending}
            disabled={invalid}
            onClick={() => create.mutate()}
          >
            Create project
          </Button>
        </>
      }
    >
      <label htmlFor="project-name" className="block text-small font-medium text-text-secondary">
        Name
      </label>
      <input
        id="project-name"
        value={name}
        onChange={(event) => {
          setName(event.currentTarget.value);
          setProblem(null);
        }}
        aria-invalid={duplicate || undefined}
        aria-describedby={duplicate ? "project-name-error" : "project-name-help"}
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
      />
      {/*
        Helper text and error text never appear together — docs/UI-UX/07
        § Form Field. The error REPLACES the help rather than stacking under it.
      */}
      {duplicate ? (
        <p id="project-name-error" className="mt-1 text-small text-danger">
          A project with that name already exists in this organization.
        </p>
      ) : (
        <p id="project-name-help" className="mt-1 text-small text-text-secondary">
          Unique within this organization, case-insensitively.
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

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

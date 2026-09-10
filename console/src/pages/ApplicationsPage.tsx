import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ClientSecretModal } from "../components/ClientSecretModal";
import { Modal } from "../components/Modal";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import { useApplications, useOrgId, useProjects } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Application = {
  id: string;
  name: string;
  type: string;
  has_secret: boolean;
  redirect_uris: string[];
};

/**
 * The Applications tab (P1-22, `docs/UI-UX/08` § Applications tab).
 *
 * Its one hard requirement is the create flow: the client secret is shown
 * **once**, with a copy action and an unmistakable warning. That is not a UI
 * convention to be polite about — the service genuinely cannot show it again
 * (`P1-18`), so the dialog is the only moment it exists outside the service.
 */
export function ApplicationsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const orgId = useOrgId();
  const projects = useProjects(orgId);
  const applications = useApplications(orgId, projectId ?? null);
  const [creating, setCreating] = useState(false);
  const [issued, setIssued] = useState<{ name: string; secret: string } | null>(null);
  const { hasRole } = useAuth();

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  const rows = (applications.data ?? []) as Application[];

  const columns: Column<Application>[] = [
    { key: "name", header: "Name", cell: (app) => app.name },
    {
      key: "type",
      header: "Type",
      cell: (app) => <Badge tone="neutral">{app.type}</Badge>,
    },
    {
      key: "secret",
      header: "Secret",
      cell: (app) => (
        // Text, not a colour or an icon alone (docs/UI-UX/07 § Badge rule,
        // docs/UI-UX/13). "None" is not a problem — a public client is
        // supposed to have none — so it is muted rather than a warning.
        <Badge tone={app.has_secret ? "positive" : "muted"}>
          {app.has_secret ? "Configured" : "None (public client)"}
        </Badge>
      ),
    },
    {
      key: "redirects",
      header: "Redirect URIs",
      secondary: true,
      cell: (app) => (
        <span className="font-mono text-small text-text-secondary">
          {app.redirect_uris.length === 0 ? "—" : app.redirect_uris.join(", ")}
        </span>
      ),
    },
  ];

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-small text-text-secondary">
        {/* docs/UI-UX/07 § Breadcrumb: every segment clickable, reflecting the
            real data hierarchy. */}
        <Link to="/projects" className="underline">
          Projects
        </Link>{" "}
        / <span aria-current="page">{project?.name ?? "…"}</span>
      </nav>

      <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Applications</h1>
        {hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            Register application
          </Button>
        ) : null}
      </div>

      <div className="mt-4">
        <Table<Application>
          caption={`Applications in ${project?.name ?? "this project"}`}
          columns={columns}
          rows={rows}
          rowKey={(app) => app.id}
          status={applications.isPending ? "loading" : applications.isError ? "error" : "ready"}
          errorKind={kindOf(applications.error)}
          onRetry={() => void applications.refetch()}
          what="applications"
          filtered={false}
          emptyAction={
            hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Register the first application
              </Button>
            ) : undefined
          }
        />
      </div>

      <CreateApplicationModal
        open={creating}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setCreating(false)}
        onIssued={(name, secret) => {
          setCreating(false);
          // Only when there IS one. A public client gets none, and an empty
          // secret dialog would be a warning about nothing.
          if (secret !== undefined) setIssued({ name, secret });
        }}
      />

      <ClientSecretModal
        open={issued !== null}
        applicationName={issued?.name ?? ""}
        secret={issued?.secret ?? ""}
        onClose={() => setIssued(null)}
      />
    </>
  );
}

function CreateApplicationModal({
  open,
  orgId,
  projectId,
  onClose,
  onIssued,
}: {
  open: boolean;
  orgId: string | null;
  projectId: string | null;
  onClose: () => void;
  onIssued: (name: string, secret: string | undefined) => void;
}) {
  const [name, setName] = useState("");
  const [type, setType] = useState<"web" | "spa" | "native" | "api">("web");
  const [redirects, setRedirects] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const create = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST(
        "/v1/organizations/{org_id}/projects/{project_id}/applications",
        {
          params: { path: { org_id: orgId as string, project_id: projectId as string } },
          body: {
            name: name.trim(),
            type,
            redirect_uris: redirects
              .split("\n")
              .map((line) => line.trim())
              .filter((line) => line !== ""),
          },
        },
      );
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: (data) => {
      void queryClient.invalidateQueries({
        queryKey: [...queryKeys.applications, orgId, projectId],
      });
      onIssued(data.name, data.client_secret);
      setName("");
      setRedirects("");
      setProblem(null);
    },
    onError: (error: unknown) => {
      const envelope = error as { error?: { message?: string; details?: { issue?: string }[] } };
      // The server's per-field detail where there is one: a wildcard redirect
      // URI comes back with a sentence explaining why exact matching means a
      // wildcard would match nothing (P1-18), and that is more useful than
      // "invalid".
      setProblem(
        envelope.error?.details?.[0]?.issue ?? envelope.error?.message ?? "That could not be saved.",
      );
    },
  });

  const publicClient = type === "spa" || type === "native";

  return (
    <Modal
      open={open}
      title="Register an application"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            loading={create.isPending}
            disabled={name.trim() === ""}
            onClick={() => create.mutate()}
          >
            Register
          </Button>
        </>
      }
    >
      <label htmlFor="app-name" className="block text-small font-medium text-text-secondary">
        Name
      </label>
      <input
        id="app-name"
        value={name}
        onChange={(event) => setName(event.currentTarget.value)}
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
      />

      <label htmlFor="app-type" className="mt-4 block text-small font-medium text-text-secondary">
        Type
      </label>
      <select
        id="app-type"
        value={type}
        onChange={(event) => setType(event.currentTarget.value as typeof type)}
        aria-describedby="app-type-help"
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
      >
        <option value="web">Web — a server-side application</option>
        <option value="spa">Single-page app — runs in a browser</option>
        <option value="native">Native — a mobile or desktop app</option>
        <option value="api">API — a service with no user present</option>
      </select>
      <p id="app-type-help" className="mt-1 text-small text-text-secondary">
        {publicClient
          ? "A public client gets no secret: it cannot keep one confidential, so it uses PKCE instead."
          : "A confidential client is issued a secret, shown once when it is registered."}{" "}
        The type cannot be changed afterwards.
      </p>

      <label
        htmlFor="app-redirects"
        className="mt-4 block text-small font-medium text-text-secondary"
      >
        Redirect URIs
      </label>
      <textarea
        id="app-redirects"
        rows={3}
        value={redirects}
        onChange={(event) => setRedirects(event.currentTarget.value)}
        aria-describedby="app-redirects-help"
        className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-small text-text-primary"
      />
      <p id="app-redirects-help" className="mt-1 text-small text-text-secondary">
        One per line, matched by exact string comparison — a wildcard would be matched literally
        and never match anything.
      </p>

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

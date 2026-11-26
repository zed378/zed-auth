import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ClientSecretModal } from "../components/ClientSecretModal";
import { Modal } from "../components/Modal";
import { SelectField, TextAreaField, TextField } from "../components/FormField";
import { FormSummary, FormSystem, useExplicitForm } from "../components/FormSystem";
import {
  SamlRegistrationFields,
  draftFrom,
  emptySamlDraft,
  inputFrom,
  samlDraftValues,
  samlFieldBindings,
  samlFormFailure,
  samlFormRules,
  samlFormValues,
} from "../components/SamlRegistrationFields";
import type { SamlDraft, SamlRegistration } from "../components/SamlRegistrationFields";
import { ProjectNav } from "../components/ProjectNav";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { CollectionStateBoundary } from "../components/states";
import { api } from "../lib/api/client";
import { invalidateQueries, mutationInvalidations } from "../lib/api/queryPolicy";
import { useApplications, useOrgId, useProjects } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type Application = {
  id: string;
  name: string;
  type: string;
  has_secret: boolean;
  redirect_uris: string[];
  saml?: SamlRegistration;
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
  return <ApplicationsScope key={JSON.stringify([orgId, projectId])} orgId={orgId} projectId={projectId ?? null} />;
}

function ApplicationsScope({ orgId, projectId }: { orgId: string | null; projectId: string | null }) {
  const projects = useProjects(orgId);
  const applications = useApplications(orgId, projectId ?? null);
  const [creating, setCreating] = useState(false);
  const [issued, setIssued] = useState<{ name: string; secret: string } | null>(null);
  const [editingSaml, setEditingSaml] = useState<Application | null>(null);
  const { hasRole } = useAuth();

  const project = (projects.data ?? []).find((candidate) => candidate.id === projectId);
  const rows = (applications.data ?? []) as Application[];
  const accessDenied = (applications.isError && kindOf(applications.error) === "permission")
    || (projects.isError && kindOf(projects.error) === "permission");
  const canManage = !accessDenied && hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<Application>[] = [
    { key: "name", header: "Name", cell: (app) => app.name },
    {
      key: "client_id",
      header: "Client ID",
      // `docs/UI-UX/08` names this tab's purpose as "Register application,
      // **view `client_id`**/secret, set redirect URIs", and it was missing
      // until `P1-28` walked the acceptance criteria. An integrator's first
      // need from this screen is the value they paste into their own
      // configuration; without it the console could register an application
      // and then not tell anyone what it was called.
      //
      // Shown in full rather than truncated. A shortened id is one somebody
      // copies, pastes, and debugs for an hour — and `docs/PLAN/04` is
      // explicit that the id IS the client_id, so there is no second, shorter
      // identifier to show instead.
      cell: (app) => (
        <code className="font-mono text-small text-text-secondary">{app.id}</code>
      ),
    },
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
          {app.has_secret
            ? "Configured"
            : app.type === "saml"
              ? "None (SAML)"
              : "None (public client)"}
        </Badge>
      ),
    },
    {
      key: "redirects",
      // Where a sign-in ends, whichever protocol it is. For SAML that is the
      // registered ACS URL — the only address an assertion is ever sent to.
      header: "Where sign-in ends",
      secondary: true,
      cell: (app) => (
        <span className="font-mono text-small text-text-secondary">
          {app.saml !== undefined
            ? app.saml.acs_url
            : app.redirect_uris.length === 0
              ? "—"
              : app.redirect_uris.join(", ")}
        </span>
      ),
    },
    {
      key: "saml",
      header: "SAML",
      cell: (app) =>
        app.saml === undefined ? null : (
          <div className="flex flex-col items-start gap-1">
            <code className="font-mono text-small text-text-secondary">{app.saml.entity_id}</code>
            <CertificateStatus registration={app.saml} />
            {canManage ? (
              <Button onClick={() => setEditingSaml(app)}>SAML settings</Button>
            ) : null}
          </div>
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
        / <span aria-current="page">{accessDenied ? "Project" : project?.name ?? "…"}</span>
      </nav>

      <ProjectNav projectId={projectId ?? ""} />

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Applications</h1>
        {canManage ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            Register application
          </Button>
        ) : null}
      </div>

      <div className="mt-4">
        <CollectionStateBoundary
          count={rows.length}
          loading={applications.isPending}
          failed={applications.isError}
          permitted={!accessDenied}
          errorKind={kindOf(applications.error) === "network" ? "network" : "server"}
          onRetry={() => void applications.refetch()}
          what="applications"
          loadingLabel="Loading applications"
          skeleton={<Table<Application> caption={`Applications in ${project?.name ?? "this project"}`}
            columns={columns} rows={[]} rowKey={(app) => app.id} status="loading" what="applications" />}
          emptyAction={canManage ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              Register the first application
            </Button>
          ) : undefined}
        >
        <Table<Application>
          caption={`Applications in ${project?.name ?? "this project"}`}
          columns={columns}
          rows={rows}
          rowKey={(app) => app.id}
          status="ready"
          what="applications"
          filtered={false}
        />
        </CollectionStateBoundary>
      </div>

      <CreateApplicationModal
        open={canManage && creating}
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

      <EditSamlModal
        key={canManage ? "manage" : "readonly"}
        application={canManage ? editingSaml : null}
        orgId={orgId}
        projectId={projectId ?? null}
        onClose={() => setEditingSaml(null)}
      />

      <ClientSecretModal
        open={canManage && issued !== null}
        applicationName={canManage ? issued?.name ?? "" : ""}
        secret={canManage ? issued?.secret ?? "" : ""}
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
  const formId = useId();
  const [saml, setSaml] = useState<SamlDraft>(emptySamlDraft);
  const queryClient = useQueryClient();

  const create = useMutation({
    mutationFn: async ({ name, type, redirect_uris, registration }: { name: string; type: "web" | "spa" | "native" | "api" | "saml"; redirect_uris: string; registration: SamlDraft }) => {
      const { data, error } = await api.POST(
        "/v1/organizations/{org_id}/projects/{project_id}/applications",
        {
          params: { path: { org_id: orgId as string, project_id: projectId as string } },
          // A SAML application sends its registration and nothing OIDC; any
          // other type sends redirect URIs and no registration. The service
          // refuses the crossed combinations, so the console never builds one.
          body:
            type === "saml"
              ? { name: name.trim(), type, saml: inputFrom(registration) }
              : {
                  name: name.trim(),
                  type,
                  redirect_uris: redirect_uris
                    .split("\n")
                    .map((line) => line.trim())
                    .filter((line) => line !== ""),
                },
        },
      );
      if (error !== undefined) throw samlFormFailure(error, registration);
      return data;
    },
    onSuccess: (data) => {
      void invalidateQueries(queryClient, mutationInvalidations.applications(orgId, projectId)).catch(() => undefined);
      onIssued(data.name, data.client_secret);
      form.reset();
      setSaml(emptySamlDraft());
    },
  });

  const form = useExplicitForm({
    initialValues: { name: "", type: "web", redirect_uris: "", ...samlFormValues(emptySamlDraft()) },
    fields: {
      name: { label: "Name", validate: (value) => value.trim() ? undefined : "Enter an application name." },
      type: { label: "Type" },
      redirect_uris: { label: "Redirect URIs" },
      ...samlFormRules(saml, (values) => values.type === "saml"),
    },
    onSave: async (values) => {
      await create.mutateAsync({ ...values, type: values.type as "web" | "spa" | "native" | "api" | "saml", registration: samlDraftValues(saml, values) });
    },
  });
  const type = form.values.type;

  const publicClient = type === "spa" || type === "native";

  return (
    <Modal
      open={open}
      title="Register an application"
      onClose={onClose}
      dismissible={!form.saving}
      footer={
        <>
          <Button onClick={onClose} disabled={form.saving} disabledReason="Wait for registration to finish.">Cancel</Button>
          <Button
            variant="primary"
            loading={form.saving}
            type="submit"
            form={formId}
          >
            Register
          </Button>
        </>
      }
    >
      <FormSystem {...form.formProps} id={formId} pattern="explicit" className="space-y-3">
        <TextField label="Name" {...form.field("name")} />
        <SelectField label="Type" {...form.field("type")} helperText={type === "saml"
          ? "A SAML service provider gets no secret: it trusts this service by its signing certificate. The type cannot be changed afterwards."
          : publicClient
            ? "A public client gets no secret: it cannot keep one confidential, so it uses PKCE instead. The type cannot be changed afterwards."
            : "A confidential client is issued a secret, shown once when it is registered. The type cannot be changed afterwards."}>
          <option value="web">Web — a server-side application</option>
          <option value="spa">Single-page app — runs in a browser</option>
          <option value="native">Native — a mobile or desktop app</option>
          <option value="api">API — a service with no user present</option>
          <option value="saml">SAML — a service provider that signs in with SAML 2.0</option>
        </SelectField>

        {type === "saml" ? (
          <fieldset disabled={form.saving}><SamlRegistrationFields idPrefix="app-saml" draft={samlDraftValues(saml, form.values)} onChange={setSaml} fields={samlFieldBindings(form.field)} /></fieldset>
        ) : (
          <TextAreaField label="Redirect URIs" {...form.field("redirect_uris")} rows={3}
            helperText="One per line, matched by exact string comparison — a wildcard would be matched literally and never match anything." />
        )}

        <FormSummary message={form.summary} />
      </FormSystem>
    </Modal>
  );
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

/**
 * The service provider's certificate, and whether it is about to stop working.
 *
 * The warning comes from the server (`certificate_expires_soon`), which is the
 * one place the 30-day window is defined. Text carries it, not the colour: an
 * administrator reading a screenshot in greyscale gets the same date.
 */
function CertificateStatus({ registration }: { registration: SamlRegistration }) {
  if (registration.certificate_expires_at === undefined) {
    return <Badge tone="muted">No certificate</Badge>;
  }
  const date = new Date(registration.certificate_expires_at).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
  // No comparison against this browser's clock. The server decides whether the
  // certificate is inside the window — including already past it — and a second
  // opinion from a clock the administrator's laptop may have wrong would be a
  // console disagreeing with the service it describes.
  return registration.certificate_expires_soon === true ? (
    <Badge tone="attention">{`Certificate expiry ${date} — ask for a new one`}</Badge>
  ) : (
    <Badge tone="neutral">{`Certificate valid until ${date}`}</Badge>
  );
}

/**
 * Changing a registration — including the two switches P4-08 shipped with no
 * administrator in front of them.
 *
 * The whole registration is sent, not the fields that changed, because the
 * service replaces it whole: several fields constrain each other, and a caller
 * sending only a switch would be relying on values it has not looked at.
 */
function EditSamlModal({
  application,
  orgId,
  projectId,
  onClose,
}: {
  application: Application | null;
  orgId: string | null;
  projectId: string | null;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState(application);
  if (application !== null && selected?.id !== application.id) setSelected(application);
  if (selected?.saml === undefined) return null;
  return <EditSamlForm key={selected.id} application={selected} orgId={orgId} projectId={projectId}
    open={application !== null} onClose={onClose} onSaved={() => { setSelected(null); onClose(); }} />;
}

function EditSamlForm({ application, orgId, projectId, open, onClose, onSaved }: {
  application: Application;
  orgId: string | null;
  projectId: string | null;
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState(() => draftFrom(application.saml as SamlRegistration));
  const formId = useId();
  const queryClient = useQueryClient();

  const save = useMutation({
    mutationFn: async (registration: SamlDraft) => {
      const { error } = await api.PATCH(
        "/v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}",
        {
          params: {
            path: {
              org_id: orgId as string,
              project_id: projectId as string,
              application_id: application.id,
            },
          },
          body: { saml: inputFrom(registration) },
        },
      );
      if (error !== undefined) throw samlFormFailure(error, registration);
    },
    onSuccess: () => {
      void invalidateQueries(queryClient, mutationInvalidations.applications(orgId, projectId)).catch(() => undefined);
      onSaved();
    },
  });
  const form = useExplicitForm({
    initialValues: samlFormValues(draft),
    fields: samlFormRules(draft, () => true),
    onSave: async (values) => { await save.mutateAsync(samlDraftValues(draft, values)); },
  });

  return (
    <Modal
      open={open}
      title={`SAML settings — ${application.name}`}
      onClose={onClose}
      dismissible={!form.saving}
      footer={
        <>
          <Button onClick={onClose} disabled={form.saving} disabledReason="Wait for settings to finish saving.">Cancel</Button>
          <Button
            variant="primary"
            loading={form.saving}
            type="submit"
            form={formId}
          >
            Save
          </Button>
        </>
      }
    >
      <FormSystem {...form.formProps} id={formId} pattern="explicit" className="space-y-3">
      <fieldset disabled={form.saving}>
        <SamlRegistrationFields
          idPrefix="edit-saml"
          draft={samlDraftValues(draft, form.values)}
          onChange={setDraft}
          allowMetadata={false}
          fields={samlFieldBindings(form.field)}
        />
      </fieldset>
      <FormSummary message={form.summary} />
      </FormSystem>
    </Modal>
  );
}

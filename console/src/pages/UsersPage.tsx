import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";

import { Badge, statusTone } from "../components/Badge";
import { Button } from "../components/Button";
import { SidePanel } from "../components/SidePanel";
import { FormField } from "../components/FormField";
import { FormReview, FormSummary, FormSystem, useExplicitForm } from "../components/FormSystem";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { CollectionStateBoundary } from "../components/states";
import { api } from "../lib/api/client";
import { invalidateQueries, mutationInvalidations } from "../lib/api/queryPolicy";
import { useOrgId, useProjects, useUsers } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

type User = {
  id: string;
  email: string;
  display_name?: string | null;
  status: string;
  email_verified: boolean;
};

/**
 * The Users list (P1-23, `docs/UI-UX/18` § Users List).
 *
 * `docs/UI-UX/01`'s Budi persona spends more time here than anywhere else, so
 * the hierarchy is the specification's: search and filter always visible and
 * never behind a click, then the primary action, then the table.
 */
export function UsersPage() {
  const orgId = useOrgId();
  const [params, setParams] = useSearchParams();
  const status = params.get("status") ?? "";
  const [search, setSearch] = useState("");
  const users = useUsers(orgId, search);
  const [invitingOrg, setInvitingOrg] = useState<string | null>(null);
  const { hasRole } = useAuth();

  const all = (users.data?.items ?? []) as User[];
  const complete = users.data?.complete ?? true;
  const rows = status === "" ? all : all.filter((user) => user.status === status);
  const accessDenied = users.isError && kindOf(users.error) === "permission";
  const mayInvite = !accessDenied && hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER");

  const columns: Column<User>[] = [
    {
      key: "user",
      header: "User",
      cell: (user) => (
        <Link to={`/users/${user.id}`} className="text-accent underline">
          {user.display_name !== null && user.display_name !== undefined && user.display_name !== ""
            ? user.display_name
            : user.email}
        </Link>
      ),
    },
    { key: "email", header: "Email", cell: (user) => user.email, secondary: true },
    {
      key: "status",
      header: "Status",
      cell: (user) => <Badge tone={statusTone(user.status)}>{user.status}</Badge>,
    },
    {
      key: "verified",
      header: "Address",
      secondary: true,
      cell: (user) => (
        // "Not asserted" rather than "unverified". PG-18: absent means we have
        // not checked, not that we checked and it failed — and the console
        // should not make a claim the service deliberately does not.
        <span className="text-text-secondary">
          {user.email_verified ? "Verified" : "Not asserted"}
        </span>
      ),
    },
  ];

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-heading-1 font-medium text-text-primary">Users</h1>
        {mayInvite ? (
          <Button variant="primary" onClick={() => setInvitingOrg(orgId)}>
            Invite user
          </Button>
        ) : null}
      </div>

      {!accessDenied ? <div className="mt-4 flex flex-wrap gap-3">
        <div>
          <label htmlFor="user-search" className="sr-only">
            Search users
          </label>
          <input
            id="user-search"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.currentTarget.value)}
            placeholder="Search by name, email or username"
            className="w-72 rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
          />
        </div>

        <div>
          <label htmlFor="user-status" className="sr-only">
            Filter by status
          </label>
          <select
            id="user-status"
            value={status}
            onChange={(event) => {
              const next = event.currentTarget.value;
              // In the URL, so a filtered list is a link somebody can send —
              // and so the dashboard's "pending invites" card has somewhere to
              // point (docs/UI-UX/18).
              setParams(next === "" ? {} : { status: next }, { replace: true });
            }}
            className="rounded border border-border bg-bg-surface px-3 py-2 text-body text-text-primary"
          >
            <option value="">All statuses</option>
            <option value="active">Active</option>
            <option value="invited">Invited</option>
            <option value="locked">Locked</option>
            <option value="deactivated">Deactivated</option>
          </select>
        </div>
      </div> : null}

      {!accessDenied && !complete ? (
        // The list reached the bound on how many pages one read follows.
        // Saying so is the whole point: a silently shortened list is how the
        // newest users disappeared in the first place.
        <p role="status" className="mt-4 rounded border border-warning bg-bg-surface p-3 text-small text-text-primary">
          Showing the first {all.length.toLocaleString()} users, oldest first. Search by name or
          email to find anyone else{status !== "" ? "; the status filter applies to these only" : ""}.
        </p>
      ) : null}

      <div className="mt-4">
        <CollectionStateBoundary
          count={rows.length}
          loading={users.isPending}
          failed={users.isError}
          permitted={!accessDenied}
          errorKind={kindOf(users.error) === "network" ? "network" : "server"}
          onRetry={() => void users.refetch()}
          what="users"
          loadingLabel="Loading users"
          skeleton={<Table<User> caption="Users in this organization" columns={columns}
            rows={[]} rowKey={(user) => user.id} status="loading" what="users" />}
          // Either control counts as a filter. Someone who typed a search AND
          // picked a status needs "nothing matches", not "invite your first
          // user" (docs/UI-UX/14).
          filtered={search !== "" || status !== ""}
          onClearFilter={() => {
            setSearch("");
            setParams({}, { replace: true });
          }}
          emptyAction={
            mayInvite ? (
              <Button variant="primary" onClick={() => setInvitingOrg(orgId)}>
                Invite the first user
              </Button>
            ) : undefined
          }
        >
          <Table<User> caption="Users in this organization" columns={columns}
            rows={rows} rowKey={(user) => user.id} status="ready" what="users" />
        </CollectionStateBoundary>
      </div>

      <InvitePanel key={orgId} open={!accessDenied && orgId !== null && invitingOrg === orgId}
        orgId={orgId} onClose={() => setInvitingOrg((current) => current === orgId ? null : current)} />
    </>
  );
}

/**
 * Flow 1 from `docs/UI-UX/04`, as one flow in two steps.
 *
 * The safeguards are the specification's and both are implemented literally:
 *
 *   - **One flow, not two screens**, with a "Step 1 of 2" indicator so it does
 *     not feel like a single overloaded form.
 *   - **"No access yet" is an explicit, visible choice**, never "skip this
 *     step" — so an admin never accidentally believes they granted access when
 *     they did not.
 *
 * Role assignment itself is `P2`'s: `manager_roles` has no API in Phase 1. So
 * step 2 offers the explicit no-access choice and says plainly that roles
 * arrive later, rather than showing an empty picker that looks broken.
 */
function InvitePanel({
  open,
  orgId,
  onClose,
}: {
  open: boolean;
  orgId: string | null;
  onClose: () => void;
}) {
  const [step, setStep] = useState<1 | 2>(1);
  const [acknowledgedNoAccess, setAcknowledgedNoAccess] = useState(false);
  const queryClient = useQueryClient();
  const projects = useProjects(open ? orgId : null);

  const invite = useMutation({
    mutationFn: async (values: InviteValues) => {
      const { data, error } = await api.POST("/v1/organizations/{org_id}/users", {
        params: { path: { org_id: orgId as string } },
        body: {
          email: values.email.trim(),
          ...(values.display_name.trim() === "" ? {} : { display_name: values.display_name.trim() }),
          send_invite_email: true,
        },
      });
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void invalidateQueries(queryClient, mutationInvalidations.users(orgId));
      close();
    },
    onError: () => setStep(1),
  });

  const form = useExplicitForm<InviteValues>({
    initialValues: { email: "", display_name: "" },
    fields: {
      email: {
        label: "Email address",
        validate: (value) => value.trim() === ""
          ? "Enter an email address."
          : /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim())
            ? undefined
            : "Enter a valid email address.",
      },
      display_name: { label: "Display name" },
    },
    onSave: (values) => invite.mutateAsync(values).then(() => undefined),
  });

  function close() {
    setStep(1);
    form.reset();
    setAcknowledgedNoAccess(false);
    onClose();
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    if (step === 1) {
      event.preventDefault();
      if (form.validate(["email"])) setStep(2);
      return;
    }
    form.formProps.onSubmit(event);
  }

  const emailField = form.field("email");
  const displayNameField = form.field("display_name");

  return (
    <SidePanel
      open={open}
      title="Invite a user"
      progress={"Step " + step + " of 2"}
      onClose={close}
      footer={step === 1 ? (
        <>
          <Button type="button" onClick={close}>Cancel</Button>
          <Button variant="primary" type="submit" form="invite-user-form">Next</Button>
        </>
      ) : (
        <>
          <Button type="button" onClick={() => setStep(1)}>Back</Button>
          <Button variant="primary" type="submit" form="invite-user-form" loading={form.saving}
            disabled={!acknowledgedNoAccess}
            disabledReason="Confirm the invitation will grant no project access.">Send invitation</Button>
        </>
      )}
    >
      <FormSystem {...form.formProps} id="invite-user-form" pattern="explicit" onSubmit={submit}>
        <FormSummary message={form.summary} />
        {step === 1 ? (
          <div className="space-y-4">
            <FormField id="invite-email" label="Email address" error={emailField.error}
              helperText="The invitation goes here. They choose their own password through a link in it.">
              {({ id, describedBy }) => <input id={id} name={emailField.name} type="email"
                value={emailField.value} onChange={emailField.onChange} onBlur={emailField.onBlur}
                disabled={emailField.disabled} aria-invalid={Boolean(emailField.error)} aria-describedby={describedBy}
                className="min-h-11 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60" />}
            </FormField>
            <FormField id="invite-name" label="Display name (optional)"
              error={displayNameField.error}>
              {({ id, describedBy }) => <input id={id} name={displayNameField.name} type="text"
                value={displayNameField.value} onChange={displayNameField.onChange} onBlur={displayNameField.onBlur}
                disabled={displayNameField.disabled} aria-invalid={Boolean(displayNameField.error)} aria-describedby={describedBy}
                className="min-h-11 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60" />}
            </FormField>
          </div>
        ) : (
          <>
            <FormReview entries={[
              { label: "Email address", value: form.values.email.trim() },
              { label: "Display name", value: form.values.display_name.trim() },
            ]} />
            <p className="mt-4 text-body text-text-secondary">
              {form.values.email.trim()} will be able to sign in once they accept the invitation.
            </p>
            <div className="mt-4 rounded border border-border bg-bg-base p-3">
              <p className="text-body text-text-primary">
                Assigning roles to a user arrives in a later phase. Right now the only available
                choice is to invite them with no access to any project.
              </p>
              {projects.data !== undefined && projects.data.length > 0 ? (
                <p className="mt-2 text-small text-text-secondary">
                  This organization has {projects.data.length} project
                  {projects.data.length === 1 ? "" : "s"}. They will have access to none of them.
                </p>
              ) : null}
            </div>
            <label className="mt-4 flex items-start gap-2 text-body text-text-primary">
              <input type="checkbox" checked={acknowledgedNoAccess}
                onChange={(event) => setAcknowledgedNoAccess(event.currentTarget.checked)} className="mt-1" />
              <span>Invite with no access yet. I will grant access separately.</span>
            </label>
          </>
        )}
      </FormSystem>
    </SidePanel>
  );
}

type InviteValues = { email: string; display_name: string };

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  if (error instanceof TypeError) return "network";
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

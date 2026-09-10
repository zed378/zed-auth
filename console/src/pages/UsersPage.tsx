import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";

import { Badge, statusTone } from "../components/Badge";
import { Button } from "../components/Button";
import { SidePanel } from "../components/SidePanel";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
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
  const [inviting, setInviting] = useState(false);
  const { hasRole } = useAuth();

  const all = (users.data ?? []) as User[];
  const rows = status === "" ? all : all.filter((user) => user.status === status);

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
        {hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
          <Button variant="primary" onClick={() => setInviting(true)}>
            Invite user
          </Button>
        ) : null}
      </div>

      <div className="mt-4 flex flex-wrap gap-3">
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
      </div>

      <div className="mt-4">
        <Table<User>
          caption="Users in this organization"
          columns={columns}
          rows={rows}
          rowKey={(user) => user.id}
          status={users.isPending ? "loading" : users.isError ? "error" : "ready"}
          errorKind={kindOf(users.error)}
          onRetry={() => void users.refetch()}
          what="users"
          // Either control counts as a filter. Someone who typed a search AND
          // picked a status needs "nothing matches", not "invite your first
          // user" (docs/UI-UX/14).
          filtered={(search !== "" || status !== "") && all.length > 0}
          onClearFilter={() => {
            setSearch("");
            setParams({}, { replace: true });
          }}
          emptyAction={
            hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") ? (
              <Button variant="primary" onClick={() => setInviting(true)}>
                Invite the first user
              </Button>
            ) : undefined
          }
        />
      </div>

      <InvitePanel open={inviting} orgId={orgId} onClose={() => setInviting(false)} />
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
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [acknowledgedNoAccess, setAcknowledgedNoAccess] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const projects = useProjects(orgId);

  const close = () => {
    setStep(1);
    setEmail("");
    setDisplayName("");
    setAcknowledgedNoAccess(false);
    setFieldErrors({});
    setProblem(null);
    onClose();
  };

  const invite = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/organizations/{org_id}/users", {
        params: { path: { org_id: orgId as string } },
        body: {
          email: email.trim(),
          ...(displayName.trim() === "" ? {} : { display_name: displayName.trim() }),
          send_invite_email: true,
        },
      });
      if (error !== undefined) throw error;
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.users, orgId] });
      close();
    },
    onError: (error: unknown) => {
      const envelope = error as {
        error?: { message?: string; details?: { field?: string; issue?: string }[] };
      };
      // **Field-level errors against the right fields** (docs/UI-UX/15,
      // docs/PLAN/05's details[]). An envelope's per-field detail rendered as
      // one banner at the top is a form the user has to re-read to fix.
      const byField: Record<string, string> = {};
      for (const detail of envelope.error?.details ?? []) {
        if (detail.field !== undefined && detail.issue !== undefined) {
          byField[detail.field] = detail.issue;
        }
      }
      setFieldErrors(byField);
      setProblem(Object.keys(byField).length > 0 ? null : (envelope.error?.message ?? "That could not be sent."));
      setStep(1);
    },
  });

  return (
    <SidePanel
      open={open}
      title="Invite a user"
      progress={`Step ${step} of 2`}
      onClose={close}
      footer={
        step === 1 ? (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" disabled={email.trim() === ""} onClick={() => setStep(2)}>
              Next
            </Button>
          </>
        ) : (
          <>
            <Button onClick={() => setStep(1)}>Back</Button>
            <Button
              variant="primary"
              loading={invite.isPending}
              disabled={!acknowledgedNoAccess}
              onClick={() => invite.mutate()}
            >
              Send invitation
            </Button>
          </>
        )
      }
    >
      {step === 1 ? (
        <>
          <label htmlFor="invite-email" className="block text-small font-medium text-text-secondary">
            Email address
          </label>
          <input
            id="invite-email"
            type="email"
            value={email}
            onChange={(event) => {
              setEmail(event.currentTarget.value);
              setFieldErrors((current) => ({ ...current, email: "" }));
            }}
            aria-invalid={fieldErrors.email ? true : undefined}
            aria-describedby={fieldErrors.email ? "invite-email-error" : "invite-email-help"}
            className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
          />
          {fieldErrors.email ? (
            <p id="invite-email-error" className="mt-1 text-small text-danger">
              {fieldErrors.email}
            </p>
          ) : (
            <p id="invite-email-help" className="mt-1 text-small text-text-secondary">
              The invitation goes here. They choose their own password through a link in it.
            </p>
          )}

          <label
            htmlFor="invite-name"
            className="mt-4 block text-small font-medium text-text-secondary"
          >
            Display name <span className="font-normal">(optional)</span>
          </label>
          <input
            id="invite-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.currentTarget.value)}
            className="mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary"
          />

          {problem !== null ? (
            <p role="alert" className="mt-3 text-small text-danger">
              {problem}
            </p>
          ) : null}
        </>
      ) : (
        <>
          <h3 className="text-heading-3 font-medium text-text-primary">Access</h3>
          <p className="mt-1 text-body text-text-secondary">
            {email} will be able to sign in once they accept the invitation.
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

          {/*
            An explicit, visible choice — never "skip this step". docs/UI-UX/04
            Flow 1's safeguard exists so an admin never walks away believing
            they granted access when they did not.
          */}
          <label className="mt-4 flex items-start gap-2 text-body text-text-primary">
            <input
              type="checkbox"
              checked={acknowledgedNoAccess}
              onChange={(event) => setAcknowledgedNoAccess(event.currentTarget.checked)}
              className="mt-1"
            />
            <span>Invite with no access yet. I will grant access separately.</span>
          </label>

          {problem !== null ? (
            <p role="alert" className="mt-3 text-small text-danger">
              {problem}
            </p>
          ) : null}
        </>
      )}
    </SidePanel>
  );
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

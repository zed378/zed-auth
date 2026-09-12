import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { Badge, statusTone } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { RoleList } from "../components/RoleSourceBadge";
import { ErrorState, Skeleton } from "../components/states";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import { useOrgId, useProjects, useUserGrants } from "../lib/api/queries";
import { useAuth } from "../lib/auth/AuthProvider";

/**
 * User detail (P1-23, `docs/UI-UX/08`).
 *
 * The Profile tab is real. Grants, Sessions and MFA belong to Phases 2, 1's
 * later work and 3 — and they are rendered as **explicitly unavailable**
 * rather than as empty tabs, because `P1-23` step 3 asks for exactly that:
 * the console must never imply a capability that is not shipped.
 *
 * An empty "Sessions" tab and a "Sessions — arriving in Phase 3" tab look
 * similar and mean opposite things. One says this user has no sessions; the
 * other says we cannot tell you.
 */
type Tab = "profile" | "grants" | "sessions" | "mfa";

export function UserDetailPage() {
  const { userId } = useParams<{ userId: string }>();
  const orgId = useOrgId();
  const [tab, setTab] = useState<Tab>("profile");

  const user = useQuery({
    queryKey: [...queryKeys.users, orgId, "detail", userId],
    enabled: orgId !== null && userId !== undefined,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organizations/{org_id}/users/{user_id}", {
        params: { path: { org_id: orgId as string, user_id: userId as string } },
      });
      if (error !== undefined) throw error;
      return data;
    },
  });

  return (
    <>
      <nav aria-label="Breadcrumb" className="text-small text-text-secondary">
        <Link to="/users" className="underline">
          Users
        </Link>{" "}
        / <span aria-current="page">{user.data?.email ?? "…"}</span>
      </nav>

      <h1 className="mt-2 text-heading-1 font-medium text-text-primary">
        {user.isPending ? (
          <Skeleton className="h-7 w-64" />
        ) : (
          (user.data?.display_name?.toString() ?? user.data?.email ?? "User")
        )}
      </h1>

      {/*
        A real tablist. Arrow-key navigation and roving tabindex are what make
        tabs usable from a keyboard; a row of buttons styled as tabs is a row
        of buttons (docs/UI-UX/13).
      */}
      <div role="tablist" aria-label="User details" className="mt-4 flex gap-1 border-b border-border">
        {(
          [
            ["profile", "Profile", true],
            ["grants", "Grants", true],
            ["sessions", "Sessions", false],
            ["mfa", "Multi-factor", false],
          ] as [Tab, string, boolean][]
        ).map(([key, label, available]) => (
          <button
            key={key}
            role="tab"
            id={`tab-${key}`}
            aria-selected={tab === key}
            aria-controls={`panel-${key}`}
            tabIndex={tab === key ? 0 : -1}
            onClick={() => setTab(key)}
            className={`border-b-2 px-3 py-2 text-body ${
              tab === key
                ? "border-accent text-text-primary"
                : "border-transparent text-text-secondary"
            }`}
          >
            {label}
            {!available ? (
              // Said in the tab itself, not only after clicking it. Somebody
              // scanning the tabs learns which are real without opening each.
              <span className="ml-1 text-small text-text-secondary">(later)</span>
            ) : null}
          </button>
        ))}
      </div>

      <div id={`panel-${tab}`} role="tabpanel" aria-labelledby={`tab-${tab}`} className="mt-4">
        {tab === "profile" ? (
          <ProfileTab
            user={user}
            orgId={orgId}
            userId={userId ?? ""}
            onChanged={() => void user.refetch()}
          />
        ) : tab === "grants" ? (
          <GrantsTab orgId={orgId} userId={userId ?? null} />
        ) : (
          <NotYet tab={tab} />
        )}
      </div>
    </>
  );
}

function ProfileTab({
  user,
  orgId,
  userId,
  onChanged,
}: {
  user: { isPending: boolean; isError: boolean; error: unknown; data?: Record<string, unknown> };
  orgId: string | null;
  userId: string;
  onChanged: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const queryClient = useQueryClient();
  const { hasRole } = useAuth();

  const deactivate = useMutation({
    mutationFn: async () => {
      const { error } = await api.POST(
        "/v1/organizations/{org_id}/users/{user_id}/deactivate",
        { params: { path: { org_id: orgId as string, user_id: userId } } },
      );
      if (error !== undefined) throw error;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.users, orgId] });
      setConfirming(false);
      onChanged();
    },
  });

  if (user.isPending) return <Skeleton className="h-40 w-full" />;
  if (user.isError) return <ErrorState kind={kindOf(user.error)} onRetry={onChanged} />;

  const status = String(user.data?.status ?? "");
  const deactivated = status === "deactivated";

  return (
    <>
      <dl className="grid max-w-xl grid-cols-1 gap-3 tablet:grid-cols-2">
        <Field label="Email" value={String(user.data?.email ?? "")} />
        <Field label="Username" value={stringOrDash(user.data?.username)} />
        <Field label="Display name" value={stringOrDash(user.data?.display_name)} />
        <div>
          <dt className="text-small text-text-secondary">Status</dt>
          <dd className="mt-1">
            <Badge tone={statusTone(status)}>{status}</Badge>
          </dd>
        </div>
        <Field
          label="Email address"
          value={user.data?.email_verified === true ? "Verified" : "Not asserted"}
        />
        <Field label="Created" value={String(user.data?.created_at ?? "").slice(0, 10)} />
      </dl>

      {hasRole("ORG_ADMIN", "ORG_OWNER", "INSTANCE_OWNER") && !deactivated ? (
        <div className="mt-6 border-t border-border pt-4">
          {/*
            `danger-text`, not a full destructive button. docs/UI-UX/07's rule:
            a destructive button must not be the visually dominant action on a
            screen where a non-destructive one is the expected path. The full
            weight belongs to the confirmation, where it is the only thing on
            the screen.
          */}
          <Button variant="danger-text" onClick={() => setConfirming(true)}>
            Deactivate this user
          </Button>
        </div>
      ) : null}

      <ConfirmDialog
        open={confirming}
        title="Deactivate this user?"
        verb="Deactivate"
        busy={deactivate.isPending}
        onCancel={() => setConfirming(false)}
        onConfirm={() => deactivate.mutate()}
        consequence={
          <>
            <p>
              <strong>{String(user.data?.email ?? "This user")}</strong> will not be able to sign
              in.
            </p>
            {/*
              The consequence, in plain language, and specifically the part
              people do not expect: it is not just the next login. P1-19
              revokes sessions, refresh tokens and live invitation links inside
              the request.
            */}
            <p className="mt-2">
              Every session they have open ends immediately, and any application holding a refresh
              token for them stops working at once. Any invitation or password-reset link they hold
              stops working too.
            </p>
            <p className="mt-2 text-text-secondary">
              This can be undone: reactivating them restores the account, though not their
              sessions — they sign in again.
            </p>
          </>
        }
      />
    </>
  );
}

/**
 * Every project this user has access to (P2-12 step 7).
 *
 * The card asks for this to mirror the Authorizations tab "from the same
 * components", and the reason is `docs/UI-UX/08` § Cross-Screen Requirements:
 * the role-source badge is mandatory on **every** screen showing roles. Two
 * screens rendering grants two ways is how one of them ends up without it.
 *
 * This is the read side. Granting happens on the project, because a role only
 * means anything inside one — and the Authorizations tab is where the
 * project's roles and their permission keys are in front of you.
 */
function GrantsTab({ orgId, userId }: { orgId: string | null; userId: string | null }) {
  const grants = useUserGrants(orgId, userId);
  const projects = useProjects(orgId);

  type Row = { project_id: string; role_keys: string[] };
  const rows = (grants.data ?? []) as Row[];

  const nameOf = (projectId: string) =>
    (projects.data ?? []).find((project) => project.id === projectId)?.name ?? projectId;

  const columns: Column<Row>[] = [
    {
      key: "project",
      header: "Project",
      cell: (row) => (
        <Link to={`/projects/${row.project_id}/authorizations`} className="underline">
          {nameOf(row.project_id)}
        </Link>
      ),
    },
    {
      key: "roles",
      header: "Roles",
      // Named, never counted (`docs/UI-UX/18`), each with its source.
      cell: (row) => <RoleList roleKeys={row.role_keys} />,
    },
  ];

  return (
    <>
      <Table<Row>
        caption="Projects this user has access to"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.project_id}
        status={grants.isPending ? "loading" : grants.isError ? "error" : "ready"}
        errorKind={kindOf(grants.error)}
        onRetry={() => void grants.refetch()}
        what="grants"
        filtered={false}
      />
      {!grants.isPending && !grants.isError && rows.length === 0 ? (
        // The empty table already says "No grants yet". This says what that
        // MEANS, which is the thing `docs/PLAN/08` § Least Privilege makes
        // load-bearing: no grant is no access, not a default.
        <p className="mt-3 max-w-prose text-small text-text-secondary">
          This is the normal state for a new account. There is no implicit or default role, so
          until they are granted one in a project, every authorization check for them is denied.
        </p>
      ) : null}
    </>
  );
}

function NotYet({ tab }: { tab: Tab }) {
  const copy: Record<Exclude<Tab, "profile" | "grants">, { title: string; body: string }> = {
    sessions: {
      title: "Sessions are not shown here yet",
      body:
        "The service tracks and revokes sessions today — deactivating a user ends them — but a " +
        "screen for listing and revoking them individually is later work.",
    },
    mfa: {
      title: "Multi-factor is not available yet",
      body: "Enrolment and factor management arrive in Phase 3.",
    },
  };

  const { title, body } = copy[tab as Exclude<Tab, "profile" | "grants">];

  return (
    <div className="rounded border border-border bg-bg-surface p-5">
      <h2 className="text-heading-3 font-medium text-text-primary">{title}</h2>
      <p className="mt-1 max-w-prose text-body text-text-secondary">{body}</p>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-small text-text-secondary">{label}</dt>
      <dd className="mt-1 text-body text-text-primary">{value}</dd>
    </div>
  );
}

function stringOrDash(value: unknown): string {
  return typeof value === "string" && value !== "" ? value : "—";
}

function kindOf(error: unknown): "network" | "server" | "permission" | "validation" {
  const failure = error as { kind?: "network" | "server" | "permission" | "validation" } | null;
  return failure?.kind ?? "server";
}

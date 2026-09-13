import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorState, Skeleton } from "../components/states";
import { Table } from "../components/Table";
import type { Column } from "../components/Table";
import { api, queryKeys } from "../lib/api/client";
import { ApiFailure, asFailure } from "../lib/api/queries";
import type { components } from "../lib/api/schema.gen";
import { useAuth } from "../lib/auth/AuthProvider";

type Session = components["schemas"]["Session"];

/**
 * The Sessions tab on User detail (P3-11, `docs/UI-UX/04` Flow 4).
 *
 * Built to `docs/UI-UX/19`'s worked example, which specifies the revoke button
 * down to its screen-reader label:
 *
 * | Step | This implementation |
 * |---|---|
 * | Design | A secondary button per row |
 * | Component | `Button` (secondary) inside a `Table` row |
 * | State | default · revoking (spinner) · removed · error |
 * | Interaction | One click, no modal; the row disappears at once and comes back if the API refuses |
 * | API | `DELETE /v1/me/sessions/{id}` for oneself, the organization route for a member |
 * | Loading | The button's spinner keeps its width |
 * | Error | Inline, in the row that came back |
 * | Empty | "No other active sessions" for oneself; "no active sessions" for a member |
 * | Permission | Oneself, or `ORG_ADMIN`/`ORG_OWNER` for a member — and the API decides |
 * | Responsive | One tap target per row at every width |
 * | Accessibility | "Revoke session on Chrome on Windows", never a bare "Revoke" |
 * | Test | Component test for the rollback; E2E for Flow 4 |
 *
 * **One exception to "no modal", and it is the current session.** Revoking it
 * signs the person out of the console they are using, which is not the
 * low-risk action the example describes — so that row asks first.
 */
export function SessionsTab({ orgId, userId }: { orgId: string | null; userId: string }) {
  const { claims, logout } = useAuth();
  const self = claims !== null && claims.subject === userId;
  const queryClient = useQueryClient();
  const key = [...queryKeys.sessions, orgId, self ? "me" : userId];

  const [failures, setFailures] = useState<Record<string, string>>({});
  const [confirmingCurrent, setConfirmingCurrent] = useState<Session | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const sessions = useQuery({
    queryKey: key,
    enabled: orgId !== null,
    queryFn: async () => {
      const { data, error } = self
        ? await api.GET("/v1/me/sessions", { params: { query: { page_size: 100 } } })
        : await api.GET("/v1/organizations/{org_id}/users/{user_id}/sessions", {
            params: { path: { org_id: orgId as string, user_id: userId }, query: { page_size: 100 } },
          });
      if (error !== undefined) throw asFailure(error);
      return data.sessions;
    },
  });

  const revoke = useMutation({
    mutationFn: async (session: Session) => {
      const { error } = self
        ? await api.DELETE("/v1/me/sessions/{session_id}", {
            params: { path: { session_id: session.id } },
          })
        : await api.DELETE("/v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}", {
            params: { path: { org_id: orgId as string, user_id: userId, session_id: session.id } },
          });
      if (error !== undefined) throw asFailure(error);
    },
    // Optimistic: the row goes at once. `docs/UI-UX/04` Flow 4 asks for this to
    // FEEL instantaneous at the moment somebody is worried about their account.
    onMutate: async (session) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<Session[]>(key);
      queryClient.setQueryData<Session[]>(key, (rows) => (rows ?? []).filter((row) => row.id !== session.id));
      setFailures((current) => {
        const next = { ...current };
        delete next[session.id];
        return next;
      });
      return { previous };
    },
    // And it comes back if the API refused, with the reason in its own row.
    onError: (error, session, context) => {
      if (context?.previous !== undefined) queryClient.setQueryData(key, context.previous);
      setFailures((current) => ({
        ...current,
        [session.id]: error instanceof ApiFailure ? error.message : "The session could not be revoked.",
      }));
    },
    onSuccess: (_data, session) => {
      if (session.current) {
        // The token this console holds was issued through it, and is now dead.
        logout();
      }
    },
    onSettled: () => void queryClient.invalidateQueries({ queryKey: key }),
  });

  const revokeOthers = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/sessions/revoke-others", {});
      if (error !== undefined) throw asFailure(error);
      return data.revoked;
    },
    onSuccess: (count) => {
      setNotice(count === 1 ? "1 other session was signed out." : `${count} other sessions were signed out.`);
      void queryClient.invalidateQueries({ queryKey: key });
    },
    onError: (error) => {
      setNotice(error instanceof ApiFailure ? error.message : "The other sessions could not be signed out.");
    },
  });

  if (sessions.isPending) return <Skeleton className="h-40 w-full" />;
  if (sessions.isError) {
    return (
      <ErrorState
        kind={sessions.error instanceof ApiFailure ? sessions.error.kind : "server"}
        onRetry={() => void sessions.refetch()}
      />
    );
  }

  const rows = sessions.data;
  const others = rows.filter((row) => !row.current);

  const columns: Column<Session>[] = [
    {
      key: "device",
      header: "Device",
      cell: (row) => (
        <span className="inline-flex flex-wrap items-center gap-2">
          {deviceOf(row)}
          {row.current ? <Badge tone="positive">This session</Badge> : null}
        </span>
      ),
    },
    { key: "location", header: "Location", cell: (row) => row.location ?? "Unknown", secondary: true },
    { key: "created", header: "Signed in", cell: (row) => when(row.created_at), secondary: true },
    { key: "active", header: "Last active", cell: (row) => when(row.last_active_at) },
  ];

  return (
    <>
      {self ? (
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <Button
            variant="primary"
            loading={revokeOthers.isPending}
            disabled={others.length === 0}
            onClick={() => {
              setNotice(null);
              revokeOthers.mutate();
            }}
          >
            Revoke all other sessions
          </Button>
          {notice !== null ? (
            <p role="status" className="text-body text-text-secondary">
              {notice}
            </p>
          ) : null}
        </div>
      ) : null}

      <Table<Session>
        caption={self ? "Where you are signed in" : "Where this user is signed in"}
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        status="ready"
        what="active sessions"
        filtered={false}
        actions={(row) => (
          <span className="inline-flex flex-wrap items-center gap-2">
            <Button
              aria-label={`Revoke session on ${deviceOf(row)}`}
              loading={revoke.isPending && revoke.variables?.id === row.id}
              onClick={() => (row.current ? setConfirmingCurrent(row) : revoke.mutate(row))}
            >
              Revoke
            </Button>
            {failures[row.id] !== undefined ? (
              <span role="alert" className="text-small text-text-primary">
                {failures[row.id]}
              </span>
            ) : null}
          </span>
        )}
      />

      {self && others.length === 0 ? (
        // Not "no sessions": the one you are using always exists (card step 6).
        <p className="mt-3 text-body text-text-secondary">No other active sessions.</p>
      ) : null}

      <ConfirmDialog
        open={confirmingCurrent !== null}
        title="Sign out of this session?"
        verb="Sign out"
        onCancel={() => setConfirmingCurrent(null)}
        onConfirm={() => {
          if (confirmingCurrent !== null) revoke.mutate(confirmingCurrent);
          setConfirmingCurrent(null);
        }}
        consequence={<p>This is the session you are using now. You will be signed out of the console.</p>}
      />
    </>
  );
}

function deviceOf(session: Session): string {
  const browser = session.device.browser;
  const os = session.device.os;
  if (browser && os) return `${browser} on ${os}`;
  return browser ?? os ?? "Unknown device";
}

function when(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? iso : at.toLocaleString();
}

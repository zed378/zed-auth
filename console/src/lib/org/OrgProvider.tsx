import { createContext, useCallback, useContext, useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";

import { useAuth } from "../auth/AuthProvider";

/**
 * Which organization the console is acting in (P2-13).
 *
 * Until this task there was one answer and it came from the token: `org_id` is
 * the organization that owns the OIDC client the console authenticated to
 * (ADR-023). That is still the **default**, and for the overwhelming majority
 * of administrators it is the only one they will ever have.
 *
 * An administrator holding manager roles in more than one organization needs to
 * act in each. Three properties make that safe rather than merely possible:
 *
 * **The URL carries it** (`?org=`), so a link is shareable and a refresh lands
 * in the same place — `P2-13` step 5. A context held only in memory turns
 * "send me that page" into "send me that page and also click the switcher
 * first", and a context held only in storage makes two tabs fight.
 *
 * **Switching clears the cache** (step 3). TanStack Query keys are scoped per
 * organization, so stale data could not be *served* for the wrong one — but it
 * would still be sitting in memory, and any key that forgot its `orgId` would
 * leak silently. Clearing is the thing that does not depend on every future
 * hook remembering.
 *
 * **The server decides.** This is UI, not a control (`docs/UI-UX/08`
 * § Cross-Screen Requirements). Forcing `?org=` to an organization the caller
 * does not administer produces a console that asks for it and an API that
 * refuses every request — which is what `P2-13` step 6 asks for and what the
 * E2E test asserts by driving exactly that URL.
 */

interface OrgContext {
  /** The organization the console is acting in. Null before sign-in. */
  orgId: string | null;

  /** The organization the caller's token was issued for. Null before sign-in. */
  homeOrgId: string | null;

  /** True when acting somewhere other than the token's own organization. */
  switched: boolean;

  /** Switch context. Clears every cached response first. */
  switchTo: (orgId: string) => void;
}

const Context = createContext<OrgContext | null>(null);

/** The search parameter that carries the active organization. */
export const ORG_PARAM = "org";

export function OrgProvider({ children }: { children: ReactNode }) {
  const { claims } = useAuth();
  const [params, setParams] = useSearchParams();
  const queryClient = useQueryClient();

  const homeOrgId = claims?.orgId ?? null;
  const fromUrl = params.get(ORG_PARAM);

  // The URL wins when it names one, so a pasted link lands where it says.
  const orgId = fromUrl !== null && fromUrl !== "" ? fromUrl : homeOrgId;
  const switched = orgId !== null && homeOrgId !== null && orgId !== homeOrgId;

  /**
   * Keeps `?org=` attached across internal navigation.
   *
   * React Router drops search parameters on a `<Link to="/users">`, and every
   * link in this console is written that way. Without this, switching
   * organizations and then clicking anything would silently drop back to the
   * token's organization — a context change nobody asked for, in a console
   * where the next click might be a delete.
   *
   * `replace` so it does not add a history entry: the user navigated once, and
   * a back button that walks through invisible re-parameterisations is worse
   * than useless.
   *
   * Only while switched. When acting in the token's own organization the URL
   * stays clean, because a parameter that is always present is one nobody
   * reads.
   */
  useEffect(() => {
    if (!switched) return;
    if (params.get(ORG_PARAM) === orgId) return;

    const next = new URLSearchParams(params);
    next.set(ORG_PARAM, orgId as string);
    setParams(next, { replace: true });
  }, [switched, orgId, params, setParams]);

  const switchTo = useCallback(
    (target: string) => {
      if (target === orgId) return;

      // Everything, not just this organization's keys.
      //
      // Query keys are scoped per organization, so the wrong tenant's data
      // could not be served under the right key. Clearing anyway is what makes
      // that a property of the cache rather than a property of every hook
      // author remembering — and `PF-20` is on the watch list precisely
      // because that kind of leak is invisible until it is in a screenshot.
      queryClient.clear();

      const next = new URLSearchParams(params);
      if (target === homeOrgId) {
        next.delete(ORG_PARAM);
      } else {
        next.set(ORG_PARAM, target);
      }
      setParams(next);
    },
    [orgId, homeOrgId, params, setParams, queryClient],
  );

  const value = useMemo<OrgContext>(
    () => ({ orgId, homeOrgId, switched, switchTo }),
    [orgId, homeOrgId, switched, switchTo],
  );

  return <Context.Provider value={value}>{children}</Context.Provider>;
}

/**
 * The active organization.
 *
 * Falls back to the token's organization when there is no provider, so a
 * component rendered outside one — a test, a screen mounted directly — behaves
 * as it did before this task rather than throwing.
 */
export function useOrgContext(): OrgContext {
  const context = useContext(Context);
  const { claims } = useAuth();

  const fallback = useMemo<OrgContext>(
    () => ({
      orgId: claims?.orgId ?? null,
      homeOrgId: claims?.orgId ?? null,
      switched: false,
      switchTo: () => {},
    }),
    [claims],
  );

  return context ?? fallback;
}

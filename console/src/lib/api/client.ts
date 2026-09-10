import createClient from "openapi-fetch";

import { currentToken } from "../auth/tokens";

import type { paths } from "./schema.gen";

/**
 * The Management API client.
 *
 * Typed from `openapi/openapi.yaml` via `schema.gen.ts`, which is generated
 * and committed. `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` names the property this
 * buys: a backend API change not reflected in the spec breaks CI before it
 * breaks the console at runtime. The backend gets the same guarantee from the
 * other direction — its handlers implement an interface generated from the
 * same file (ADR-013) — so the spec is the one place the two sides meet.
 *
 * Never hand-write a request against a path string. A hand-written call is
 * exactly the drift the generation exists to prevent, and it will be the call
 * that keeps working locally and fails in staging.
 */

/**
 * Where the API lives.
 *
 * Injected at build time rather than discovered at runtime. The console is a
 * static bundle on a CDN (`docs/PLAN/06`), and a runtime-configurable API base URL
 * in a static SPA means an attacker who can influence that value redirects
 * every bearer token the console holds.
 *
 * Same-origin by default, which is what the local dev proxy and a
 * reverse-proxied deployment both give.
 */
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

export const api = createClient<paths>({
  baseUrl,

  // The console authenticates as an ordinary OIDC client (docs/PLAN/06 § Why the
  // Console Must Log In Through the Same OIDC Flow), so it carries an SSO
  // session cookie for the silent-authentication redirect.
  credentials: "include",
});

/**
 * The bearer token, attached to every Management API request (P1-21).
 *
 * Read at request time rather than captured at startup. The token is renewed
 * in place (ADR-019), and a client built once with the token it saw first
 * would keep presenting an expired one — which looks like an authorization
 * bug and is a staleness bug.
 *
 * A request with no token is sent WITHOUT an Authorization header rather than
 * with an empty one. `P1-15`'s middleware answers a missing header with a
 * clean 401 and `WWW-Authenticate`; an empty bearer is a malformed request,
 * and the difference is what the console can tell the user.
 */
api.use({
  onRequest({ request }) {
    const token = currentToken();
    if (token !== null) {
      request.headers.set("Authorization", `Bearer ${token}`);
    }
    return request;
  },
});

/**
 * Recovers from an expired token once, and only once per request.
 *
 * A 401 mid-action is the case `P1-21` step 8 and `docs/UI-UX/14` are about:
 * the user did nothing wrong and their work should not vanish. The renewal
 * runs, and the caller retries.
 *
 * The retry is the CALLER's, not this middleware's. Replaying a request here
 * would replay a POST as well as a GET, and silently repeating a mutation the
 * server may already have applied is worse than the 401 — `Idempotency-Key`
 * exists precisely because that decision belongs to whoever knows what the
 * request was.
 */
export function onUnauthorized(handler: () => Promise<boolean>): void {
  renewHandler = handler;
}

let renewHandler: (() => Promise<boolean>) | null = null;

api.use({
  async onResponse({ response }) {
    if (response.status === 401 && renewHandler !== null) {
      await renewHandler();
    }
    return response;
  },
});

/**
 * Query keys for TanStack Query.
 *
 * Centralised so an invalidation cannot miss a cache entry because two call
 * sites spelled the same key differently. Hierarchical, so invalidating
 * `["organizations"]` also clears every organization detail beneath it.
 */
export const queryKeys = {
  health: ["health"] as const,

  // Every key below is combined with the organization id at the call site.
  // Without that, switching organizations renders the previous one's cached
  // data — a leak in the UI even with a correct API (PF-20).
  organizations: ["organizations"] as const,
  projects: ["projects"] as const,
  applications: ["applications"] as const,
  users: ["users"] as const,
  events: ["events"] as const,
} as const;

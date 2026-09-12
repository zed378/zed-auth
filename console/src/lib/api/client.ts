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
/**
 * Same-origin by default, spelled absolutely.
 *
 * An empty base URL produces relative request paths, which a browser resolves
 * against the document and `fetch` outside one cannot parse at all — the
 * failure is `Invalid URL`, from a layer that names neither the console nor
 * the endpoint. `window.location.origin` is the same destination and works in
 * both places.
 */
const baseUrl =
  import.meta.env.VITE_API_BASE_URL ??
  (typeof window === "undefined" ? "" : window.location.origin);

export const api = createClient<paths>({
  baseUrl,

  // Resolved per request rather than captured at module load.
  //
  // openapi-fetch takes `globalThis.fetch` once, when the client is created —
  // so anything that replaces it afterwards is ignored. That is a problem
  // beyond tests: instrumentation, a service worker registering late, and any
  // polyfill loaded after this module all get skipped silently. Reading it per
  // call costs nothing and means the transport in use is the one currently
  // installed.
  fetch: (request) => globalThis.fetch(request),

  // **No credentials on API calls** (P1-29).
  //
  // The console authenticates as an ordinary OIDC client (docs/PLAN/06 § Why
  // the Console Must Log In Through the Same OIDC Flow), and the SSO session
  // cookie that flow depends on belongs to the silent-authentication redirect
  // — which is a NAVIGATION in a hidden iframe, not a fetch, and carries its
  // cookies regardless of what this client says.
  //
  // These calls are authenticated by a bearer token and nothing else. `/v1/*`
  // never reads a cookie, so sending one would be ambient authority created
  // for no purpose — and it would force the service to answer
  // `Access-Control-Allow-Credentials: true`, which is a larger claim than
  // this API needs to make about any origin.
  credentials: "omit",
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
  roles: ["roles"] as const,
  users: ["users"] as const,
  events: ["events"] as const,
} as const;

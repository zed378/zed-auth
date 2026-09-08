import createClient from "openapi-fetch";

import type { paths } from "./schema.gen";

/**
 * The Management API client.
 *
 * Typed from `openapi/openapi.yaml` via `schema.gen.ts`, which is generated
 * and committed. `PLAN/06-FRONTEND-ARCHITECTURE.md` names the property this
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
 * static bundle on a CDN (`PLAN/06`), and a runtime-configurable API base URL
 * in a static SPA means an attacker who can influence that value redirects
 * every bearer token the console holds.
 *
 * Same-origin by default, which is what the local dev proxy and a
 * reverse-proxied deployment both give.
 */
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

export const api = createClient<paths>({
  baseUrl,

  // The console authenticates as an ordinary OIDC client (PLAN/06 § Why the
  // Console Must Log In Through the Same OIDC Flow), so it carries an SSO
  // session cookie for the silent-authentication redirect. Bearer tokens for
  // Management API calls are attached by the middleware added in P1-03, when
  // there is a token to attach.
  credentials: "include",
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
} as const;

/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Base URL of the Management API.
   *
   * Build-time rather than runtime: the console is a static bundle, and a
   * runtime-configurable API origin in a static SPA is a way to redirect every
   * bearer token it holds. Empty means same-origin.
   */
  readonly VITE_API_BASE_URL?: string;

  /**
   * The OIDC issuer the console logs in against (P1-21).
   *
   * Build-time for the same reason as the API base URL, and the reason is
   * sharper here: anything that can influence this value receives every
   * authorization code the console produces. Defaults to the current origin,
   * which is what a reverse-proxied deployment gives.
   */
  readonly VITE_AUTH_ISSUER?: string;

  /**
   * The console's own `client_id` — the application record registered for it
   * as an ordinary `type: spa` client, with no secret (ADR-019).
   *
   * Not a secret. What matters is that it names a registration whose redirect
   * URIs include this origin's `/auth/callback` and `/auth/silent`, because
   * the service matches them by exact string comparison (`P1-05`).
   */
  readonly VITE_AUTH_CLIENT_ID?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

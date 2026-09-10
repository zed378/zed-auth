import type { Config } from "./oidc";

/**
 * Where the console logs in, and as whom (P1-21).
 *
 * Injected at build time, for the reason `client.ts` gives about the API base
 * URL: the console is a static bundle, and a runtime-configurable issuer in a
 * static SPA means anything that can influence that value receives every
 * authorization code the console produces.
 *
 * The client id is not a secret and never has been — it identifies a public
 * client. What matters is that it names a registered `type: spa` application
 * with this exact redirect URI, because the service matches redirect URIs by
 * exact string comparison (`P1-05`).
 */
export function authConfig(): Config {
  const issuer = import.meta.env.VITE_AUTH_ISSUER ?? window.location.origin;
  const clientId = import.meta.env.VITE_AUTH_CLIENT_ID ?? "";

  return {
    issuer: issuer.replace(/\/$/, ""),
    clientId,
    redirectUri: `${window.location.origin}/auth/callback`,
    silentRedirectUri: `${window.location.origin}/auth/silent`,

    // `openid` and nothing else. The console reads the subject and the role
    // claims from the access token and asks the API for everything else, so
    // `profile` and `email` would widen the token for no reader. Adding a
    // scope is a decision about what a stolen token can reach.
    scope: "openid",
  };
}

/** Whether the console has been told who it is. */
export function isConfigured(config: Config): boolean {
  return config.clientId !== "";
}

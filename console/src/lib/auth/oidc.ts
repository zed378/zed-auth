/**
 * The console's OIDC client (P1-21).
 *
 * Authorization Code with PKCE, exactly as any other `type: spa` client would
 * do it. `docs/PLAN/02` § Constraints and `docs/PLAN/06` both require this: the
 * console must log in through the same flow, with no endpoint, parameter or
 * exemption of its own. If something here needs a special case on the server,
 * that is a bug in this file rather than a feature to add there.
 */

import { challengeFor, randomString } from "./pkce";
import { clearToken, storeToken } from "./tokens";

/** Where the identity provider lives, and who this client is. */
export interface Config {
  issuer: string;
  clientId: string;
  redirectUri: string;
  silentRedirectUri: string;
  scope: string;
}

/**
 * The pending authorization request, held across the redirect.
 *
 * `sessionStorage`, not `localStorage`, and this is the one thing the console
 * does persist. What it holds is a verifier and a `state` — single-use values
 * for one in-flight login, useless afterwards — rather than a credential, and
 * they must survive a full page navigation to the identity provider and back.
 * `sessionStorage` is per tab, so two tabs logging in at once do not overwrite
 * each other's verifier, which `localStorage` would.
 */
const PENDING_KEY = "zedauth.console.pending";

interface Pending {
  verifier: string;
  state: string;
  /** Where the user was going before they were sent to log in. */
  returnTo: string;
}

/** Builds the authorization URL and remembers what the callback must check. */
export async function beginLogin(config: Config, returnTo: string): Promise<string> {
  const verifier = randomString();
  const state = randomString();

  savePending({ verifier, state, returnTo });

  return authorizeUrl(config, {
    state,
    challenge: await challengeFor(verifier),
    redirectUri: config.redirectUri,
  });
}

/**
 * Completes a login from the callback URL.
 *
 * Throws on anything that does not add up. The caller renders the failure;
 * this returns nothing partial.
 */
export async function completeLogin(
  config: Config,
  search: string,
): Promise<{ returnTo: string }> {
  const params = new URLSearchParams(search);
  const pending = takePending();

  const error = params.get("error");
  if (error !== null) {
    throw new AuthError(error, params.get("error_description"));
  }

  const state = params.get("state");
  const code = params.get("code");

  if (pending === null) {
    // No verifier means this callback belongs to no request this tab made —
    // a bookmarked callback URL, a replayed link, or a second tab. There is
    // nothing to exchange and nothing to prove, so it is refused rather than
    // attempted.
    throw new AuthError("invalid_request", "there is no login in progress in this tab");
  }

  // **Compared before the code is used, and compared to what THIS tab
  // generated.** A callback carrying somebody else's code and a state we never
  // issued is the login-CSRF attack: it signs the victim into the attacker's
  // account, quietly.
  if (state === null || state !== pending.state) {
    throw new AuthError("invalid_state", "the response does not match the request this tab made");
  }
  if (code === null) {
    throw new AuthError("invalid_request", "the response carries no authorization code");
  }

  await exchange(config, code, pending.verifier, config.redirectUri);
  return { returnTo: pending.returnTo };
}

/**
 * Renews the access token without touching the page.
 *
 * `prompt=none` in a hidden iframe, against the SSO session cookie the login
 * already established (ADR-019). The iframe is same-origin with the issuer, so
 * the callback inside it can post its result out; nothing else is read from it.
 *
 * Rejects rather than throwing anything the caller has to interpret: every
 * failure means the same thing to the console, which is "ask the user to log
 * in again".
 */
export async function renewSilently(config: Config, timeoutMs = 10_000): Promise<void> {
  const verifier = randomString();
  const state = randomString();

  const url = authorizeUrl(config, {
    state,
    challenge: await challengeFor(verifier),
    redirectUri: config.silentRedirectUri,
    prompt: "none",
  });

  const result = await runInHiddenFrame(url, state, timeoutMs);
  await exchange(config, result.code, verifier, config.silentRedirectUri);
}

/**
 * Ends the session at the identity provider and locally.
 *
 * The local token is cleared FIRST. If the redirect is blocked, interrupted or
 * simply slow, the console must not still be holding a usable Management API
 * token — and a logout that half-worked should fail closed.
 */
export function logout(config: Config, postLogoutRedirectUri: string): string {
  clearToken();
  clearPending();

  const params = new URLSearchParams({
    post_logout_redirect_uri: postLogoutRedirectUri,
    client_id: config.clientId,
  });
  return `${config.issuer}/oidc/logout?${params.toString()}`;
}

// --- the pieces ---------------------------------------------------------------

interface AuthorizeOptions {
  state: string;
  challenge: string;
  redirectUri: string;
  prompt?: "none";
}

function authorizeUrl(config: Config, options: AuthorizeOptions): string {
  const params = new URLSearchParams({
    response_type: "code",
    client_id: config.clientId,
    redirect_uri: options.redirectUri,
    scope: config.scope,
    state: options.state,
    code_challenge: options.challenge,
    code_challenge_method: "S256",
  });
  if (options.prompt !== undefined) params.set("prompt", options.prompt);

  return `${config.issuer}/oauth/authorize?${params.toString()}`;
}

/** Exchanges an authorization code for an access token. */
async function exchange(
  config: Config,
  code: string,
  verifier: string,
  redirectUri: string,
): Promise<void> {
  const response = await fetch(`${config.issuer}/oauth/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      client_id: config.clientId,
      redirect_uri: redirectUri,
      code_verifier: verifier,
    }),
  });

  if (!response.ok) {
    const detail = (await response.json().catch(() => null)) as {
      error?: string;
      error_description?: string;
    } | null;
    throw new AuthError(detail?.error ?? "token_exchange_failed", detail?.error_description ?? null);
  }

  const body = (await response.json()) as { access_token?: string; expires_in?: number };
  if (typeof body.access_token !== "string" || typeof body.expires_in !== "number") {
    throw new AuthError("token_exchange_failed", "the token response is not the shape we expect");
  }

  // **No refresh token is requested and none is kept.** ADR-019: a long-lived
  // credential in a public client is the thing this design avoids, and the
  // durable credential is the HttpOnly session cookie instead.
  storeToken(body.access_token, body.expires_in);
}

/**
 * Loads a URL in a hidden iframe and waits for the callback inside it to
 * report back.
 *
 * The frame posts its result with `postMessage`, and this checks the origin
 * before believing anything: a message from anywhere else is from another
 * page, and acting on one would let any site that can open a frame hand this
 * console an authorization code.
 */
function runInHiddenFrame(
  url: string,
  expectedState: string,
  timeoutMs: number,
): Promise<{ code: string }> {
  return new Promise((resolve, reject) => {
    const frame = document.createElement("iframe");
    frame.setAttribute("hidden", "");
    frame.setAttribute("title", "silent token renewal");
    frame.setAttribute("aria-hidden", "true");
    frame.style.display = "none";
    frame.src = url;

    const finish = (outcome: () => void) => {
      window.removeEventListener("message", onMessage);
      window.clearTimeout(timer);
      frame.remove();
      outcome();
    };

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;

      const data = event.data as { type?: string; state?: string; code?: string; error?: string };
      if (data?.type !== "zedauth:silent-renewal") return;
      if (data.state !== expectedState) return;

      if (typeof data.code === "string") {
        finish(() => resolve({ code: data.code as string }));
        return;
      }
      finish(() => reject(new AuthError(data.error ?? "login_required", null)));
    };

    // A silent renewal that never answers must not leave a frame and a
    // listener behind forever. The timeout is the failure path, and its
    // outcome is the same as any other: interactive login.
    const timer = window.setTimeout(() => {
      finish(() => reject(new AuthError("renewal_timeout", "the silent renewal did not answer")));
    }, timeoutMs);

    window.addEventListener("message", onMessage);
    document.body.appendChild(frame);
  });
}

function savePending(pending: Pending): void {
  sessionStorage.setItem(PENDING_KEY, JSON.stringify(pending));
}

/** Reads and removes the pending request. Single use, like the code it guards. */
function takePending(): Pending | null {
  const raw = sessionStorage.getItem(PENDING_KEY);
  sessionStorage.removeItem(PENDING_KEY);
  if (raw === null) return null;

  try {
    const parsed = JSON.parse(raw) as Partial<Pending>;
    if (typeof parsed.verifier !== "string" || typeof parsed.state !== "string") return null;
    return {
      verifier: parsed.verifier,
      state: parsed.state,
      returnTo: typeof parsed.returnTo === "string" ? parsed.returnTo : "/",
    };
  } catch {
    return null;
  }
}

function clearPending(): void {
  sessionStorage.removeItem(PENDING_KEY);
}

/** An authentication failure the console can render. */
export class AuthError extends Error {
  readonly code: string;

  constructor(code: string, description: string | null) {
    super(description ?? code);
    this.name = "AuthError";
    this.code = code;
  }

  /** Whether the user simply needs to log in again, as opposed to something being wrong. */
  get needsInteractiveLogin(): boolean {
    return (
      this.code === "login_required" ||
      this.code === "interaction_required" ||
      this.code === "consent_required" ||
      this.code === "renewal_timeout"
    );
  }
}

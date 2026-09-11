/**
 * The access token, and the claims the console is allowed to read from it
 * (P1-21, ADR-019).
 *
 * The token lives in a module-scoped variable and nowhere else — not
 * `localStorage`, not `sessionStorage`, not a cookie. ADR-019 has the full
 * reasoning; the short version is that a Management API token in `localStorage`
 * outlives the tab, the browser restart, and the incident response, and any
 * script on the origin can read it by a well-known key.
 */

let accessToken: string | null = null;
let expiresAt = 0;

/** The current token, or null when there is none. */
export function currentToken(): string | null {
  return accessToken;
}

/** When the current token expires, as epoch milliseconds. Zero when there is none. */
export function currentExpiry(): number {
  return expiresAt;
}

/** Stores a freshly issued token. */
export function storeToken(token: string, expiresInSeconds: number): void {
  accessToken = token;
  expiresAt = Date.now() + expiresInSeconds * 1000;
}

/** Forgets the token. Called on logout and on any renewal failure. */
export function clearToken(): void {
  accessToken = null;
  expiresAt = 0;
}

/**
 * How long before expiry a renewal starts.
 *
 * Sixty seconds. Long enough that a renewal has time to complete and to fail
 * over to an interactive login before anything breaks; short enough that the
 * console is not renewing constantly. A token issued with a lifetime shorter
 * than this renews immediately, which is correct rather than a bug.
 */
export const RENEW_MARGIN_MS = 60_000;

/** Whether the token is close enough to expiry to renew. */
export function needsRenewal(now = Date.now()): boolean {
  if (accessToken === null) return true;
  return now >= expiresAt - RENEW_MARGIN_MS;
}

/**
 * The claims the console reads from an access token.
 *
 * **Nothing here is a security decision.** `docs/UI-UX/08` § Cross-Screen
 * Requirements and `docs/PLAN/08` are both explicit: the API enforces every
 * permission independently, and what the console does with these claims is
 * decide what to *show*. A hidden button is not a control, and neither is a
 * route guard built on this.
 *
 * The token is not verified here either, and that is deliberate rather than an
 * omission. Verifying a signature in the browser proves the token was not
 * altered by something that already had the ability to alter it, which is no
 * assurance at all — the party this token needs to convince is the API, and it
 * verifies. Reading the payload is a rendering convenience.
 */
export interface Claims {
  subject: string;
  orgId: string | null;
  /**
   * The manager roles the token asserts, or `null` when it asserts nothing.
   *
   * The distinction is not pedantry. Until `P2-04` fills the role claim, this
   * service issues access tokens with no role information at all — so "the
   * array is empty" and "we have not been told" are both represented by an
   * empty array unless they are kept apart, and treating the second as the
   * first means the console refuses every role-gated screen to everybody,
   * including an organization owner.
   */
  roles: string[] | null;
  expiresAt: number;
}

/** Decodes an access token's payload. Returns null for anything unreadable. */
export function claimsFrom(token: string | null): Claims | null {
  if (token === null) return null;

  const parts = token.split(".");
  if (parts.length !== 3) return null;

  try {
    const payload = JSON.parse(decodeBase64Url(parts[1])) as Record<string, unknown>;
    const subject = typeof payload.sub === "string" ? payload.sub : "";
    if (subject === "") return null;

    return {
      subject,
      orgId: typeof payload.org_id === "string" ? payload.org_id : null,
      roles: rolesFrom(payload),
      expiresAt: typeof payload.exp === "number" ? payload.exp * 1000 : 0,
    };
  } catch {
    // An unreadable token is treated as no token. It cannot be used against
    // the API either, so there is nothing to salvage by guessing at it.
    return null;
  }
}

/**
 * The manager roles in a token, or `null` when it carries none.
 *
 * Tolerant about shape and strict about type: `P2-04` will nest role claims
 * under an organization key, and this must not start reporting a role because
 * a future claim happens to be an object with a `roles` property. Only an
 * array of strings counts.
 *
 * **`null` means the token said nothing**, which is the state every token is
 * in today. An empty array means it said "none", which will become possible
 * when the claim exists. See `hasRole` for why the two must not be conflated.
 */
function rolesFrom(payload: Record<string, unknown>): string[] | null {
  const raw = payload.roles ?? payload.manager_roles;
  if (!Array.isArray(raw)) return null;
  return raw.filter((entry): entry is string => typeof entry === "string");
}

function decodeBase64Url(segment: string): string {
  const padded = segment.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(padded.padEnd(padded.length + ((4 - (padded.length % 4)) % 4), "="));
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

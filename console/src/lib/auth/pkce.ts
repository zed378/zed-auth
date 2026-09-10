/**
 * PKCE, and the two random values an authorization request carries (P1-21).
 *
 * Everything here uses `crypto.getRandomValues` and `crypto.subtle`. `Math.random`
 * is not a CSPRNG and is not close to one: a `state` an attacker can predict is
 * a CSRF token that does not work, and a `code_verifier` they can predict
 * defeats the entire point of PKCE.
 *
 * `crypto.subtle` is only available on a secure context — HTTPS or localhost —
 * which is a constraint worth knowing rather than working around. A console
 * served over plaintext HTTP has larger problems than its PKCE implementation.
 */

/**
 * Bytes of entropy in a verifier and in `state`.
 *
 * RFC 7636 permits a verifier of 43 to 128 characters; 32 bytes base64url-encodes
 * to 43, the shortest the specification allows, and 256 bits is far past what
 * guessing can reach. Longer buys nothing and only makes the URL longer.
 */
const ENTROPY_BYTES = 32;

/** A random, URL-safe, unpadded base64 string. */
export function randomString(bytes = ENTROPY_BYTES): string {
  const buffer = new Uint8Array(bytes);
  crypto.getRandomValues(buffer);
  return base64Url(buffer);
}

/**
 * The S256 challenge for a verifier.
 *
 * S256 rather than `plain`. `plain` sends the verifier itself in the
 * authorization request, so anything that can read the request — a proxy log,
 * a browser history, a referrer — has the value the token exchange will be
 * checked against, and PKCE protects nothing. The service refuses `plain`
 * (`P1-06`); this refuses to offer it.
 */
export async function challengeFor(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64Url(new Uint8Array(digest));
}

/**
 * base64url without padding, as RFC 7636 §4.2 requires.
 *
 * The `=` padding and the `+` and `/` characters are all legal base64 and all
 * wrong here: two of them need escaping in a URL and the third changes the
 * string the server hashes. A mismatch shows up as a token exchange that fails
 * with an error naming neither the encoding nor the character.
 */
function base64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

import { describe, expect, it } from "vitest";

import { challengeFor, randomString } from "./pkce";
import {
  claimsFrom,
  clearToken,
  currentToken,
  needsRenewal,
  RENEW_MARGIN_MS,
  storeToken,
} from "./tokens";

describe("PKCE", () => {
  it("produces a verifier RFC 7636 accepts", () => {
    const verifier = randomString();

    // 43 characters is the shortest the specification permits, and 32 bytes
    // base64url-encodes to exactly that.
    expect(verifier).toHaveLength(43);
    expect(verifier).toMatch(/^[A-Za-z0-9\-._~]+$/);
  });

  it("produces a different verifier every time", () => {
    // Not a proof of randomness — nothing at this level is — but it does catch
    // the failure that matters: a constant, which is what a mocked or
    // misconfigured CSPRNG produces, and which would make every login
    // predictable.
    const values = new Set(Array.from({ length: 200 }, () => randomString()));
    expect(values.size).toBe(200);
  });

  it("derives the S256 challenge RFC 7636 documents", async () => {
    // The specification's own worked example (RFC 7636 appendix B), so this
    // checks the implementation against the standard rather than against
    // itself.
    const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
    await expect(challengeFor(verifier)).resolves.toBe(
      "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
    );
  });

  it("emits no base64 characters that would need escaping in a URL", async () => {
    for (let i = 0; i < 50; i += 1) {
      const challenge = await challengeFor(randomString());
      expect(challenge).not.toMatch(/[+/=]/);
    }
  });
});

describe("the token store", () => {
  it("holds a token in memory and nowhere else", () => {
    clearToken();
    storeToken("a.b.c", 300);

    expect(currentToken()).toBe("a.b.c");

    // ADR-019: not localStorage, not sessionStorage. Asserted by looking,
    // because "we did not write that line" is a statement about intent and
    // this is a statement about the browser.
    const stored = [
      ...Object.entries(localStorage),
      ...Object.entries(sessionStorage),
    ].map(([, value]) => String(value));
    expect(stored.some((value) => value.includes("a.b.c"))).toBe(false);
  });

  it("forgets the token on clear", () => {
    storeToken("a.b.c", 300);
    clearToken();
    expect(currentToken()).toBeNull();
  });

  it("renews ahead of expiry rather than after it", () => {
    clearToken();
    storeToken("a.b.c", 300);

    const now = Date.now();
    expect(needsRenewal(now)).toBe(false);

    // One millisecond inside the margin.
    expect(needsRenewal(now + 300_000 - RENEW_MARGIN_MS + 1)).toBe(true);

    // And one millisecond outside it, so the boundary is checked from both
    // sides — a test that only checks the true case passes against a function
    // that always returns true.
    expect(needsRenewal(now + 300_000 - RENEW_MARGIN_MS - 1)).toBe(false);
  });

  it("treats no token as needing renewal", () => {
    clearToken();
    expect(needsRenewal()).toBe(true);
  });
});

describe("reading claims", () => {
  const encode = (payload: Record<string, unknown>) =>
    `header.${btoa(JSON.stringify(payload)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")}.signature`;

  it("reads the subject, organization and roles", () => {
    const claims = claimsFrom(
      encode({ sub: "user-1", org_id: "org-1", roles: ["ORG_ADMIN"], exp: 1_800_000_000 }),
    );

    expect(claims).not.toBeNull();
    expect(claims?.subject).toBe("user-1");
    expect(claims?.orgId).toBe("org-1");
    expect(claims?.roles).toEqual(["ORG_ADMIN"]);
  });

  it("returns null for anything unreadable", () => {
    for (const token of [null, "", "not-a-jwt", "a.b", "a.!!!.c", `header.${btoa("[]")}.sig`]) {
      expect(claimsFrom(token)).toBeNull();
    }
  });

  it("reports roles as unknown rather than guessing when the claim is the wrong shape", () => {
    // Strict about type: `P2-04` nests role claims under an organization key,
    // and this must not start reporting a role because a future claim happens
    // to be an object. Anything that is not an array of strings is "the token
    // did not tell us" — `null` — which `hasRole` treats as deferring to the
    // API rather than as "no roles".
    //
    // The distinction matters because every token issued today is in that
    // state: the role claim does not exist yet. Reading it as an empty array
    // made the console refuse every role-gated screen to everybody, including
    // an organization owner, from `P1-22` until `P1-27` ran it in a browser.
    for (const roles of [{ org: ["ORG_OWNER"] }, "ORG_OWNER", 42, null, undefined]) {
      expect(claimsFrom(encode({ sub: "u", roles }))?.roles).toBeNull();
    }

    // And an actual array is taken at face value, empty or not.
    expect(claimsFrom(encode({ sub: "u", roles: [] }))?.roles).toEqual([]);
    expect(claimsFrom(encode({ sub: "u", roles: ["ORG_ADMIN"] }))?.roles).toEqual(["ORG_ADMIN"]);
  });

  it("drops non-string entries from a role array", () => {
    const claims = claimsFrom(encode({ sub: "u", roles: ["ORG_ADMIN", 7, null, "ORG_OWNER"] }));
    expect(claims?.roles).toEqual(["ORG_ADMIN", "ORG_OWNER"]);
  });

  it("refuses a token with no subject", () => {
    // A token that names nobody cannot drive a UI affordance, and treating it
    // as a session would show a signed-in shell to somebody who is not.
    expect(claimsFrom(encode({ org_id: "org-1", roles: ["ORG_ADMIN"] }))).toBeNull();
  });
});

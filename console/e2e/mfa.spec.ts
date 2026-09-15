import { createHmac } from "node:crypto";

import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Enrolling both factor types from the console, against a real service (P3-10).
 *
 * The card's Definition of Done names this literally: "Enrollment flows for
 * both factor types complete successfully in an E2E test". Each test ends with
 * the part that makes enrolment mean something — signing in again and being
 * asked for the new factor — because a factor row that exists and is never
 * challenged is the failure P3-03 was written to prevent.
 *
 * Each test uses its own freshly invited user, never the shared administrator:
 * an enrolled factor changes how that account signs in, and every other suite
 * signs in as the administrator.
 */

test.describe("adding a second factor from the console", () => {
  test("an authenticator app: scan, confirm, save codes, then sign in with it", async ({ page, user }) => {
    await signIn(page, user);
    await page.goto(`/users/${user.id}?tab=mfa`);

    await page.getByRole("button", { name: "Add authenticator app" }).click();
    const dialog = page.getByRole("dialog", { name: "Add an authenticator app" });
    await expect(dialog.getByRole("img", { name: /QR code/ })).toBeVisible();

    // The text alternative is the secret itself; the test reads it the way a
    // person who cannot scan would.
    const key = (await dialog.getByLabel("Setup key").textContent())?.replace(/\s+/g, "") ?? "";
    expect(key).toMatch(/^[A-Z2-7]+$/);

    await dialog.getByLabel(/6-digit code/).fill(totp(key, Date.now()));
    await dialog.getByRole("button", { name: "Confirm" }).click();

    const codes = page.getByRole("dialog", { name: "Save your recovery codes" });
    await expect(codes.getByRole("list", { name: "Recovery codes" }).getByRole("listitem")).toHaveCount(10);
    await expect(codes.getByRole("button", { name: "Done" })).toBeDisabled();
    await codes.getByLabel("I have saved these codes").check();
    await codes.getByRole("button", { name: "Done" }).click();

    // A row, not a cell: the type and the default name are both "Authenticator app".
    await expect(page.getByRole("row", { name: /Authenticator app/ })).toBeVisible();

    // Sign out and back in: the new factor is asked for. The code is one step
    // ahead, because confirming spent the current step's counter (P3-02's
    // replay bound) and the verifier accepts one step of skew.
    await page.getByRole("button", { name: /sign out/i }).click();
    // The provider's confirmation interstitial (P1-10).
    await expect(page.getByRole("heading", { name: /sign out/i })).toBeVisible();
    await page.getByRole("button", { name: /sign out/i }).click();
    await expect(page.getByRole("heading", { name: /^sign in$/i })).toBeVisible();

    await page.getByRole("button", { name: /continue to sign in/i }).click();
    await fillHostedLogin(page, user);

    const code = page.locator('input[name="code"]').first();
    await expect(code).toBeVisible();
    await code.fill(totp(key, Date.now() + 30_000));
    await page.getByRole("button", { name: "Verify" }).click();

    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
  });

  test("a passkey: registered on the service's own page, then used to sign in", async ({ page, user }) => {
    // A virtual authenticator, so the browser really runs the ceremony: the
    // console hands over to the hosted page, the page calls
    // navigator.credentials.create, and the relying party verifies what the
    // "device" signed.
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("WebAuthn.enable");
    await cdp.send("WebAuthn.addVirtualAuthenticator", {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });

    await signIn(page, user);
    await page.goto(`/users/${user.id}?tab=mfa`);
    await page.getByRole("button", { name: "Add passkey" }).click();

    // The service's origin, not the console's: that is where the relying
    // party is (PG-43).
    await expect(page.getByRole("heading", { name: "Add a passkey" })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/account/passkeys");

    await page.getByLabel("Name this passkey").fill("e2e virtual key");
    await page.getByRole("button", { name: "Create passkey" }).click();

    await expect(page.getByRole("heading", { name: "Passkey added" })).toBeVisible();
    await expect(page.locator("#recovery-codes li")).toHaveCount(10);

    await page.getByRole("link", { name: "Back to your account" }).click();
    await expect(page.getByRole("row", { name: /Passkey.*e2e virtual key/ })).toBeVisible();

    // Sign out and back in: the challenge offers the passkey, and the same
    // virtual authenticator answers it.
    await page.getByRole("button", { name: /sign out/i }).click();
    await expect(page.getByRole("heading", { name: /sign out/i })).toBeVisible();
    await page.getByRole("button", { name: /sign out/i }).click();
    await expect(page.getByRole("heading", { name: /^sign in$/i })).toBeVisible();

    await page.getByRole("button", { name: /continue to sign in/i }).click();
    await fillHostedLogin(page, user);
    await page.getByRole("button", { name: "Use a passkey or security key" }).click();

    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
  });
});

/**
 * The two cases `P3-14` found existed only below the browser (P3-14 step 3).
 *
 * `docs/PLAN/11` names "correct password with wrong TOTP is rejected" as an
 * end-to-end case, and `docs/PLAN/17`'s Phase 3 criterion is TOTP "including
 * recovery from a lost device". Both had integration tests against the login
 * handler; neither had ever been done by a browser, where the things that break
 * are the ones a handler test cannot see — which form a button submits, whether
 * a refusal leaves the person on a page they can retry from, and whether the
 * console agrees afterwards about what was spent.
 */
test.describe("signing in with a second factor, when it goes wrong", () => {
  test("a correct password with a wrong code is refused, and the right code still works", async ({ page, user }) => {
    const { key } = await enrolAuthenticatorApp(page, user);
    await signOut(page);

    await page.getByRole("button", { name: /continue to sign in/i }).click();
    await fillHostedLogin(page, user);

    // A code that is certainly wrong for every step the verifier accepts: the
    // right one for this step, plus one.
    const code = page.locator('input[name="code"]').first();
    await expect(code).toBeVisible();
    const right = totp(key, Date.now() + 30_000);
    const wrong = ((Number(right) + 1) % 1_000_000).toString().padStart(6, "0");
    await code.fill(wrong);
    await page.getByRole("button", { name: "Verify" }).click();

    // Refused: still on the challenge, told why, and not signed in.
    await expect(page.getByText(/That code is not correct/)).toBeVisible();
    await expect(page.getByRole("button", { name: /sign out/i })).not.toBeVisible();
    await expect(page.locator('input[name="code"]').first()).toBeVisible();

    // Positive control: the same page, the right code. Without this the
    // refusal above would also pass against a challenge that refuses everything.
    await page.locator('input[name="code"]').first().fill(right);
    await page.getByRole("button", { name: "Verify" }).click();
    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
  });

  test("a lost device: a recovery code signs in once, and only once", async ({ page, user }) => {
    const { codes } = await enrolAuthenticatorApp(page, user);
    expect(codes).toHaveLength(10);
    await signOut(page);

    // Typed the way somebody reads it off paper: lower case, dashes kept.
    await page.getByRole("button", { name: /continue to sign in/i }).click();
    await fillHostedLogin(page, user);
    await page.getByLabel("Recovery code").fill(codes[0].toLowerCase());
    await page.getByRole("button", { name: "Use a recovery code" }).click();
    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();

    // The account screen agrees the code is spent.
    await page.goto("/account");
    await expect(page.getByText("Unused recovery codes: 9")).toBeVisible();

    // The same code again, on a fresh sign-in: refused.
    await page.getByRole("button", { name: /sign out/i }).click();
    await expect(page.getByRole("heading", { name: /sign out/i })).toBeVisible();
    await page.getByRole("button", { name: /sign out/i }).click();
    await page.waitForURL((url) => !url.pathname.startsWith("/oidc/logout"));
    await page.goto("/");
    await page.getByRole("button", { name: /continue to sign in/i }).click();
    await fillHostedLogin(page, user);
    await page.getByLabel("Recovery code").fill(codes[0]);
    await page.getByRole("button", { name: "Use a recovery code" }).click();
    await expect(page.getByText(/not correct/)).toBeVisible();
    await expect(page.getByRole("button", { name: /sign out/i })).not.toBeVisible();

    // And a different, unused code works — the refusal was about that code.
    await page.getByLabel("Recovery code").fill(codes[1]);
    await page.getByRole("button", { name: "Use a recovery code" }).click();
    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
  });
});

/** Enrols an authenticator app from the console and returns its key and the codes shown. */
async function enrolAuthenticatorApp(
  page: Page,
  user: { id: string; email: string; password: string },
): Promise<{ key: string; codes: string[] }> {
  await signIn(page, user);
  await page.goto(`/users/${user.id}?tab=mfa`);

  await page.getByRole("button", { name: "Add authenticator app" }).click();
  const dialog = page.getByRole("dialog", { name: "Add an authenticator app" });
  const key = (await dialog.getByLabel("Setup key").textContent())?.replace(/\s+/g, "") ?? "";
  await dialog.getByLabel(/6-digit code/).fill(totp(key, Date.now()));
  await dialog.getByRole("button", { name: "Confirm" }).click();

  const shown = page.getByRole("dialog", { name: "Save your recovery codes" });
  const items = shown.getByRole("list", { name: "Recovery codes" }).getByRole("listitem");
  await expect(items).toHaveCount(10);
  const codes = (await items.allTextContents()).map((code) => code.trim());
  await shown.getByLabel("I have saved these codes").check();
  await shown.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("row", { name: /Authenticator app/ })).toBeVisible();

  return { key, codes };
}

async function signOut(page: Page): Promise<void> {
  await page.getByRole("button", { name: /sign out/i }).click();
  await expect(page.getByRole("heading", { name: /sign out/i })).toBeVisible();
  await page.getByRole("button", { name: /sign out/i }).click();
  await expect(page.getByRole("heading", { name: /^sign in$/i })).toBeVisible();
}

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await fillHostedLogin(page, user);
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

async function fillHostedLogin(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
}

/** RFC 6238 with SHA-1, 30-second steps, 6 digits — what every authenticator app computes. */
function totp(base32: string, at: number): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const char of base32.replace(/=+$/, "")) {
    bits += alphabet.indexOf(char).toString(2).padStart(5, "0");
  }
  const bytes = new Uint8Array(Math.floor(bits.length / 8));
  for (let i = 0; i < bytes.length; i++) bytes[i] = parseInt(bits.slice(i * 8, i * 8 + 8), 2);

  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const digest = createHmac("sha1", Buffer.from(bytes)).update(counter).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const value = (digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000;
  return value.toString().padStart(6, "0");
}

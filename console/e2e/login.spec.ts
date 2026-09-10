import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Login, silent renewal and logout, against a real service (P1-21 DoD item 6).
 *
 * Everything goes through the hosted login page and the same endpoints any
 * other client uses. There is no console-specific route, parameter or
 * exemption — the dogfooding constraint from `docs/PLAN/02` § Constraints —
 * and that is why this is worth an E2E test rather than a unit one: the claim
 * is about the system, not about this bundle.
 *
 * **The console under test is built with its own `VITE_AUTH_CLIENT_ID`**,
 * pointing at an application seeded before the build. That matters: the
 * alternative was a window override so a test could retarget the running
 * bundle, which would have put a test-only path into production code — on the
 * one file whose entire job is deciding where authorization codes are sent.
 */

test.describe("the console logs in like any other client", () => {
  test("signs in through the hosted page and lands where it was going", async ({ page, user }) => {
    // A deep link, not the root. A login that drops the user on the overview
    // has lost what they were doing (docs/UI-UX/14).
    await page.goto("/audit-log");

    // The hosted login page, served by the identity provider rather than by
    // the console — and on the provider's own path.
    await expect(page.getByRole("heading", { name: /sign in to/i })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/login");

    await signInOnHostedPage(page, user);

    await expect(page).toHaveURL(/\/audit-log$/);
    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();

    // **The token is not in browser storage** (ADR-019). Asserted here as well
    // as in the unit test, because this is the built bundle in a real browser
    // rather than a module under jsdom — and the unit test cannot see what the
    // bundler did.
    const stored = await page.evaluate(() => ({
      local: JSON.stringify(window.localStorage),
      session: JSON.stringify(window.sessionStorage),
    }));
    expect(stored.local).not.toMatch(/eyJ/); // a JWT's base64 header
    expect(stored.session).not.toMatch(/eyJ/);
  });

  test("recovers the session on reload, without leaving the console", async ({ page, user }) => {
    await signIn(page, user);

    // The token lives only in memory, so a reload has none and the console
    // recovers it through prompt=none. This is the assertion that says silent
    // renewal actually works rather than merely being implemented.
    await page.reload();

    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
    expect(new URL(page.url()).pathname).not.toBe("/login");
  });

  test("keeps the page it is on while it renews", async ({ page, user }) => {
    await signIn(page, user);
    await page.goto("/settings");

    const heading = await page.getByRole("heading").first().textContent();

    // A renewal in a hidden iframe must not navigate the top window. Waiting
    // out a token lifetime is not practical, so this drives the same path a
    // reload takes and asserts the page survived it.
    await page.reload();
    await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();

    expect(await page.getByRole("heading").first().textContent()).toBe(heading);
    await expect(page).toHaveURL(/\/settings$/);
  });

  test("signs out, and the session is genuinely over", async ({ page, user }) => {
    await signIn(page, user);

    await page.getByRole("button", { name: /sign out/i }).click();

    // Back at the console with no session: the silent renewal now fails with
    // login_required, so the sign-in prompt appears — not a blank screen, and
    // not a signed-in shell whose every data call 401s.
    await expect(page.getByRole("heading", { name: /^sign in$/i })).toBeVisible();

    // And it stays over across a reload. **This is the assertion that
    // separates "the console forgot its token" from "the session ended"** —
    // without it, a logout that cleared only local state would pass.
    await page.reload();
    await expect(page.getByRole("heading", { name: /^sign in$/i })).toBeVisible();
  });
});

test.describe("a route the claims do not permit", () => {
  test("is unreachable by typing its URL", async ({ page, user }) => {
    await signIn(page, user);

    // The seeded user holds no manager role: `P1-19` creates users with none,
    // and granting them is `P2`'s. So every role-gated screen must refuse —
    // by URL, which is what docs/UI-UX/08 asks for over merely hiding the nav
    // item.
    await page.goto("/policies");

    await expect(page.getByText(/do not have access/i)).toBeVisible();
  });
});

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await signInOnHostedPage(page, user);
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

async function signInOnHostedPage(
  page: Page,
  user: { email: string; password: string },
): Promise<void> {
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
}

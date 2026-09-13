import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Personal account settings on a phone (P3-12).
 *
 * `docs/UI-UX/08` makes this the one console screen that must work on a phone,
 * and the card's Definition of Done asks for every action to work on a real
 * mobile viewport. So these run at 390×844 — a common phone — and assert what
 * breaks first on a small screen: sideways scrolling, targets too small to tap,
 * and forms that cannot be completed.
 *
 * It also carries `P3-11`'s last item: the Sessions revoke button at phone
 * width, which only this screen can show.
 */

test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

test.describe("personal account settings on a phone", () => {
  test("renders without sideways scrolling, with tappable targets", async ({ page, user }) => {
    // A second session, so the Sessions list has a revoke button to measure.
    // A desktop context: `test.use` above applies to new contexts too, and the
    // console itself refuses a phone-width screen.
    const other = await (
      await page.context().browser()!.newContext({ viewport: { width: 1280, height: 800 }, isMobile: false, hasTouch: false })
    ).newPage();
    await signIn(other, user, "/");

    await signIn(page, user, "/account");
    await expect(page.getByRole("heading", { name: "Your account", level: 1 })).toBeVisible();
    await expect(page.getByText("This screen is too narrow")).not.toBeVisible();

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);

    for (const name of ["Change password", "Add authenticator app", /^Revoke session on /]) {
      const button = page.getByRole("button", { name }).first();
      await button.scrollIntoViewIfNeeded();
      const box = await button.boundingBox();
      expect(box, `the ${String(name)} button has no box`).not.toBeNull();
      // WCAG 2.2 AA 2.5.8: at least 24×24 CSS pixels, and inside the screen.
      expect(box!.height).toBeGreaterThanOrEqual(24);
      expect(box!.width).toBeGreaterThanOrEqual(24);
      expect(box!.x + box!.width).toBeLessThanOrEqual(390);
    }

    await expect(page.getByText("Not available", { exact: true })).toBeVisible();
  });

  test("changes the password on a phone, and the new one signs in", async ({ page, user }) => {
    await signIn(page, user, "/account");

    const next = "A Phone-Chosen Password 2026";
    await page.getByLabel("Current password").fill(user.password);
    await page.getByLabel("New password", { exact: true }).fill(next);
    await page.getByLabel("Confirm new password").fill(next);
    await page.getByRole("button", { name: "Change password" }).tap();

    await expect(page.getByText(/Your password is changed/)).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).tap();
    await expect(page.getByRole("heading", { name: /sign out/i })).toBeVisible();
    await page.getByRole("button", { name: /sign out/i }).tap();
    // Wait for the logout to land before navigating, or the navigation cancels it.
    await page.waitForURL((url) => !url.pathname.startsWith("/oidc/logout"));

    await signIn(page, { email: user.email, password: next }, "/account");
    await expect(page.getByRole("heading", { name: "Your account", level: 1 })).toBeVisible();
  });
});

async function signIn(page: Page, user: { email: string; password: string }, path: string): Promise<void> {
  await page.goto(path);
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).toHaveURL(new RegExp(`${path === "/" ? "/$" : path}`));
}

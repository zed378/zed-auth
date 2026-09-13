import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * `docs/UI-UX/04` Flow 4, end to end (P3-11).
 *
 * "Revoke a lost-device session": the same person signed in on two browsers,
 * one of them revokes the other from the Sessions tab, and the other browser's
 * session is genuinely over — not merely gone from a list. Asserted the way the
 * logout suite asserts it: the revoked browser reloads and is asked to sign in.
 */

test.describe("revoking a lost device's session", () => {
  test("from the Sessions tab, and the other browser really is signed out", async ({ browser, user }) => {
    const lost = await (await browser.newContext()).newPage();
    await signIn(lost, user);

    const here = await (await browser.newContext()).newPage();
    await signIn(here, user);
    await here.goto(`/users/${user.id}?tab=sessions`);

    const rows = here.getByRole("table", { name: "Where you are signed in" }).getByRole("row");
    const current = rows.filter({ hasText: "This session" });
    const other = rows.filter({ hasNotText: "This session" }).filter({ has: here.getByRole("button") });
    await expect(current).toHaveCount(1);
    await expect(other).toHaveCount(1);

    // The screen-reader label names the device, never a bare "Revoke".
    const revoke = other.getByRole("button", { name: /^Revoke session on / });
    await revoke.click();
    await expect(other).toHaveCount(0);
    await expect(here.getByText("No other active sessions.")).toBeVisible();

    // The lost browser's session is over: its next load cannot renew silently.
    await lost.reload();
    await expect(lost.getByRole("heading", { name: /^sign in$/i })).toBeVisible();

    // And the browser that did the revoking is still signed in.
    await here.reload();
    await expect(here.getByRole("button", { name: /sign out/i })).toBeVisible();
  });

  // At the NARROWEST width the console serves. Below 768px the console shows
  // "this screen is too narrow" by design (docs/UI-UX/12): it is a desktop
  // tool, and the one screen that must work on a phone is P3-12's personal
  // settings, which reuses this tab. The phone-width check belongs there, and
  // P3-12's card carries it — asserting it here would test a screen that
  // deliberately does not render.
  test("the revoke target is a usable tap target at the narrowest supported width", async ({ browser, user }) => {
    const other = await (await browser.newContext()).newPage();
    await signIn(other, user);

    const tablet = await (await browser.newContext({ viewport: { width: 768, height: 1024 } })).newPage();
    await signIn(tablet, user);
    await tablet.goto(`/users/${user.id}?tab=sessions`);

    const button = tablet.getByRole("button", { name: /^Revoke session on / }).first();
    await expect(button).toBeVisible();
    const box = await button.boundingBox();
    expect(box).not.toBeNull();
    // WCAG 2.2 AA 2.5.8: at least 24 by 24 CSS pixels.
    expect(box!.height).toBeGreaterThanOrEqual(24);
    expect(box!.width).toBeGreaterThanOrEqual(24);
    // Reachable without scrolling sideways.
    expect(box!.x + box!.width).toBeLessThanOrEqual(768);
  });
});

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * The organization switcher against a real service (`P2-13`).
 *
 * The Definition of Done asks for one thing no component test can show:
 * **selecting an unauthorized organization is refused server-side even if
 * forced client-side.** The switcher is UI, and `docs/UI-UX/08`
 * § Cross-Screen Requirements is explicit that UI is never the control — so
 * the test that matters drives the URL the switcher would never produce and
 * checks that the service refuses anyway.
 */

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

test.describe("the organization switcher", () => {
  test("names the active organization on every screen", async ({ page, admin, organization }) => {
    await signIn(page, admin);

    // Step 4: unmistakable at all times, not only where a switch is possible.
    // The E2E administrator administers exactly one organization, so this is
    // also the single-organization case — a label, with no control.
    // Scoped to the navigation. The organization is named "e2e" on this stack
    // and so is the overview page's heading — an unscoped locator matches both
    // and proves neither.
    const chrome = page.getByRole("navigation", { name: /console/i });

    for (const destination of ["/", "/projects", "/users", "/audit-log"]) {
      await page.goto(destination);
      await expect(chrome.getByText("Organization", { exact: true })).toBeVisible();
      await expect(chrome.getByText(organization.name, { exact: true })).toBeVisible();
    }
  });

  test("offers only what the API says the caller administers", async ({ page, admin, api }) => {
    await signIn(page, admin);

    // Server-side truth, read directly. The console must offer this and
    // nothing else — the DoD's first line.
    const administered = await api<{ organizations: { id: string; name: string }[] }>(
      "GET",
      "/v1/me/organizations?page_size=100",
    );

    expect(
      administered.organizations.length,
      "the E2E administrator should administer at least one organization",
    ).toBeGreaterThan(0);

    // One organization: a label rather than a switcher, per step 1.
    if (administered.organizations.length === 1) {
      await expect(
        page.getByRole("button", { name: new RegExp(administered.organizations[0].name, "i") }),
      ).toBeHidden();
    }
  });

  test("an organization forced into the URL is refused by the service", async ({
    page,
    admin,
    organization,
  }) => {
    await signIn(page, admin);

    // An organization this caller holds nothing over. A well-formed id that
    // the switcher would never offer, which is exactly the point: the refusal
    // has to come from the service, not from the absence of a menu entry.
    const forged = "00000000-0000-4000-8000-000000000000";
    expect(forged).not.toBe(organization.id);

    await page.goto(`/projects?org=${forged}`);

    // The console asked, and the API refused. Either rendering is correct —
    // what must NOT happen is a list of projects.
    await expect(
      page.getByText(/do not have access|something went wrong/i).first(),
    ).toBeVisible();

    // And the chrome says why, rather than leaving somebody to read an error
    // state and wonder what broke. This is the switcher reporting server-side
    // truth back at the user: the organization in the URL is not one the
    // caller administers.
    await expect(
      page.getByRole("navigation", { name: /console/i }).getByRole("status"),
    ).toContainText(/do not administer this organization/i);

    // Nothing from the caller's own organization leaked into a context they
    // are not in. A console that quietly fell back to the token's organization
    // on a refusal would look like it worked.
    //
    // The create button is NOT asserted hidden: it is gated on the token's
    // manager-role claim, which is a property of the caller rather than of the
    // context, and the API refuses the POST regardless — a hidden button was
    // never the control (`docs/UI-UX/08` § Cross-Screen Requirements). What
    // matters is that no project of theirs is listed here.
    await expect(page.getByRole("table")).toBeHidden();
  });

  test("the forced context survives a refresh rather than being silently dropped", async ({
    page,
    admin,
  }) => {
    await signIn(page, admin);

    const forged = "00000000-0000-4000-8000-000000000000";
    await page.goto(`/projects?org=${forged}`);
    await page.reload();

    // Step 5 in its least comfortable form: the URL is the context, so a
    // refresh lands in the same place — including a place the service refuses.
    // Quietly dropping back to the token's organization would be a context
    // change nobody asked for, in a console where the next click might delete
    // something.
    expect(new URL(page.url()).searchParams.get("org")).toBe(forged);
  });
});

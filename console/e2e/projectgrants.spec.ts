import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Creating and revoking a Project Grant through the console (`P4-05`,
 * `docs/UI-UX/04` Flow 2).
 *
 * As in `authorizations.spec.ts`, every step is asserted on the screen and then
 * against the Management API. A console that showed a grant the API does not
 * hold would pass every on-screen check.
 *
 * The typed-confirmation branch of revocation needs someone to hold a role
 * through the grant, and nothing can create that until `P4-02`. It is covered
 * by `projectgrants.test.tsx` with the count stubbed; this suite covers the
 * path a real grant takes today.
 */

const partnerOrgId = process.env.E2E_PARTNER_ORG_ID;

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

interface GrantList {
  grants: {
    id: string;
    granted_org_id: string;
    granted_org_name: string;
    granted_role_keys: string[];
    status: string;
  }[];
}

test.describe("Project Grants", () => {
  test("a grant created and revoked in the console is the grant the API holds", async ({
    page,
    admin,
    organization,
    project,
    api,
  }) => {
    expect(
      partnerOrgId,
      "E2E_PARTNER_ORG_ID is unset — run scripts/e2e-up.sh and source .e2e.env",
    ).toBeTruthy();
    const partner = partnerOrgId as string;
    const grantsPath = `/v1/organizations/${organization.id}/projects/${project.id}/grants`;

    // Two roles, so the grant can share one and visibly withhold the other.
    const stamp = Date.now();
    const shared = `cashier-${stamp}`;
    const withheld = `manager-${stamp}`;
    for (const key of [shared, withheld]) {
      await api("POST", `/v1/organizations/${organization.id}/projects/${project.id}/roles`, {
        key,
        display_name: key,
        permission_keys: [],
      });
    }

    await signIn(page, admin);
    await page.goto(`/projects/${project.id}/grants`);
    // Exact: a fresh project has no grants, so the empty state's
    // "No Project Grants yet" heading is on the page too, and a substring
    // match failed strict mode whenever the list resolved first.
    await expect(
      page.getByRole("heading", { name: "Project Grants", exact: true }),
    ).toBeVisible();

    await page.getByRole("button", { name: /^create grant$/i }).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Organization ID").fill(partner);
    // Nothing ticked yet: the warning, and no way forward.
    await expect(dialog.getByText("This Project Grant has no roles selected yet.")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Review" })).toBeDisabled();

    await dialog.getByRole("checkbox", { name: new RegExp(shared) }).check();
    // The summary builds as the box is ticked.
    await expect(dialog.getByText(/will be able to give this role/)).toContainText(shared);

    await dialog.getByRole("button", { name: "Review" }).click();
    await expect(dialog.getByText(`Not shared: ${withheld}.`)).toBeVisible();
    await dialog.getByRole("button", { name: "Create grant" }).click();
    await expect(dialog).toBeHidden();

    // The table names the partner, which the form could not (ADR-026).
    const row = page.getByRole("row", { name: /e2e-partner/ });
    await expect(row).toBeVisible();
    await expect(row.getByText(`Not shared: ${withheld}`)).toBeVisible();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();

    // **The direction that matters.**
    const created = await api<GrantList>("GET", grantsPath);
    const grant = created.grants.find((entry) => entry.granted_org_id === partner);
    expect(grant, "the API holds no grant the console said it created").toBeDefined();
    expect(grant?.granted_role_keys).toEqual([shared]);
    expect(grant?.status).toBe("active");

    // Revoke. Nobody holds a role through it, so the consequence says so and
    // no typing is asked for.
    await row.getByRole("button", { name: "Revoke" }).click();
    const confirm = page.getByRole("dialog");
    await expect(confirm.getByText(/Nobody currently holds a role through this grant/)).toBeVisible();
    await confirm.getByRole("button", { name: "Revoke grant" }).click();
    await expect(confirm).toBeHidden();

    await expect(row.getByText("Revoked", { exact: true })).toBeVisible();
    await expect(row.getByRole("button", { name: "Revoke" })).toHaveCount(0);

    const after = await api<GrantList>("GET", grantsPath);
    expect(after.grants.find((entry) => entry.id === grant?.id)?.status).toBe("revoked");
  });
});

import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Assigning and revoking a project role through the console (`P2-12`).
 *
 * Its Definition of Done asks for assignment and revocation to be covered
 * end-to-end, and `docs/PLAN/06` § Testing names "invite plus first role
 * assignment" as one of the console flows this layer exists for.
 *
 * Every assertion is made **twice**: once against the screen, and once against
 * the Management API. The second is the one that matters. A console that wrote
 * a grant somewhere the API cannot see would satisfy every on-screen
 * assertion — and `docs/PLAN/02` FR-14's rule that the console has no private
 * path is exactly the kind of claim only a cross-surface test can make.
 */

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

interface GrantList {
  grants: { project_id: string; role_keys: string[] }[];
}

test.describe("granting and revoking a role", () => {
  test("a role granted in the console is the grant the API reports", async ({
    page,
    admin,
    organization,
    project,
    user,
    api,
  }) => {
    // A role to grant. Created through the API, so the test is about the
    // grant rather than about role creation — `roles.test.tsx` covers that.
    const roleKey = `cashier-${Date.now()}`;
    await api(
      "POST",
      `/v1/organizations/${organization.id}/projects/${project.id}/roles`,
      { key: roleKey, display_name: "Cashier", permission_keys: ["sale:create"] },
    );

    await signIn(page, admin);
    await page.goto(`/projects/${project.id}/authorizations`);

    // The user starts with no access, and the screen says so rather than
    // showing a blank (`docs/PLAN/08` § Least Privilege).
    await page.getByLabel(/find a user/i).fill(user.email);
    await page.getByRole("button", { name: new RegExp(user.email, "i") }).click();
    await expect(page.getByText(/No access\./i)).toBeVisible();

    await page.getByRole("button", { name: /grant access/i }).click();
    // The permission keys are on screen at the moment of granting — the whole
    // point of step 3 — so this also proves they rendered.
    await expect(page.getByText("sale:create")).toBeVisible();
    await page.getByRole("checkbox", { name: new RegExp(roleKey, "i") }).check();
    await page.getByRole("button", { name: /save roles/i }).click();

    // The console says it worked.
    await expect(page.getByText(/No access\./i)).toBeHidden();
    await expect(page.getByText(roleKey, { exact: true })).toBeVisible();

    // **The direction that matters.**
    const granted = await api<GrantList>(
      "GET",
      `/v1/organizations/${organization.id}/users/${user.id}/grants`,
    );
    const grant = granted.grants.find((entry) => entry.project_id === project.id);
    expect(grant, `the API reports no grant for a role the console said it granted`).toBeDefined();
    expect(grant?.role_keys).toContain(roleKey);
  });

  test("revoking in the console deletes the grant, not just the row on screen", async ({
    page,
    admin,
    organization,
    project,
    user,
    api,
  }) => {
    const roleKey = `auditor-${Date.now()}`;
    await api(
      "POST",
      `/v1/organizations/${organization.id}/projects/${project.id}/roles`,
      { key: roleKey, display_name: "Auditor", permission_keys: ["sale:read"] },
    );
    await api("POST", `/v1/organizations/${organization.id}/users/${user.id}/grants`, {
      project_id: project.id,
      role_keys: [roleKey],
    });

    await signIn(page, admin);
    await page.goto(`/projects/${project.id}/authorizations`);
    await page.getByLabel(/find a user/i).fill(user.email);
    await page.getByRole("button", { name: new RegExp(user.email, "i") }).click();

    // The grant the API created is visible in the console — the other half of
    // the consistency claim.
    await expect(page.getByText(roleKey, { exact: true })).toBeVisible();

    await page.getByRole("button", { name: /revoke all access/i }).click();
    // The dialog states the consequence before the confirmation, and says the
    // part people get wrong: an already-issued token keeps its claims.
    await expect(page.getByRole("dialog")).toContainText(/takes effect immediately/i);
    await expect(page.getByRole("dialog")).toContainText(/until it expires/i);
    await page.getByRole("button", { name: "Revoke access" }).click();

    await expect(page.getByText(/No access\./i)).toBeVisible();

    const after = await api<GrantList>(
      "GET",
      `/v1/organizations/${organization.id}/users/${user.id}/grants`,
    );
    expect(
      after.grants.find((entry) => entry.project_id === project.id),
      "the API still reports a grant the console said it revoked",
    ).toBeUndefined();
  });

  test("a grant made through the API appears on the user's Grants tab", async ({
    page,
    admin,
    organization,
    project,
    user,
    api,
  }) => {
    const roleKey = `viewer-${Date.now()}`;
    await api(
      "POST",
      `/v1/organizations/${organization.id}/projects/${project.id}/roles`,
      { key: roleKey, display_name: "Viewer", permission_keys: [] },
    );
    await api("POST", `/v1/organizations/${organization.id}/users/${user.id}/grants`, {
      project_id: project.id,
      role_keys: [roleKey],
    });

    await signIn(page, admin);
    await page.goto(`/users/${user.id}`);
    await page.getByRole("tab", { name: /grants/i }).click();

    // `P2-12` step 7: the same information, from the same components, on the
    // screen an administrator reaches from the user rather than the project.
    await expect(page.getByRole("link", { name: project.name })).toBeVisible();
    await expect(page.getByText(roleKey, { exact: true })).toBeVisible();
  });
});

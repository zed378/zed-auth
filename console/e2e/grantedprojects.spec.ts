import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * Granted Projects — the receiving side, end to end (`P4-06`,
 * `docs/UI-UX/04` Flow 3).
 *
 * The grant comes from `scripts/e2e-up.sh`, not from this suite: the E2E
 * administrator holds `ORG_ADMIN` over `e2e` and nothing at all in
 * `e2e-partner`, so there is no API call it could make to create the receiving
 * side of a grant. That limitation is the suite working as intended — an
 * administrator who could grant themselves a project would make every
 * permission check here untestable.
 *
 * As in `projectgrants.spec.ts`, every step is asserted on the screen and then
 * against the Management API. A console that showed an assignment the API does
 * not hold would pass every on-screen check.
 */

const grantId = process.env.E2E_RECEIVED_GRANT_ID;
const projectName = process.env.E2E_RECEIVED_PROJECT_NAME;
const sharedRole = process.env.E2E_RECEIVED_SHARED_ROLE;
const withheldRole = process.env.E2E_RECEIVED_WITHHELD_ROLE;

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

interface DelegatedList {
  grants: { user_id: string; role_keys: string[]; project_grant_id?: string | null }[];
}

test.describe("Granted Projects", () => {
  test("a delegated role assigned in the console is the grant the API holds", async ({
    page,
    admin,
    organization,
    user,
    api,
  }) => {
    expect(
      grantId,
      "E2E_RECEIVED_GRANT_ID is unset — run scripts/e2e-up.sh and source .e2e.env",
    ).toBeTruthy();
    const assignmentsPath = `/v1/organizations/${organization.id}/project-grants/${grantId}/user-grants`;

    await signIn(page, admin);
    await page.goto("/granted-projects");
    await expect(page.getByRole("heading", { name: "Granted Projects" })).toBeVisible();

    // The row names the project and the organization behind it — neither of
    // which this tenant can see in its own tables.
    const row = page.getByRole("row", { name: new RegExp(projectName as string) });
    await expect(row).toBeVisible();
    // Exact: the delegated badge also carries "— delegated from e2e-partner"
    // for screen readers, which is the next assertion's business.
    await expect(row.getByText("e2e-partner", { exact: true })).toBeVisible();
    await expect(row.getByText(/delegated from e2e-partner/i).first()).toBeAttached();
    await expect(row.getByText(sharedRole as string)).toBeVisible();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();

    await row.getByRole("button", { name: /assign roles/i }).click();
    const panel = page.getByRole("dialog");

    // **The rule this screen exists to keep.** The partner's other role is not
    // disabled or greyed here; it is absent (`docs/UI-UX/08`).
    await expect(panel.getByRole("checkbox", { name: new RegExp(sharedRole as string) })).toBeVisible();
    await expect(
      panel.getByRole("checkbox", { name: new RegExp(withheldRole as string) }),
    ).toHaveCount(0);
    await expect(panel.getByText(withheldRole as string)).toHaveCount(0);

    await panel.getByLabel(/person/i).selectOption(user.id);
    await panel.getByRole("checkbox", { name: new RegExp(sharedRole as string) }).check();
    await panel.getByRole("button", { name: /^assign roles$/i }).click();

    // The assignment appears in the panel, marked as coming from elsewhere.
    // Exact: the person picker still holds an <option> naming the same
    // address, and an <option> is not visible.
    await expect(panel.getByText(user.email, { exact: true })).toBeVisible();
    await expect(panel.getByText(/delegated from e2e-partner/i).first()).toBeVisible();

    // **The direction that matters.**
    const held = await api<DelegatedList>("GET", assignmentsPath);
    const assignment = held.grants.find((entry) => entry.user_id === user.id);
    expect(assignment, "the API holds no assignment the console said it made").toBeDefined();
    expect(assignment?.role_keys).toEqual([sharedRole]);
    expect(assignment?.project_grant_id).toBe(grantId);

    // Removing it says who loses what, and then the API agrees.
    //
    // Scoped to THIS user's entry: a run that failed after assigning leaves its
    // own holder behind, and a suite that picked the first Remove button would
    // delete a stranger's row and still pass.
    const entry = panel.getByRole("listitem").filter({ hasText: user.email });
    await entry.getByRole("button", { name: /^remove$/i }).click();
    const confirm = page.getByRole("dialog").last();
    await expect(confirm.getByText(new RegExp(user.email)).first()).toBeVisible();
    await confirm.getByRole("button", { name: /remove roles/i }).click();

    await expect(entry).toHaveCount(0);
    const after = await api<DelegatedList>("GET", assignmentsPath);
    expect(after.grants.find((entry) => entry.user_id === user.id)).toBeUndefined();
  });
});

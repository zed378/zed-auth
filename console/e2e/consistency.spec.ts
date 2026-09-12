import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";

/**
 * API and console consistency (`P1-28`, `docs/PLAN/17` § Phase 1).
 *
 * > *Organizations, projects, applications, and users can be created via both
 * > the REST API and the console, and the two stay consistent (creating via
 * > one is visible via the other).*
 *
 * That criterion is about a **relationship between two surfaces**, and it is
 * the one kind of claim neither surface's own tests can make. The API tests
 * prove the API; the console's unit tests prove the console renders an array.
 * Only this can show that the array is the one the API returned.
 *
 * `docs/PLAN/02` FR-14 is the constraint underneath: every capability in the
 * console is available through the same public REST API, with no console-only
 * shortcut. Both directions are checked, because "the console can read what
 * the API wrote" and "the API can read what the console wrote" are different
 * statements and only the second catches a console that writes somewhere else.
 */

async function signIn(page: Page, user: { email: string; password: string }): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: /continue to sign in/i }).click();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.getByRole("button", { name: /sign out/i })).toBeVisible();
}

test.describe("the API and the console show the same thing", () => {
  test("a project created through the API appears in the console", async ({
    page,
    admin,
    project,
  }) => {
    // `project` is the fixture, and the fixture creates it through the
    // Management API — the same endpoint an operator would call.
    await signIn(page, admin);
    await page.goto("/projects");

    await expect(page.getByRole("link", { name: project.name })).toBeVisible();
  });

  test("a project created in the console is visible through the API", async ({
    page,
    admin,
    organization,
    api,
  }) => {
    const name = `console-made-${Date.now()}`;

    await signIn(page, admin);
    await page.goto("/projects");
    await page.getByRole("button", { name: /new project/i }).click();
    await page.getByLabel(/^name$/i).fill(name);
    await page.getByRole("button", { name: /create project/i }).click();

    // The console says it worked.
    await expect(page.getByRole("link", { name })).toBeVisible();

    // **The direction that matters.** A console writing to somewhere the API
    // cannot see would pass every assertion above it.
    const listed = await api<{ projects: { id: string; name: string }[] }>(
      "GET",
      `/v1/organizations/${organization.id}/projects?page_size=100`,
    );
    const found = listed.projects.find((entry) => entry.name === name);
    expect(found, `the API does not list a project the console created: ${name}`).toBeDefined();

    // Not cleaned up here, deliberately. Deleting a project needs ORG_OWNER
    // (`P1-17` raised it above the other project verbs), and this suite runs
    // as ORG_ADMIN on purpose — a suite holding the most powerful role in the
    // system is one that cannot notice a missing permission check. A leftover
    // project is visible in the next run's list rather than silent.
  });

  test("a user created through the API appears in the console, with the same status", async ({
    page,
    admin,
    user,
  }) => {
    await signIn(page, admin);
    await page.goto("/users");

    const row = page.getByRole("row", { name: new RegExp(user.email, "i") });
    await expect(row).toBeVisible();

    // Not just present — the same state. `P1-19` creates a user as `invited`,
    // and this account has since accepted (the fixture sets a password through
    // the real hosted page), so the console must say `active`. A console
    // showing a stale or defaulted status is the failure this catches.
    await expect(row).toContainText(/active/i);
  });

  test("an application registered through the API appears in its project", async ({
    page,
    admin,
    project,
    application,
  }) => {
    await signIn(page, admin);
    await page.goto(`/projects/${project.id}`);

    await expect(page.getByRole("heading", { name: /applications/i })).toBeVisible();

    // The application id IS the client_id (`docs/PLAN/04`), and `docs/UI-UX/08`
    // names viewing it as one of three things this tab is for. It was absent
    // until this test asked for it — the column existed in the specification
    // and not in the table, which is the kind of gap only a criterion walked
    // deliberately will find.
    await expect(page.getByText(application.clientId, { exact: false })).toBeVisible();
  });

  test("the console never reaches an endpoint the contract does not document", async ({
    page,
    admin,
  }) => {
    // FR-14 from the other side: no console-only shortcut. Every request the
    // console makes to the service must be a documented path.
    //
    // Checked by watching the traffic rather than by reading the source,
    // because the claim is about what it DOES — and a fetch added in a
    // component nobody re-reads is exactly how a private endpoint arrives.
    const issuer = new URL(process.env.E2E_AUTH_ISSUER!);
    const seen: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.host === issuer.host) seen.push(url.pathname);
    });

    await signIn(page, admin);
    for (const path of ["/projects", "/users", "/audit-log", "/settings"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");
    }

    const documented = (path: string) =>
      path.startsWith("/v1/") ||
      path.startsWith("/oauth/") ||
      path.startsWith("/oidc/") ||
      path.startsWith("/.well-known/") ||
      path === "/login";

    const undocumented = [...new Set(seen)].filter((path) => !documented(path));
    expect(undocumented, "the console called a path outside the published surface").toEqual([]);

    // And it did call the API — otherwise the assertion above is satisfied by
    // a console that talks to nothing.
    expect(seen.some((path) => path.startsWith("/v1/"))).toBe(true);
  });
});

import { test as base } from "@playwright/test";

/**
 * Fixtures for console E2E tests.
 *
 * `P0-15` step 3 asked for "the fixtures Phase 1 will need: seed an org, a
 * project, an application, and a user", and left them unimplemented rather
 * than mocked — a fixture returning fake data lets a test pass against
 * nothing. `P1-15` through `P1-19` shipped the endpoints they were waiting
 * for, so they are real now.
 *
 * **Everything is seeded through the Management API**, using the same
 * endpoints an administrator would. That is not a convenience: a fixture that
 * reached into the database would create objects the API might refuse, and a
 * suite built on those tests a system nobody can operate.
 */

const issuer = required("E2E_AUTH_ISSUER");
const bootstrapToken = required("E2E_BOOTSTRAP_TOKEN");
const orgId = required("E2E_ORG_ID");

/**
 * Reads an environment variable, or fails loudly.
 *
 * Not a default and not a skip. `P0-15`'s rule: a suite that skips when its
 * dependency is unreachable reports success having run nothing, and a green
 * suite that ran no tests is a false statement everybody acts on.
 */
function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") {
    throw new Error(
      `${name} is required to run the console E2E suite.\n\n` +
        `These tests drive a real login against a real service; there is no ` +
        `mock to fall back to, and skipping would report success having ` +
        `tested nothing. See TASKS/PHASE-1 P1-27 for how CI supplies it.`,
    );
  }
  return value;
}

export interface SeededOrganization {
  id: string;
  name: string;
}

export interface SeededProject {
  id: string;
  orgId: string;
  name: string;
}

export interface SeededApplication {
  id: string;
  projectId: string;
  clientId: string;
  redirectUri: string;
}

export interface SeededUser {
  id: string;
  orgId: string;
  email: string;
  password: string;
}

interface Fixtures {
  organization: SeededOrganization;
  project: SeededProject;
  application: SeededApplication;
  user: SeededUser;
}

/** A Management API call as the bootstrap administrator. */
async function manage<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(`${issuer}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${bootstrapToken}`,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`${method} ${path} answered ${response.status}: ${detail}`);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

/** A name nothing else will collide with, and that says where it came from. */
function unique(prefix: string): string {
  return `${prefix}-e2e-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
}

export const test = base.extend<Fixtures>({
  organization: async ({}, use) => {
    // The organization the bootstrap token is scoped to. Creating a second one
    // needs INSTANCE_OWNER, which the E2E administrator deliberately does not
    // hold — a test suite running with the most powerful role in the system is
    // a test suite that cannot notice a missing permission check.
    const found = await manage<{ id: string; name: string }>(
      "GET",
      `/v1/organizations/${orgId}`,
    );
    await use({ id: found.id, name: found.name });
  },

  project: async ({ organization }, use) => {
    const created = await manage<{ id: string; name: string }>(
      "POST",
      `/v1/organizations/${organization.id}/projects`,
      { name: unique("project") },
    );
    await use({ id: created.id, orgId: organization.id, name: created.name });

    // Deleting the project refuses while anything hangs off it (P1-17), so the
    // application fixture must have cleaned up first. Ordering is why teardown
    // runs in reverse.
    await manage("DELETE", `/v1/organizations/${organization.id}/projects/${created.id}`).catch(
      () => {
        // A project left behind is visible in the next run's list rather than
        // silent, and failing teardown would mask the test's own result.
      },
    );
  },

  application: async ({ organization, project }, use) => {
    const redirectUri = `${process.env.E2E_BASE_URL ?? "http://127.0.0.1:4173"}/auth/callback`;
    const created = await manage<{ id: string }>(
      "POST",
      `/v1/organizations/${organization.id}/projects/${project.id}/applications`,
      {
        name: unique("console"),
        type: "spa",
        redirect_uris: [redirectUri, redirectUri.replace("/auth/callback", "/auth/silent")],
      },
    );

    await use({
      id: created.id,
      projectId: project.id,
      // The application id IS the client id (docs/PLAN/04): one identifier, so
      // a console cannot show the wrong one.
      clientId: created.id,
      redirectUri,
    });

    await manage(
      "DELETE",
      `/v1/organizations/${organization.id}/projects/${project.id}/applications/${created.id}`,
    ).catch(() => {});
  },

  user: async ({ organization }, use) => {
    const email = `${unique("user")}@example.test`;
    const created = await manage<{ id: string }>(
      "POST",
      `/v1/organizations/${organization.id}/users`,
      { email, display_name: "E2E User", send_invite_email: false },
    );

    // The password is set the way a real user sets one: through the invitation
    // link. There is no API that sets a password for somebody else, and that
    // absence is deliberate (`P1-19`) — so the suite proves the invitation
    // flow works on its way to having a user who can log in.
    const password = "Correct-Horse-Battery-Staple-E2E";
    await acceptInvitation(created.id, password);

    await use({ id: created.id, orgId: organization.id, email, password });

    await manage(
      "POST",
      `/v1/organizations/${organization.id}/users/${created.id}/deactivate`,
    ).catch(() => {});
  },
});

/**
 * Completes an invitation without a mailbox.
 *
 * The link is never returned by the API — that is the point of `P1-19` — so a
 * test environment has to read the token from where the service put it. The
 * helper below is the seam: CI supplies `E2E_INVITE_READER`, a small endpoint
 * or script with database access, rather than the suite reaching into the
 * database itself from a browser process.
 */
async function acceptInvitation(userId: string, password: string): Promise<void> {
  const reader = process.env.E2E_INVITE_READER;
  if (reader === undefined || reader === "") {
    throw new Error(
      "E2E_INVITE_READER is required: an invitation link is only ever mailed, " +
        "so a test that needs a usable account has to read the token from the " +
        "environment that sent it. See TASKS/PHASE-1 P1-27.",
    );
  }

  const token = (await (await fetch(`${reader}?user_id=${encodeURIComponent(userId)}`)).text()).trim();
  if (token === "") {
    throw new Error(`no invitation token was found for ${userId}`);
  }

  const form = new URLSearchParams({ token, password, password_confirm: password });
  const page = await fetch(`${issuer}/password/set?token=${encodeURIComponent(token)}`);
  const html = await page.text();
  const csrf = /name="csrf_token" value="([^"]+)"/.exec(html)?.[1];
  const cookie = page.headers.get("set-cookie")?.split(";")[0];
  if (csrf === undefined || cookie === undefined) {
    throw new Error("the set-password page did not issue a CSRF token");
  }
  form.set("csrf_token", csrf);

  const response = await fetch(`${issuer}/password/set`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded", Cookie: cookie },
    body: form,
  });
  if (!response.ok) {
    throw new Error(`setting the password answered ${response.status}`);
  }
}

export { expect } from "@playwright/test";

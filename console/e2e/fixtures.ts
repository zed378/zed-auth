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
const orgId = required("E2E_ORG_ID");
const clientId = required("E2E_CONSOLE_CLIENT_ID");
const refreshToken = required("E2E_BOOTSTRAP_REFRESH_TOKEN");

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
  admin: SeededUser;

  /**
   * A Management API call as the bootstrap administrator.
   *
   * Exposed so a test can assert what the API sees, which is the only way to
   * check that the console wrote where it claimed to (`P1-28`, `docs/PLAN/17`
   * § Phase 1 — "creating via one is visible via the other").
   */
  api: <T>(method: string, path: string, body?: unknown) => Promise<T>;
}

/**
 * The bootstrap administrator's access token, obtained on demand.
 *
 * An access token lives ten minutes (`P1-07`), and a Playwright run does not
 * reliably finish inside ten minutes. Handing the suite a token minted at
 * setup time produces a suite that passes locally and fails in CI at whichever
 * test happens to be running when the clock runs out — which is the shape of
 * flake that gets a test quarantined instead of fixed.
 *
 * So the environment supplies a REFRESH token and this mints access tokens
 * from it. Kept in a variable rather than fetched per call, because a token
 * exchange per API call would make the suite's own load the thing under test.
 */
let accessToken: string | null = null;

async function mintAccessToken(): Promise<string> {
  const response = await fetch(`${issuer}/oauth/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      refresh_token: refreshToken,
      client_id: clientId,
    }),
  });

  if (!response.ok) {
    throw new Error(
      `the bootstrap refresh token was refused (${response.status}). It is ` +
        `minted by scripts/e2e-up.sh; re-run that. ${await response.text()}`,
    );
  }
  const tokens = (await response.json()) as { access_token?: string };
  if (tokens.access_token === undefined) {
    throw new Error("the refresh gave back no access token");
  }
  return tokens.access_token;
}

/**
 * A Management API call as the bootstrap administrator.
 *
 * Retries exactly once on a 401, after minting a fresh token. One retry, not a
 * loop: a 401 that survives a new token is a permission problem, and retrying
 * it would turn a clear failure into a slow one.
 */
async function manage<T>(method: string, path: string, body?: unknown): Promise<T> {
  const send = async (token: string) =>
    fetch(`${issuer}${path}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });

  accessToken ??= await mintAccessToken();
  let response = await send(accessToken);

  if (response.status === 401) {
    accessToken = await mintAccessToken();
    response = await send(accessToken);
  }

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
  api: async ({}, use) => {
    await use(manage);
  },

  /**
   * The bootstrap administrator, for the screens a manager role gates.
   *
   * Not created per test, and not creatable at all through the API: assigning
   * a manager role is `P2`'s work, so this account is seeded by SQL with the
   * rest of the bootstrap (`PG-26`) and `scripts/e2e-up.sh` hands over its
   * credentials.
   *
   * **Most tests should use `user`, not this.** A suite that runs everything
   * as the most powerful account in the system is a suite that cannot notice a
   * missing permission check — which is exactly what `login.spec.ts` asserts
   * with the ordinary user, by URL.
   */
  admin: async ({}, use) => {
    await use({
      id: "",
      orgId,
      email: required("E2E_ADMIN_EMAIL"),
      password: required("E2E_ADMIN_PASSWORD"),
    });
  },

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
    const redirectUri = `${process.env.E2E_BASE_URL ?? "http://localhost:4173"}/auth/callback`;
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
      // The email IS sent, because the token only exists inside it. `P0-15`
      // wrote `false` here when the seam was imagined as a database reader;
      // `user_tokens` stores a SHA-256 and nothing else, so there is nothing
      // for a reader to read. Locally the mail goes to Mailpit, which accepts
      // everything and delivers nothing.
      { email, display_name: "E2E User", send_invite_email: true },
    );

    // The password is set the way a real user sets one: through the invitation
    // link. There is no API that sets a password for somebody else, and that
    // absence is deliberate (`P1-19`) — so the suite proves the invitation
    // flow works on its way to having a user who can log in.
    const password = "Correct-Horse-Battery-Staple-E2E";
    await acceptInvitation(email, password);

    await use({ id: created.id, orgId: organization.id, email, password });

    await manage(
      "POST",
      `/v1/organizations/${organization.id}/users/${created.id}/deactivate`,
    ).catch(() => {});
  },
});

/**
 * Completes an invitation by reading the mailbox the service actually sent to.
 *
 * `P0-15` left this as a seam called `E2E_INVITE_READER` — "a small endpoint or
 * script with database access". `P1-27` implemented it against the mailbox
 * instead, and the reason is that the database **cannot** answer the question:
 * `user_tokens` stores only a SHA-256 of the token, so the plaintext exists in
 * exactly one place, which is the email. A reader with database access would
 * have had to be given the ability to mint a token instead of read one, and a
 * binary in this repository that hands out working invitation links is a worse
 * thing to own than a test that reads a development mailbox.
 *
 * It is also the stronger test. Reading the mail exercises the delivery path —
 * template, recipient, link construction — none of which any other test covers.
 *
 * `E2E_MAILPIT_URL` points at the Mailpit instance in `deploy/docker-compose.yml`.
 * Mailpit is a development mail sink: it accepts everything and delivers
 * nothing, so no message this suite triggers can reach a real address.
 */
async function acceptInvitation(email: string, password: string): Promise<void> {
  const mailpit = process.env.E2E_MAILPIT_URL;
  if (mailpit === undefined || mailpit === "") {
    throw new Error(
      "E2E_MAILPIT_URL is required: an invitation link is only ever mailed, and " +
        "the service stores only a hash of the token — so a test that needs a " +
        "usable account has to read the message. Bring the stack up with " +
        "scripts/e2e-up.sh, which sets it. See TASKS/PHASE-1 P1-27.",
    );
  }

  const token = await readInvitationToken(mailpit, email);

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

/**
 * Finds the invitation message for one address and extracts its token.
 *
 * Polls, because the send happens in the request that created the user and the
 * message lands a moment later. A single immediate read would be flaky in
 * exactly the way that gets a test quarantined rather than fixed.
 *
 * The search is scoped to the address, so two tests running in parallel cannot
 * read each other's invitations — the addresses are unique per fixture.
 */
async function readInvitationToken(mailpit: string, email: string): Promise<string> {
  const deadline = Date.now() + 15_000;
  let lastSeen = "no message arrived";

  while (Date.now() < deadline) {
    const response = await fetch(
      `${mailpit}/api/v1/search?query=${encodeURIComponent(`to:${email}`)}&limit=5`,
    );
    if (response.ok) {
      const found = (await response.json()) as { messages?: { ID: string }[] };
      for (const message of found.messages ?? []) {
        const body = await (await fetch(`${mailpit}/api/v1/message/${message.ID}`)).json();
        const text = `${(body as { Text?: string }).Text ?? ""}`;
        // The link the service built, with the token in its query string.
        // Matched from the URL rather than from prose, so a copy change to the
        // message cannot silently break this into a timeout.
        const token = /[?&]token=([A-Za-z0-9_-]+)/.exec(text)?.[1];
        if (token !== undefined) return token;
        lastSeen = `a message arrived for ${email} with no token link in it`;
      }
    } else {
      lastSeen = `Mailpit answered ${response.status}`;
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }

  throw new Error(`no invitation token for ${email}: ${lastSeen}`);
}

export { expect } from "@playwright/test";

import { expect, test } from "./fixtures";
import type { APIRequestContext, Page } from "@playwright/test";
import type { SeededApplication } from "./fixtures";

/**
 * The MVP's definition of done, in a browser (`docs/PLAN/01` § MVP Definition of
 * Done, `P1-26`, `P1-27` step 3).
 *
 * **Log into application A, open application B, and you are not asked again.**
 *
 * The two applications are the demos in `demo/`: a confidential web client and
 * a public SPA, with different `client_id`s, different origins and different
 * sessions. They are deliberately not the console — the console proving SSO
 * with itself would be one application and one session.
 *
 * `scripts/e2e-up.sh` brings them up and registers them. The URLs arrive as
 * environment variables rather than being hard-coded, because the port a
 * developer has free is not the port CI has free.
 */

const demoA = required("E2E_DEMO_A_URL");
const demoB = required("E2E_DEMO_B_URL");
const issuer = required("E2E_AUTH_ISSUER");

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") {
    throw new Error(
      `${name} is required to run the SSO end-to-end test. Bring the stack up ` +
        `with scripts/e2e-up.sh, which registers both demo applications and ` +
        `exports their URLs. See TASKS/PHASE-1 P1-27.`,
    );
  }
  return value;
}

/**
 * Records every URL the main frame navigates to.
 *
 * This is how "no second login" becomes a fact rather than an inference. A
 * flow that completes without input already implies the login page never
 * appeared — but only if you trust the test would have blocked there, and a
 * test whose evidence is "it did not hang" is one nobody can debug when it
 * starts hanging.
 */
function recordNavigations(page: Page): string[] {
  const seen: string[] = [];
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame()) seen.push(frame.url());
  });
  return seen;
}

const loginPages = (urls: string[]) => urls.filter((url) => new URL(url).pathname === "/login");

async function signInThroughHostedPage(
  page: Page,
  user: { email: string; password: string },
): Promise<void> {
  await expect(page.getByRole("heading", { name: /sign in to/i })).toBeVisible();
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
}

test.describe("single sign-on across two applications", () => {
  test("logging into A means B does not ask again", async ({ page, user }) => {
    // --- Application A: the confidential web client -----------------------
    const firstVisit = recordNavigations(page);

    await page.goto(demoA);
    await page.getByRole("link", { name: /sign in/i }).click();
    await signInThroughHostedPage(page, user);

    await expect(page.getByText(/you are signed in/i)).toBeVisible();
    expect(await page.locator("#subject").textContent()).toBe(user.id);

    // The password was typed exactly once, and this is where.
    expect(loginPages(firstVisit).length).toBe(1);

    // --- Application B: the public SPA, same browser ----------------------
    const secondVisit = recordNavigations(page);

    await page.goto(demoB);
    await page.getByRole("button", { name: /sign in/i }).click();

    // It arrives signed in: same person, different application, no prompt.
    await expect(page.getByText(/you are signed in/i)).toBeVisible();
    expect(await page.locator("#subject").textContent()).toBe(user.id);

    // **The assertion this whole task exists for.** Not "it did not hang" —
    // the browser never visited the login page at all on the way through B.
    expect(loginPages(secondVisit)).toEqual([]);

    // And it went through the provider rather than reading application A's
    // session directly, which would be single sign-on in name only.
    expect(secondVisit.some((url) => new URL(url).pathname === "/oauth/authorize")).toBe(true);
  });

  test("signing out of A ends the shared session, so B asks again", async ({ page, user }) => {
    await page.goto(demoA);
    await page.getByRole("link", { name: /sign in/i }).click();
    await signInThroughHostedPage(page, user);
    await expect(page.getByText(/you are signed in/i)).toBeVisible();

    // Application A's sign-out ends the provider's session too, not only its
    // own. A logout that leaves the SSO session alive means the next click on
    // "sign in" signs straight back in, which reads as a broken logout — and
    // is the failure that makes people stop trusting the button.
    await page.getByRole("link", { name: /sign out/i }).click();

    const afterLogout = recordNavigations(page);
    await page.goto(demoB);
    await page.getByRole("button", { name: /sign in/i }).click();

    // B now meets the login page. This is the inverse of the first test, and
    // without it that test passes just as well against a service that never
    // ends a session at all.
    await expect(page.getByRole("heading", { name: /sign in to/i })).toBeVisible();
    expect(loginPages(afterLogout).length).toBe(1);
  });

  test("application B refuses a token minted for another application", async ({
    page,
    user,
    application,
  }) => {
    // The other half of `P1-26`'s definition of done, at the HTTP layer.
    //
    // The token is minted for the CONSOLE's application — a public client, so
    // it can be exchanged without a secret. Application A cannot be used for
    // this: it is confidential, and a browser has no way to authenticate as
    // it, which is the property that makes it confidential.
    await page.goto(demoA);
    await page.getByRole("link", { name: /sign in/i }).click();
    await signInThroughHostedPage(page, user);
    await expect(page.getByText(/you are signed in/i)).toBeVisible();

    // Playwright's request context, not the page: it shares the browser's
    // cookies (so the provider sees the session just established) and is not
    // subject to CORS, which a cross-origin `fetch` from the page would be.
    const api = page.context().request;
    const elsewhere = await mintIdToken(api, application);

    const refused = await api.get(`${demoB}/api/me`, {
      headers: {
        Authorization: `Bearer ${elsewhere}`,
        // Deliberately wrong, and it does not matter: the audience is checked
        // before the nonce, so this proves the audience check rather than
        // passing for the wrong reason.
        "X-Demo-Expected-Nonce": "not-the-nonce",
      },
    });
    const body = await refused.text();

    expect(refused.status()).toBe(401);
    expect(body).toMatch(/audience/i);
    // And the refusal does not echo the credential back.
    expect(body).not.toContain(elsewhere);

    // **The control.** Without it, an application B that refused every token —
    // because it was misconfigured, or down, or checking nothing and failing
    // closed — would pass the assertions above just as well.
    //
    // B is a public client and publishes its own client_id, so a token
    // genuinely addressed to it can be minted the same way.
    const self = (await (await api.get(`${demoB}/config.json`)).json()) as {
      client_id: string;
      redirect_uri: string;
    };
    const addressedToB = await mintIdToken(
      api,
      { clientId: self.client_id, redirectUri: self.redirect_uri },
      "a-known-nonce",
    );

    const accepted = await api.get(`${demoB}/api/me`, {
      headers: {
        Authorization: `Bearer ${addressedToB}`,
        "X-Demo-Expected-Nonce": "a-known-nonce",
      },
    });
    expect(accepted.status()).toBe(200);
    expect((await accepted.json()).subject).toBe(user.id);
  });
});

/**
 * Runs the Authorization Code flow for one registered application and returns
 * its ID token.
 *
 * A real token, minted by the provider for that `client_id` — not one
 * assembled by hand. A hand-made token would prove only that the verifier
 * rejects nonsense, which is the easy half.
 */
async function mintIdToken(
  api: APIRequestContext,
  application: Pick<SeededApplication, "clientId" | "redirectUri">,
  nonce = "e2e-nonce",
): Promise<string> {
  const verifier = "e2e-verifier-".padEnd(64, "0123456789abcdef");
  const challenge = await sha256Base64Url(verifier);

  const authorized = await api.get(`${issuer}/oauth/authorize`, {
    params: {
      response_type: "code",
      client_id: application.clientId,
      redirect_uri: application.redirectUri,
      scope: "openid",
      state: "e2e-state",
      nonce,
      code_challenge: challenge,
      code_challenge_method: "S256",
    },
    // Manual, so the code is read out of the Location header rather than
    // followed into the console, which would consume it.
    maxRedirects: 0,
  });

  const location = authorized.headers()["location"] ?? "";
  const code = new URL(location, issuer).searchParams.get("code");
  if (code === null) {
    throw new Error(`no authorization code came back (status ${authorized.status()}): ${location}`);
  }

  const tokens = await (
    await api.post(`${issuer}/oauth/token`, {
      form: {
        grant_type: "authorization_code",
        code,
        client_id: application.clientId,
        redirect_uri: application.redirectUri,
        code_verifier: verifier,
      },
    })
  ).json();

  if (typeof tokens.id_token !== "string") {
    throw new Error(`no id_token came back: ${JSON.stringify(tokens)}`);
  }
  return tokens.id_token;
}

async function sha256Base64Url(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Buffer.from(digest).toString("base64url");
}

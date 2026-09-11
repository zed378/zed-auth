import { defineConfig, devices } from "@playwright/test";

/**
 * End-to-end tests for the console — the top layer of `docs/PLAN/11`'s pyramid,
 * below the security tests.
 *
 * `docs/PLAN/11` § End-to-End names what these will cover once there is a login
 * flow to cover: successful login and redirect, SSO across two applications,
 * MFA rejection, and logout genuinely ending a session. `docs/PLAN/06` § Testing
 * adds the console flows — invite plus first role assignment, Project Grant
 * creation and revocation, session revocation.
 *
 * None of that exists in Phase 0. What exists is the harness, a fixture layer
 * for the objects those tests will need, and one real test of the shell —
 * enough that `P0-15`'s "no Phase 1 task can plead there is no harness for
 * that yet" actually holds.
 */
export default defineConfig({
  testDir: "./e2e",

  // A failing E2E test is usually a real failure, and a retried one hides a
  // race rather than fixing it. Retries only in CI, where an infrastructure
  // blip is a plausible cause and a human is not watching.
  retries: process.env.CI ? 2 : 0,

  // Fail the run if a test is left focused. `test.only` committed by accident
  // turns a suite of thirty into a suite of one, and it stays green.
  forbidOnly: !!process.env.CI,

  fullyParallel: true,
  workers: process.env.CI ? 2 : undefined,

  reporter: process.env.CI
    ? [["github"], ["html", { open: "never" }]]
    : [["list"]],

  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:4173",

    // Traces on the first retry only: a trace per test is gigabytes, and the
    // one that matters is the one from a test that just failed.
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "off",
  },

  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },

    // The console is a desktop tool (`docs/UI-UX/00`, `docs/UI-UX/12`), so there is no
    // mobile project here. The one screen group that must work on a phone —
    // personal account settings, `docs/UI-UX/16` — gets its own project when it is
    // built, rather than a mobile run of screens the plan says are desktop.
  ],

  // Serves the production build rather than the dev server: an E2E suite that
  // passes against `vite dev` and has never seen the built bundle is testing
  // something nobody deploys.
  webServer: {
    // Bound on every interface, addressed as `localhost`.
    //
    // Two separate lessons are encoded here and they pull in opposite
    // directions.
    //
    // Vite's preview server binds `localhost`, which on this machine resolves
    // to ::1 first, while a probe dialling 127.0.0.1 waits two minutes for a
    // server that started immediately and is answering on the other address
    // family. So the bind is explicit.
    //
    // But the ADDRESS has to be `localhost`, not `127.0.0.1`, because those
    // are different SITES to a browser. The console's silent renewal runs
    // `prompt=none` in an iframe against the issuer, and a third-party iframe
    // does not receive a `SameSite=Lax` session cookie — so a console at
    // `127.0.0.1:4173` talking to an issuer at `localhost:8080` can never
    // renew, and the suite reports a broken console when what is broken is
    // the test environment. Deployed, both live under one registrable domain
    // and are same-site (ADR-019 assumed as much and did not say so).
    command: "npm run preview -- --host 0.0.0.0 --port 4173 --strictPort",
    url: "http://localhost:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});

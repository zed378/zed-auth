import { defineConfig, devices } from "@playwright/test";

/**
 * End-to-end tests for the console — the top layer of `PLAN/11`'s pyramid,
 * below the security tests.
 *
 * `PLAN/11` § End-to-End names what these will cover once there is a login
 * flow to cover: successful login and redirect, SSO across two applications,
 * MFA rejection, and logout genuinely ending a session. `PLAN/06` § Testing
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

    // The console is a desktop tool (`UI-UX/00`, `UI-UX/12`), so there is no
    // mobile project here. The one screen group that must work on a phone —
    // personal account settings, `UI-UX/16` — gets its own project when it is
    // built, rather than a mobile run of screens the plan says are desktop.
  ],

  // Serves the production build rather than the dev server: an E2E suite that
  // passes against `vite dev` and has never seen the built bundle is testing
  // something nobody deploys.
  webServer: {
    // `--host 127.0.0.1` explicitly.
    //
    // Vite's preview server binds `localhost`, which on this machine resolves
    // to ::1 first, while the readiness probe below dials 127.0.0.1 — so
    // Playwright waits two minutes for a server that started immediately and
    // is answering on the other address family. The nginx healthcheck for the
    // console preview hit the same thing an hour earlier; naming the address
    // on both sides is the fix in both places.
    command: "npm run preview -- --host 127.0.0.1 --port 4173 --strictPort",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});

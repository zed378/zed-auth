import { defineConfig, devices } from "@playwright/test";

// The workbench is deliberately absent from the production build.
export default defineConfig({
  testDir: "./e2e",
  testMatch: "workbench.spec.ts",
  workers: 1,
  timeout: 60_000,
  reporter: "list",
  use: { ...devices["Desktop Chrome"], baseURL: "http://127.0.0.1:5173" },
  webServer: {
    command: "node node_modules/vite/bin/vite.js --host 127.0.0.1",
    timeout: 120_000,
    url: "http://127.0.0.1:5173/workbench.html",
    reuseExistingServer: true,
  },
});

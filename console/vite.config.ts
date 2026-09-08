/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// PLAN/06-FRONTEND-ARCHITECTURE.md: a static SPA build served from a CDN,
// calling the Management API over HTTPS. Deliberately decoupled from the
// backend deploy — a console fix must not require shipping the auth service,
// and vice versa.
export default defineConfig({
  plugins: [react(), tailwindcss()],

  build: {
    outDir: "dist",
    // Fail the build rather than silently shipping a chunk large enough to
    // hurt first paint on the login redirect.
    chunkSizeWarningLimit: 600,
    sourcemap: true,
  },

  server: {
    port: 5173,
    strictPort: true,
  },

  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});

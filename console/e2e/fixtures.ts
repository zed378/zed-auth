import { test as base } from "@playwright/test";

/**
 * Fixtures for console E2E tests.
 *
 * `P0-15` step 3 asks for "the fixtures Phase 1 will need: seed an org, a
 * project, an application, and a user". The shapes are defined here now so
 * that a Phase 1 test declares what it needs rather than inventing a seeding
 * approach per file — which is how two tests end up with two different ideas
 * of what a seeded organization looks like.
 *
 * The seeding itself cannot be written yet: the Management API endpoints that
 * would create these objects arrive in `P1-15`. Rather than a mock that would
 * have to be thrown away, each fixture fails with the task that unblocks it.
 * A fixture that silently returned fake data would let a Phase 1 test pass
 * against nothing.
 */

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

function notYet(what: string, task: string): never {
  throw new Error(
    `The ${what} fixture needs the Management API, which arrives in ${task}.\n\n` +
      `It is deliberately unimplemented rather than mocked: a fixture returning ` +
      `fake data would let a test pass against nothing, which is worse than a ` +
      `test that cannot run yet.`,
  );
}

export const test = base.extend<Fixtures>({
  organization: async ({}, use) => {
    notYet("organization", "P1-15");
    await use(undefined as never);
  },

  project: async ({}, use) => {
    notYet("project", "P1-15");
    await use(undefined as never);
  },

  application: async ({}, use) => {
    notYet("application", "P1-15");
    await use(undefined as never);
  },

  user: async ({}, use) => {
    notYet("user", "P1-15");
    await use(undefined as never);
  },
});

export { expect } from "@playwright/test";

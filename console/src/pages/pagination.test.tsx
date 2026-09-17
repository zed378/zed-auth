import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OverviewPage } from "./OverviewPage";
import { RolesPage } from "./RolesPage";
import { UsersPage } from "./UsersPage";
import { MAX_PAGES, collectPages } from "../lib/api/queries";
import { AuthError } from "../lib/auth/oidc";
import { clearToken } from "../lib/auth/tokens";
import { renderScreen, signIn, stubApi } from "../test/harness";

/**
 * Lists follow `page_info` (found by `P4-05`'s full E2E run).
 *
 * Every list hook asked for one page of 100 and ignored the token. Collections
 * are ordered oldest first, so the rows that went missing were the newest: an
 * administrator could not find the user they had just invited, and the Overview
 * counted 100 active users in an organization of 128.
 */

vi.mock("../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../lib/auth/oidc")>("../lib/auth/oidc");
  return {
    ...actual,
    renewSilently: () => Promise.reject(new AuthError("login_required", null)),
  };
});

const user = (n: number) => ({
  id: `u${n}`,
  email: `user${n}@example.test`,
  display_name: `User ${n}`,
  status: "active",
  email_verified: true,
});

beforeEach(() => {
  clearToken();
  sessionStorage.clear();
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
  signIn();
});

describe("collectPages", () => {
  it("follows tokens to the end and reports the collection complete", async () => {
    const pages: Record<string, { items: number[]; next?: string }> = {
      first: { items: [1, 2], next: "b" },
      b: { items: [3], next: "c" },
      c: { items: [4] },
    };
    const seen: (string | undefined)[] = [];

    const all = await collectPages(async (token) => {
      seen.push(token);
      const page = pages[token ?? "first"];
      return { items: page.items, next: page.next };
    });

    expect(all).toEqual({ items: [1, 2, 3, 4], complete: true });
    expect(seen).toEqual([undefined, "b", "c"]);
  });

  it("stops at the bound and says the collection is incomplete", async () => {
    let calls = 0;
    const all = await collectPages(async () => {
      calls++;
      return { items: [calls], next: "again" };
    });

    expect(calls).toBe(MAX_PAGES);
    expect(all.complete).toBe(false);
  });
});

describe("the users list", () => {
  it("shows a user who is only on the second page", async () => {
    stubApi((url) =>
      url.includes("page_token=p2")
        ? { status: 200, body: { users: [user(101)] } }
        : { status: 200, body: { users: [user(1)], page_info: { next_page_token: "p2" } } },
    );

    renderScreen(<UsersPage />);

    // The newest user — the one just invited — was the one that went missing.
    expect(await screen.findByText("User 101")).toBeInTheDocument();
    expect(screen.getByText("User 1")).toBeInTheDocument();
    expect(screen.queryByText(/Showing the first/)).not.toBeInTheDocument();
  });

  it("says the list is incomplete when the bound is reached", async () => {
    let n = 0;
    stubApi(() => ({
      status: 200,
      body: { users: [user(++n)], page_info: { next_page_token: `p${n}` } },
    }));

    renderScreen(<UsersPage />);

    expect(await screen.findByRole("status")).toHaveTextContent(
      `Showing the first ${MAX_PAGES} users, oldest first. Search by name or email to find anyone else.`,
    );
  });
});

describe("the overview", () => {
  it("counts users beyond the first page, and marks a bounded count as a lower bound", async () => {
    let n = 0;
    stubApi((url) => {
      if (url.includes("/users")) {
        n++;
        return {
          status: 200,
          body: { users: [user(n)], page_info: { next_page_token: `p${n}` } },
        };
      }
      if (url.includes("/projects")) return { status: 200, body: { projects: [] } };
      if (url.includes("/events")) return { status: 200, body: { events: [] } };
      return { status: 200, body: { id: "org-1", name: "Acme" } };
    });

    renderScreen(<OverviewPage />);

    // The card becomes a link once its data arrives; the loading card is a
    // different element, so it is found after the fact.
    const card = await screen.findByRole("link", { name: /Active users/ });
    expect(card).toHaveTextContent(`Active users${MAX_PAGES}+ (at least)`);
    expect(within(card).getByText("(at least)")).toBeInTheDocument();
  });
});

describe("the roles list", () => {
  it("includes roles past the first page, so the search can find them", async () => {
    const role = (key: string) => ({
      id: key,
      project_id: "p1",
      key,
      display_name: key,
      permission_keys: [],
      is_builtin: false,
      grant_count: 0,
      created_at: "2026-09-01T00:00:00Z",
      updated_at: "2026-09-01T00:00:00Z",
    });
    stubApi((url) => {
      if (!url.includes("/roles")) return { status: 200, body: { projects: [{ id: "p1", name: "Till" }] } };
      return url.includes("page_token=r2")
        ? { status: 200, body: { roles: [role("zebra")] } }
        : { status: 200, body: { roles: [role("alpha")], page_info: { next_page_token: "r2" } } };
    });

    renderScreen(<RolesPage />, "/projects/p1/roles", "/projects/:projectId/roles");

    expect(await screen.findByText("zebra", { selector: "code" })).toBeInTheDocument();
  });
});

import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { expectNoAxeViolations } from "../../test/axe";
import { AppShell } from "./AppShell";
import { AuthProvider } from "../../lib/auth/AuthProvider";
import { AuthError } from "../../lib/auth/oidc";
import { clearToken, storeToken } from "../../lib/auth/tokens";

/**
 * The shell renders the session bar, so it needs the auth context — in the
 * real application it always has one, and useAuth throwing without a provider
 * is the wiring check that says so.
 *
 * The silent renewal is stubbed to report "no session", which is the shell's
 * anonymous state: the session bar renders nothing, and every landmark
 * assertion below is about the shell rather than about a signed-in header.
 * The signed-in variant has its own test at the end.
 */
vi.mock("../../lib/auth/oidc", async () => {
  const actual = await vi.importActual<typeof import("../../lib/auth/oidc")>(
    "../../lib/auth/oidc",
  );
  return { ...actual, renewSilently: () => Promise.reject(new AuthError("login_required", null)) };
});

beforeEach(() => {
  // Without this the provider reports "not configured" and the session bar
  // renders nothing — which is correct behaviour and would have made the
  // signed-in test below fail for a reason that has nothing to do with the
  // shell.
  vi.stubEnv("VITE_AUTH_CLIENT_ID", "console-test-client");
  vi.stubEnv("VITE_AUTH_ISSUER", "https://auth.example.test");
});

function renderShell(children = <h1>Page</h1>) {
  return render(
    <MemoryRouter>
      <AuthProvider>
        <AppShell>{children}</AppShell>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe("landmarks", () => {
  // docs/UI-UX/13: accessibility is built in from the first screen rather than
  // retrofitted. The shell's landmarks are what every future page inherits, so
  // a missing one here is a defect on every screen at once rather than on this
  // one.
  it("provides exactly one navigation, main and contentinfo landmark", () => {
    renderShell();

    expect(screen.getAllByRole("navigation")).toHaveLength(1);
    expect(screen.getAllByRole("main")).toHaveLength(1);
    expect(screen.getAllByRole("contentinfo")).toHaveLength(1);
  });

  it("labels the navigation", () => {
    // A document may hold more than one navigation landmark. Two entries
    // reading "navigation" in a screen reader's landmark list say nothing
    // about which is which.
    expect(renderShell().container.querySelector("nav")).toHaveAttribute(
      "aria-label",
      "Console",
    );
  });

  it("renders page content inside main", () => {
    renderShell(<h1>Overview</h1>);

    expect(
      within(screen.getByRole("main")).getByRole("heading", { name: "Overview" }),
    ).toBeInTheDocument();
  });
});

describe("skip link", () => {
  // docs/UI-UX/13 § Keyboard Navigation. Without it a keyboard user tabs the whole
  // navigation on every page load before reaching what they came for.
  it("is the first thing a keyboard user reaches", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.tab();

    expect(screen.getByRole("link", { name: /skip to main content/i })).toHaveFocus();
  });

  it("points at a main region that can actually receive focus", () => {
    // The bug this catches is the common one: a skip link that moves the
    // scroll position but not the focus, so the next Tab resumes from the
    // navigation. It looks implemented and does nothing.
    //
    // jsdom does not implement fragment navigation, so this asserts the two
    // halves of the contract — the href's target exists, and that target is
    // programmatically focusable — rather than simulating the jump.
    renderShell();

    const link = screen.getByRole("link", { name: /skip to main content/i });
    const href = link.getAttribute("href");
    expect(href).toBe("#main-content");

    const main = screen.getByRole("main");
    expect(main).toHaveAttribute("id", "main-content");
    expect(main).toHaveAttribute("tabindex", "-1");
  });

  it("is hidden visually, not removed from the accessibility tree", () => {
    // The other common way to get this wrong: `hidden` or `display: none`
    // takes the link out of the tab order, so it is invisible to exactly the
    // users it exists for.
    //
    // jsdom does not apply the stylesheet, so the assertion is on the class
    // that produces the behaviour. `sr-only` keeps an element rendered and
    // focusable while clipping it to a pixel; `hidden` does not.
    renderShell();

    const link = screen.getByRole("link", { name: /skip to main content/i });

    expect(link.className).toMatch(/\bsr-only\b/);
    expect(link.className).not.toMatch(/(^|\s)hidden(\s|$)/);
    expect(link).not.toHaveAttribute("hidden");
    expect(link).not.toHaveAttribute("aria-hidden", "true");
  });
});

describe("navigation", () => {
  // docs/PLAN/06 § Information Architecture. The tree mirrors docs/PLAN/04's data model
  // on purpose, so this list changing is a signal that one of them moved.
  it("renders the organization destinations from docs/PLAN/06", () => {
    renderShell();
    const nav = screen.getByRole("navigation");

    // Asserted on hrefs rather than accessible names. Names are the wrong key
    // here for two reasons: a link's name includes its phase badge, and
    // "Projects" is a substring of "Granted Projects", so a name-based lookup
    // matches two links and fails for a reason that has nothing to do with the
    // information architecture this test is about.
    const destinations = within(nav)
      .getAllByRole("link")
      .map((link) => link.getAttribute("href"));

    expect(destinations).toEqual([
      "/",
      "/projects",
      "/users",
      "/granted-projects",
      "/policies",
      "/audit-log",
      "/settings",
    ]);
  });

  it("gives every destination a visible label", () => {
    renderShell();
    const nav = screen.getByRole("navigation");

    for (const label of [
      "Overview",
      "Projects",
      "Users",
      "Granted Projects",
      "Policies",
      "Audit Log",
      "Settings",
    ]) {
      expect(within(nav).getByText(label)).toBeVisible();
    }
  });

  it("does not show the instance-owner section yet", () => {
    // docs/PLAN/06 puts Instance administration behind INSTANCE_OWNER. The console
    // cannot read a role claim until P1-03, and showing it to everyone
    // meanwhile would teach the wrong thing about what the console is.
    const nav = renderShell().container.querySelector("nav")!;

    expect(within(nav).queryByText(/instance/i)).toBeNull();
  });

  it("reaches every navigation link by keyboard", async () => {
    // docs/UI-UX/13: every interactive element is reachable and operable by
    // keyboard alone. Tabbing from the skip link should walk the whole nav.
    const user = userEvent.setup();
    renderShell();

    const links = within(screen.getByRole("navigation")).getAllByRole("link");

    await user.tab(); // skip link
    for (const link of links) {
      await user.tab();
      expect(link).toHaveFocus();
    }
  });
});

describe("the narrow-width message replaces the app rather than covering it", () => {
  // Two <h1> elements in one document is a heading-structure defect, and the
  // first version had exactly that: the width message's heading plus the page
  // heading. Worse, at narrow width a screen reader user would have walked
  // past the message into the application it says is unusable, because a
  // visual overlay hides nothing from assistive technology.
  it("keeps the message and the application in separate subtrees", () => {
    // The invariant that makes the CSS swap correct, and the one that jsdom
    // can actually check: jsdom applies no stylesheet, so both subtrees are
    // present here regardless of width. What must be true at every width is
    // that they are siblings — if the application were nested inside the
    // message (or the reverse), no media query could show one without the
    // other, and the browser would expose two <h1> elements at once.
    const { container } = renderShell(<h1>Overview</h1>);

    const message = screen.getByText(/too narrow/i).closest("div")!;
    const main = screen.getByRole("main");

    expect(message.contains(main)).toBe(false);
    expect(main.contains(message)).toBe(false);
    expect(container.querySelectorAll("h1")).toHaveLength(2);
  });

  it("gives each subtree its own single heading", () => {
    // One <h1> per subtree, so whichever the media query exposes, the document
    // a screen reader sees has exactly one top-level heading.
    renderShell(<h1>Overview</h1>);

    const message = screen.getByText(/too narrow/i).closest("div")!;
    expect(message.querySelectorAll("h1")).toHaveLength(1);

    const main = screen.getByRole("main");
    expect(main.querySelectorAll("h1")).toHaveLength(1);
  });
});

describe("automated accessibility check", () => {
  // docs/UI-UX/13 § Testing & Sign-off asks for automated accessibility linting in
  // CI, catching regressions on every change. axe is not a substitute for the
  // manual screen-reader pass that document also requires before Phase 5 — it
  // catches the mechanical failures, not the ones that need judgement.
  it("reports no violations for the shell", async () => {
    const { container } = renderShell();

    await expectNoAxeViolations(container);
  });
});

describe("the session bar", () => {
  it("shows nothing when nobody is signed in", async () => {
    renderShell();

    // The anonymous state. A "sign out" control here would be an affordance
    // for something that has not happened.
    expect(screen.queryByRole("button", { name: /sign out/i })).not.toBeInTheDocument();
  });

  it("names the signed-in user and offers a way out", async () => {
    const payload = btoa(JSON.stringify({ sub: "user-1", org_id: "org-1", roles: ["ORG_ADMIN"] }))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
    storeToken(`header.${payload}.signature`, 3600);

    renderShell();

    expect(await screen.findByText("user-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out/i })).toBeInTheDocument();

    clearToken();
  });
});

import { expect, test } from "@playwright/test";

/**
 * The E2E layer's example test — `docs/PLAN/11`'s pyramid, top tier.
 *
 * It exercises the console shell, which is what Phase 0 actually ships. Not a
 * placeholder that asserts `true`: `P0-15`'s Definition of Done asks for "one
 * passing example test at each pyramid layer", and a test that would pass
 * against a blank page is not an example of anything.
 *
 * What this covers that the jsdom tests cannot: a real browser applying the
 * real stylesheet to the real production bundle. The console's design tokens
 * were once all defined, correctly named, and generating no CSS at all —
 * every jsdom test passed throughout, because jsdom does not apply
 * stylesheets. This layer is where that class of failure becomes visible.
 */

test.describe("the console shell", () => {
  test("renders, and lets a keyboard user skip to the content", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveTitle(/Zed Auth Console/i);

    // docs/UI-UX/13 § Keyboard Navigation: the skip link is the first stop, and
    // without it a keyboard user tabs the whole navigation on every page load.
    await page.keyboard.press("Tab");

    const skipLink = page.getByRole("link", { name: /skip to main content/i });
    await expect(skipLink).toBeFocused();

    // The ring is a token, not a hard-coded colour (docs/UI-UX/05 § Governance).
    const outline = await skipLink.evaluate((el) => {
      const style = getComputedStyle(el);
      return `${style.outlineWidth} ${style.outlineStyle}`;
    });
    expect(outline).toBe("2px solid");
  });

  test("applies the design tokens to the built bundle", async ({ page }) => {
    await page.goto("/");

    // The regression this exists for. `text-body` was defined as
    // `--font-size-body`, which is not a Tailwind namespace, so the class
    // generated nothing and every piece of text fell back to the browser
    // default of 16px. The console's body size is 14px.
    const bodyFontSize = await page.evaluate(
      () => getComputedStyle(document.body).fontSize,
    );
    expect(bodyFontSize).toBe("14px");

    const accent = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue("--color-accent").trim(),
    );
    expect(accent).toBeTruthy();
  });

  test("navigates between destinations without a full page load", async ({ page }) => {
    await page.goto("/");

    await page.getByRole("navigation").getByText("Users", { exact: true }).click();

    await expect(page).toHaveURL(/\/users$/);
    await expect(page.getByRole("main").getByRole("heading", { level: 1 })).toHaveText(
      "Users",
    );
  });

  test("a deep link loads directly", async ({ page }) => {
    // The bug this catches shipped once: a static host serves /projects as a
    // file, does not find one, and returns 404 — so the application that would
    // have routed it never loads. It works while clicking around inside the
    // app and breaks on every refresh and bookmark.
    const response = await page.goto("/projects");

    expect(response?.status()).toBe(200);
    await expect(page.getByRole("main").getByRole("heading", { level: 1 })).toHaveText(
      "Projects",
    );
  });
});

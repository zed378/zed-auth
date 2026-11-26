import { test, expect } from "@playwright/test";

for (const [width, tracks, primary, supporting, gap] of [[1440, 12, 9, 3, 24], [1200, 12, 8, 4, 16], [900, 8, 8, 8, 16], [599, 4, 4, 4, 16]]) {
  test(`responsive grid occupancy at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/workbench.html");
    const grid = page.getByLabel("Responsive grid example").first();
    const geometry = await grid.evaluate((element) => {
      const style = getComputedStyle(element);
      return {
        tracks: style.gridTemplateColumns.split(" ").length,
        gap: Number.parseFloat(style.columnGap),
        spans: [...element.children].map((child) => {
          const region = getComputedStyle(child);
          return [region.gridColumnStart, region.gridColumnEnd];
        }),
      };
    });
    expect(geometry.tracks).toBe(tracks);
    expect(geometry.gap).toBe(gap);
    expect(geometry.spans).toEqual([
      [`span ${primary}`, `span ${primary}`],
      [`span ${supporting}`, `span ${supporting}`],
    ]);
  });
}

test("admin fixture enforces the 768px width boundary", async ({ page }) => {
  await page.goto("/workbench.html");
  const fixture = page.getByText("Admin fixture is supported at tablet width and above.", { exact: true }).first();
  await page.setViewportSize({ width: 767, height: 900 });
  await expect(fixture).toBeHidden();
  await page.setViewportSize({ width: 768, height: 900 });
  await expect(fixture).toBeVisible();
});

test("tablet button and dialog close targets meet the spacing token minimum", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 900 });
  await page.goto("/workbench.html");
  const opener = page.getByRole("button", { name: "Open modal", exact: true }).first();
  const minimum = await page.evaluate(() => {
    const probe = document.createElement("div");
    probe.style.height = "var(--spacing-7)";
    document.body.append(probe);
    const height = probe.getBoundingClientRect().height;
    probe.remove();
    return height;
  });
  expect(minimum).toBeGreaterThan(0);
  const target = await opener.boundingBox();
  expect(target!.width).toBeGreaterThanOrEqual(minimum);
  expect(target!.height).toBeGreaterThanOrEqual(minimum);
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "Project details" });
  await expect(dialog).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 0)");
  const close = dialog.getByRole("button", { name: "Close dialog" });
  const closeBounds = await close.boundingBox();
  expect(closeBounds!.width).toBeGreaterThanOrEqual(minimum);
  expect(closeBounds!.height).toBeGreaterThanOrEqual(minimum);
});

test("checkbox and radio controls keep a 24px target at tablet width", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 900 });
  await page.goto("/workbench.html");
  for (const controls of [
    page.getByRole("checkbox", { name: "Notify by email" }),
    page.getByRole("radio", { name: "Viewer" }),
  ]) {
    const instances = await controls.all();
    expect(instances.length).toBeGreaterThan(0);
    for (const control of instances) {
      const bounds = await control.boundingBox();
      expect(bounds).not.toBeNull();
      expect(bounds!.width).toBeGreaterThanOrEqual(24);
      expect(bounds!.height).toBeGreaterThanOrEqual(24);
    }
  }
});

test("form review retains drafts and server errors focus their field", async ({ page }) => {
  await page.goto("/workbench.html");
  const forms = page.locator("section").filter({ has: page.getByRole("heading", { name: "Form System", exact: true }) }).first();
  const wizard = forms.locator("div.mt-4").filter({ has: page.getByRole("heading", { name: "multi-step-review", exact: true }) });
  await wizard.getByRole("textbox", { name: "Display name" }).fill("Accounts team");
  await wizard.getByRole("textbox", { name: "Role key" }).fill("accounts_admin");
  await wizard.getByRole("button", { name: "Review details" }).click();
  await expect(wizard).toContainText("Accounts team");
  await wizard.getByRole("button", { name: "Back", exact: true }).click();
  await expect(wizard.getByRole("textbox", { name: "Display name" })).toHaveValue("Accounts team");
  const server = forms.locator("div.mt-4").filter({ has: page.getByRole("heading", { name: "server-field-error", exact: true }) });
  await server.getByRole("textbox", { name: "Display name" }).fill("Accounts team");
  await server.getByRole("textbox", { name: "Role key" }).fill("accounts_admin");
  await server.getByRole("button", { name: "Save example", exact: true }).click();
  await expect(server.getByRole("textbox", { name: "Role key" })).toBeFocused();
  await expect(server).toContainText("This role key is reserved.");
  await expect(server.getByRole("textbox", { name: "Display name" })).toHaveValue("Accounts team");
});

test("typed confirmation requires the exact name and acknowledges completion", async ({ page }) => {
  await page.goto("/workbench.html");
  const opener = page.getByRole("button", { name: "Preview typed confirmation", exact: true }).first();
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "Revoke project grant?" });
  const confirm = dialog.getByRole("button", { name: "Revoke grant", exact: true });
  await expect(confirm).toBeDisabled();
  await expect(dialog).toHaveCSS("animation-duration", "0s");
  const name = dialog.getByRole("textbox", { name: "Type Acme to confirm" });
  await name.fill("acme");
  await expect(confirm).toBeDisabled();
  await name.fill("Acme");
  await confirm.click();
  await expect(dialog.getByRole("status")).toContainText("Example grant revoked.");
  await dialog.getByRole("button", { name: "Done", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  await opener.click();
  await expect(confirm).toBeDisabled();
});

for (const width of [1440, 1200, 900, 600]) {
  test(`dialog geometry and keyboard at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/workbench.html");
    const opener = page.getByRole("button", { name: "Open side panel", exact: true }).first();
    await opener.scrollIntoViewIfNeeded();
    const before = await opener.boundingBox();
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "Invite user" });
    await expect(dialog).toBeFocused();
    await expect(dialog).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 0)");
    const bounds = await dialog.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.width).toBe(width < 1024 ? width : 448);
    expect(bounds!.height).toBe(900);
    await expect(page.locator("body")).toHaveCSS("overflow", "hidden");
    const after = await opener.boundingBox();
    expect(after!.x).toBeCloseTo(before!.x, 0);
    expect(after!.width).toBeCloseTo(before!.width, 0);
    await dialog.getByRole("textbox", { name: "Name" }).fill("Example project");
    await expect(dialog.getByRole("textbox", { name: "Name" })).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    await expect(page.locator("body")).not.toHaveCSS("overflow", "hidden");
  });
}

test("reduced motion and branded dialogs preserve the instant fallback", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/workbench.html");
  await page.getByLabel("Organization accent (hex)").fill("#005a9c");
  await page.getByRole("button", { name: "Open modal", exact: true }).last().click();
  const dialog = page.getByRole("dialog", { name: "Project details" });
  await expect(dialog).toBeFocused();
  await expect(dialog).toHaveCSS("transition-duration", "0s");
  await expect(dialog).toHaveCSS("animation-duration", "0s");
  await dialog.getByRole("button", { name: "Close dialog" }).click();
  await expect(dialog).toHaveCount(0);
});

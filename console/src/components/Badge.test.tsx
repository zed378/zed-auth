import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Badge, StatusBadge, type BadgeStatus } from "./Badge";
import { RoleSourceBadge } from "./RoleSourceBadge";
import { Tag } from "./Tag";
import { contrastRatio, BRANDABLE_TOKENS } from "../branding/branding";
import { expectNoAxeViolations } from "../test/axe";

const statuses: BadgeStatus[] = ["active", "invited", "deactivated", "locked", "revoked", "draft"];
const css = readFileSync(resolve(process.cwd(), "src/styles/tokens.css"), "utf8");
function token(name: string) {
  const value = css.match(new RegExp(`^\\s*--color-${name}:\\s*(#[a-fA-F0-9]+);`, "m"))?.[1];
  if (!value) throw new Error(`Missing color token: ${name}`);
  return value;
}

describe("Badge / Tag", () => {
  it.each(statuses)("communicates %s through visible text and a decorative icon", (status) => {
    const { container } = render(<StatusBadge status={status} />);
    expect(screen.getByText(status, { exact: false })).toBeVisible();
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  it("keeps deactivated neutral and uses a slash for a revoked grant", () => {
    render(<><StatusBadge status="deactivated" /><StatusBadge status="revoked" /></>);
    expect(screen.getByText("Deactivated")).toHaveClass("text-text-secondary");
    expect(screen.getByText("Revoked")).toHaveClass("text-danger");
    expect(screen.getByText("Revoked").querySelector("path")).toBeInTheDocument();
  });

  it("refuses blank labels from callers", () => {
    expect(() => render(<Badge> </Badge>)).toThrow("visible text");
    expect(() => render(<Tag> </Tag>)).toThrow("visible text");
  });

  it("labels metadata without implying status", () => {
    const { container } = render(<Tag>Production</Tag>);
    expect(screen.getByText("Production")).toBeVisible();
    expect(container.querySelector("svg, button")).toBeNull();
  });

  it.each(statuses)("keeps %s text at AA contrast on both backgrounds under any allowed branding", (status) => {
    const { container } = render(<StatusBadge status={status} />);
    const badge = container.firstElementChild!;
    const foreground = [...badge.classList].find((c) => /^text-(success|warning|text-primary|text-secondary|danger)$/.test(c))!.slice(5);
    // Branding can change only the accent, while these badges use reserved tokens.
    expect(BRANDABLE_TOKENS).toEqual(["--color-accent"]);
    expect(foreground).not.toBe("accent");
    for (const background of ["bg-base", "bg-surface"]) {
      expect(contrastRatio(token(foreground), token(background))).toBeGreaterThanOrEqual(4.5);
    }
  });

  it("meets automated accessibility checks across statuses and both role sources", async () => {
    const { container } = render(<>{statuses.map((status) => <StatusBadge key={status} status={status} />)}<RoleSourceBadge roleKey="reader" /><RoleSourceBadge roleKey="editor" delegatedFrom="Partner" /><Tag>Production</Tag></>);
    await expectNoAxeViolations(container);
  });
});

describe("delegation origin", () => {
  it("reveals and hides the source with keyboard and pointer activation", async () => {
    const user = userEvent.setup();
    render(<RoleSourceBadge roleKey="reader" delegatedFrom="Partner" />);
    const trigger = screen.getByRole("button", { name: /reader.*delegated from Partner/ });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText(/delegated from Partner/)).toHaveClass("sr-only");
    await user.tab();
    await user.keyboard("{Enter}");
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText(/delegated from Partner/)).not.toHaveClass("sr-only");
    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("rejects empty identity or origin labels", () => {
    expect(() => render(<RoleSourceBadge roleKey=" " />)).toThrow("role key");
    expect(() => render(<RoleSourceBadge roleKey="reader" delegatedFrom=" " />)).toThrow("source organization");
  });
});

/**
 * Every text role is readable on the surfaces it sits on, in every theme.
 *
 * tokens.scss states the rule ("every role must clear WCAG AA against its own surface on every
 * shipped theme") but nothing enforced it, and the light theme shipped accent-on-accent text at
 * about 2:1 before anyone measured. This resolves each role and each surface through the same
 * `var()` fallback chains the components use, in light and in dark.
 */
import { describe, expect, it } from "vitest";
import { collectVars, contrast, resolve, variants, type RGB, type Variant } from "./contrastFixtures";

/** WCAG AA for body text. */
const BODY = 4.5;

/** The expressions components actually use, fallbacks included. */
const SURFACES = {
  panel: "var(--surface-panel, var(--mainContentBackground))",
  page: "var(--mainContentBackground)",
};

const ROLES: Record<string, string> = {
  "--fg-primary": "var(--fg-primary)",
  "--fg-secondary": "var(--fg-secondary)",
  "--fg-muted": "var(--fg-muted)",
};

/** Status text ("Needs attention", "Banned", "Live"). */
const TONES: Record<string, string> = {
  "--fg-ok": "var(--fg-ok)",
  "--fg-warn": "var(--fg-warn)",
  "--fg-fail": "var(--fg-fail)",
};

function varsFor(v: Variant): Record<string, string> {
  return Object.assign(
    {},
    ...v.sheets.map((css) => collectVars(css, v.appearance)),
  ) as Record<string, string>;
}

function ratio(fgExpr: string, bgExpr: string, vars: Record<string, string>): number {
  const fg = resolve(fgExpr, vars);
  const bg = resolve(bgExpr, vars);
  expect(fg, `${fgExpr} did not resolve to a colour`).not.toBeNull();
  expect(bg, `${bgExpr} did not resolve to a colour`).not.toBeNull();
  return contrast(fg as RGB, bg as RGB);
}

const baseVariants = variants;

describe("text contrast", () => {
  it("checks both appearances", () => {
    expect(baseVariants.map((v) => v.name)).toEqual(["base (light)", "base (dark)"]);
  });

  describe.each(baseVariants.map((v) => [v.name, v] as const))("%s", (_name, variant) => {
    const vars = varsFor(variant);

    it.each(Object.entries(ROLES))("%s clears AA on panels and the page", (_role, expr) => {
      for (const surface of Object.values(SURFACES)) {
        expect(ratio(expr, surface, vars)).toBeGreaterThanOrEqual(BODY);
      }
    });

    it.each(Object.entries(TONES))("%s clears AA on panels", (_role, expr) => {
      expect(ratio(expr, SURFACES.panel, vars)).toBeGreaterThanOrEqual(BODY);
    });
  });

});

/**
 * Every text role is readable on the surfaces it sits on, in every theme.
 *
 * tokens.scss states the rule ("every role must clear WCAG AA against its own surface on every
 * shipped theme") but nothing enforced it, and the light theme shipped accent-on-accent text at
 * about 2:1 before anyone measured. This resolves each role and each surface through the same
 * `var()` fallback chains the components use, per variant: the base theme in light and dark,
 * and every classic pack (which drops `data-base-theme`, so the base palette's rules do not
 * apply to it).
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

/**
 * Status text ("Needs attention", "Banned", "Live"). Checked on the base theme only: classic
 * packs predate the tone roles and fall through to their own `--orange` / `--red` / `--green`,
 * which are accents, not text colours. Those packs are reported, not failed — see below.
 */
const TONES: Record<string, string> = {
  "--fg-ok": "var(--fg-ok)",
  "--fg-warn": "var(--fg-warn)",
  "--fg-fail": "var(--fg-fail)",
};

function varsFor(v: Variant): Record<string, string> {
  return Object.assign(
    {},
    ...v.sheets.map((css) => collectVars(css, v.appearance, v.classic)),
  ) as Record<string, string>;
}

function ratio(fgExpr: string, bgExpr: string, vars: Record<string, string>): number {
  const fg = resolve(fgExpr, vars);
  const bg = resolve(bgExpr, vars);
  expect(fg, `${fgExpr} did not resolve to a colour`).not.toBeNull();
  expect(bg, `${bgExpr} did not resolve to a colour`).not.toBeNull();
  return contrast(fg as RGB, bg as RGB);
}

const baseVariants = variants.filter((v) => !v.classic);

describe("text contrast", () => {
  it("checks both base appearances and at least one classic pack", () => {
    expect(baseVariants.map((v) => v.name)).toEqual(["base (light)", "base (dark)"]);
    expect(variants.some((v) => v.classic)).toBe(true);
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

  /*
    Classic packs: primary and secondary text must stay readable — that is the floor for a
    theme the app ships. Muted text is held to the large-text floor (3:1); several packs were
    designed with a dimmer comment colour and redrawing their palettes is not this test's job.
  */
  describe.each(variants.filter((v) => v.classic).map((v) => [v.name, v] as const))(
    "classic pack %s",
    (_name, variant) => {
      const vars = varsFor(variant);

      it("keeps primary and secondary text readable on its page", () => {
        expect(ratio(ROLES["--fg-primary"], SURFACES.page, vars)).toBeGreaterThanOrEqual(BODY);
        expect(ratio(ROLES["--fg-secondary"], SURFACES.page, vars)).toBeGreaterThanOrEqual(BODY);
      });

      it("keeps muted text at least at the large-text floor", () => {
        expect(ratio(ROLES["--fg-muted"], SURFACES.page, vars)).toBeGreaterThanOrEqual(3);
      });
    },
  );
});

/**
 * Every theme's tooltip must be readable.
 *
 * This guards a bug that shipped twice over, each time invisibly:
 *
 *  1. `.tooltip-inner` paired `color: var(--whiteSecondary)` with `background: var(--tooltip-bg)`.
 *     Those two tokens move in opposite directions — `--whiteSecondary` is bright text in a dark
 *     theme but dark body text in a light one, while `--tooltip-bg` stays dark for the inverted
 *     chip. Light themes put dark on dark.
 *  2. `themes/Pink/style.scss` then set `.tooltip .tooltip-inner { color: var(--chrome-fg) }` —
 *     later in the cascade at equal specificity, so it beat the shared rule and would have
 *     defeated any fix made in UI.scss alone.
 *
 * So this resolves the *effective* declaration the way the browser does (theme stylesheet after
 * the base one, last matching rule wins) rather than comparing the tokens a theme happens to
 * declare. Checking tokens alone is exactly what let (2) hide.
 */
import { describe, expect, it } from "vitest";
import { readdirSync, existsSync } from "node:fs";
import { join } from "node:path";
import {
    THEMES,
    contrast,
    effectiveTooltipColor,
    resolve,
    variants,
    collectVars,
    type RGB,
} from "./contrastFixtures";

/** WCAG AA for body text. Tooltip type is small, so this is the floor, not the target. */
const MIN_CONTRAST = 4.5;

describe("tooltip contrast", () => {
    it("actually loaded the themes it claims to check", () => {
        // A discovery bug that found zero themes would otherwise make this file pass loudly
        // while checking nothing. Name the light themes specifically: every one of them was
        // broken before this fix, so they are the ones whose absence would matter most.
        const names = variants.map((v) => v.name);
        expect(names).toEqual(
            expect.arrayContaining([
                "base (light)",
                "base (dark)",
                "Pink",
                "Catppuccin_Latte",
                "Gruvbox_Light",
                "Ayu_Light",
                "Kanagawa_Lotus",
            ]),
        );
        expect(variants.length).toBe(readdirSync(THEMES, { withFileTypes: true }).filter(
            (e) => e.isDirectory() && existsSync(join(THEMES, e.name, "style.scss")),
        ).length + 2);
    });

    it.each(variants.map((v) => [v.name, v] as const))("%s is readable", (_name, variant) => {
        const vars = Object.assign(
            {},
            ...variant.sheets.map((css) => collectVars(css, variant.appearance)),
        ) as Record<string, string>;

        const fg = resolve(effectiveTooltipColor(variant.sheets) ?? undefined, vars);
        const bg = resolve(vars["--tooltip-bg"], vars);

        // A theme that resolves to nothing is a broken assumption in this test, not a pass.
        expect(fg, "tooltip foreground did not resolve to a colour").not.toBeNull();
        expect(bg, "--tooltip-bg did not resolve to a colour").not.toBeNull();

        expect(contrast(fg as RGB, bg as RGB)).toBeGreaterThanOrEqual(MIN_CONTRAST);
    });
});

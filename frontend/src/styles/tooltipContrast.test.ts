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
import { readdirSync, existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import * as sass from "sass";

const STYLES = join(__dirname);
const THEMES = join(STYLES, "themes");

/** WCAG AA for body text. Tooltip type is small, so this is the floor, not the target. */
const MIN_CONTRAST = 4.5;

const SASS_OPTS = {
    loadPaths: [STYLES, join(STYLES, ".."), join(STYLES, "..", "..", "node_modules")],
    quietDeps: true,
    silenceDeprecations: ["legacy-js-api", "import", "global-builtin", "color-functions"],
} as unknown as sass.Options<"sync">;

type RGB = [number, number, number];

/**
 * Comments are stripped because declarations are found by their `;` boundaries, and a comment
 * sitting between two declarations hides the one after it. That is not hypothetical: the comment
 * explaining this very fix sits directly above `color:` in UI.scss.
 */
function compile(file: string): string {
    return sass.compile(file, SASS_OPTS).css.replace(/\/\*[\s\S]*?\*\//g, "");
}

/** Custom properties declared anywhere in a sheet, later winning, honouring an appearance filter. */
function collectVars(css: string, appearance?: "light" | "dark"): Record<string, string> {
    const vars: Record<string, string> = {};
    for (const rule of css.matchAll(/([^{}]*)\{([^{}]*)\}/g)) {
        const selector = rule[1];
        if (appearance === "light" && /appearance=dark/.test(selector)) continue;
        if (appearance === "dark" && /appearance=light/.test(selector)) continue;
        for (const decl of rule[2].matchAll(/(--[\w-]+)\s*:\s*([^;]+)/g)) {
            vars[decl[1]] = decl[2].trim();
        }
    }
    return vars;
}

/** The last `color` declared by a rule matching `.tooltip-inner` — i.e. the one that wins. */
function effectiveTooltipColor(sheets: string[]): string | null {
    let winner: string | null = null;
    for (const css of sheets) {
        for (const rule of css.matchAll(/([^{}]*)\{([^{}]*)\}/g)) {
            if (!/\.tooltip-inner\b/.test(rule[1])) continue;
            const color = [...rule[2].matchAll(/(?:^|;)\s*color\s*:\s*([^;]+)/g)].pop();
            if (color) winner = color[1].trim();
        }
    }
    return winner;
}

function resolve(value: string | undefined, vars: Record<string, string>, depth = 0): RGB | null {
    if (!value || depth > 8) return null;
    const raw = value.replace(/\s*!important\s*$/, "").trim();

    const fn = raw.match(/^var\(\s*(--[\w-]+)\s*(?:,\s*([\s\S]+))?\)$/);
    if (fn) {
        return resolve(vars[fn[1]], vars, depth + 1) ?? resolve(fn[2], vars, depth + 1);
    }

    let m: RegExpMatchArray | null;
    if ((m = raw.match(/^#([0-9a-f]{6})$/i))) {
        const n = parseInt(m[1], 16);
        return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
    }
    if ((m = raw.match(/^#([0-9a-f]{3})$/i))) {
        return m[1].split("").map((c) => parseInt(c + c, 16)) as RGB;
    }
    if ((m = raw.match(/^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i))) {
        return [+m[1], +m[2], +m[3]];
    }
    if ((m = raw.match(/^hsla?\(\s*([\d.]+)(?:deg)?[,\s]+([\d.]+)%[,\s]+([\d.]+)%/i))) {
        const [h, s, l] = [+m[1], +m[2] / 100, +m[3] / 100];
        const c = (1 - Math.abs(2 * l - 1)) * s;
        const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
        const base = l - c / 2;
        const seg = [
            [c, x, 0],
            [x, c, 0],
            [0, c, x],
            [0, x, c],
            [x, 0, c],
            [c, 0, x],
        ][Math.floor(h / 60) % 6];
        return seg.map((v) => Math.round((v + base) * 255)) as RGB;
    }
    return null;
}

function contrast(fg: RGB, bg: RGB): number {
    const channel = (v: number): number => {
        const n = v / 255;
        return n <= 0.03928 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4;
    };
    const lum = ([r, g, b]: RGB): number =>
        0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    const [hi, lo] = [lum(fg), lum(bg)].sort((a, b) => b - a);
    return (hi + 0.05) / (lo + 0.05);
}

/**
 * The base sheets, in the order `main.ts` imports them — read from `main.ts` rather than listed
 * here, because the whole point is which rule comes last, and a hand-copied order that drifts
 * would make this test confidently wrong.
 */
function baseSheets(): string[] {
    const main = readFileSync(join(STYLES, "..", "main.ts"), "utf8");
    const files = [...main.matchAll(/^import\s+['"]\.\/styles\/([\w.-]+\.scss)['"]/gm)].map(
        (m) => m[1],
    );
    if (files.length === 0) throw new Error("no stylesheet imports found in main.ts");
    return files.map((f) => compile(join(STYLES, f)));
}

const base = baseSheets();

const variants: Array<{ name: string; sheets: string[]; appearance?: "light" | "dark" }> = [
    { name: "base (light)", sheets: base, appearance: "light" },
    { name: "base (dark)", sheets: base, appearance: "dark" },
];
for (const entry of readdirSync(THEMES, { withFileTypes: true })) {
    const file = join(THEMES, entry.name, "style.scss");
    if (!entry.isDirectory() || !existsSync(file)) continue;
    variants.push({ name: entry.name, sheets: [...base, compile(file)] });
}

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

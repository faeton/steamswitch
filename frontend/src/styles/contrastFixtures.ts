/**
 * Shared machinery for the theme contrast tests: compile the stylesheets the way `main.ts`
 * loads them, collect the custom properties a variant ends up with, and resolve a colour value
 * through `var()` fallbacks to RGB.
 *
 * Moved out of tooltipContrast.test.ts so the text-role test checks themes the same way.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";
import * as sass from "sass";

export const STYLES = join(__dirname);

const SASS_OPTS = {
    loadPaths: [STYLES, join(STYLES, ".."), join(STYLES, "..", "..", "node_modules")],
    quietDeps: true,
    silenceDeprecations: ["legacy-js-api", "import", "global-builtin", "color-functions"],
} as unknown as sass.Options<"sync">;

export type RGB = [number, number, number];

/**
 * Comments are stripped because declarations are found by their `;` boundaries, and a comment
 * sitting between two declarations hides the one after it. That is not hypothetical: the comment
 * explaining this very fix sits directly above `color:` in UI.scss.
 */
export function compile(file: string): string {
    return sass.compile(file, SASS_OPTS).css.replace(/\/\*[\s\S]*?\*\//g, "");
}

/** Custom properties declared anywhere in a sheet, later winning, honouring an appearance filter. */
export function collectVars(
    css: string,
    appearance?: "light" | "dark",
): Record<string, string> {
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
export function effectiveTooltipColor(sheets: string[]): string | null {
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

export function resolve(value: string | undefined, vars: Record<string, string>, depth = 0): RGB | null {
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

export function contrast(fg: RGB, bg: RGB): number {
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
export function baseSheets(): string[] {
    const main = readFileSync(join(STYLES, "..", "main.ts"), "utf8");
    const files = [...main.matchAll(/^import\s+['"]\.\/styles\/([\w.-]+\.scss)['"]/gm)].map(
        (m) => m[1],
    );
    if (files.length === 0) throw new Error("no stylesheet imports found in main.ts");
    return files.map((f) => compile(join(STYLES, f)));
}

export const base = baseSheets();

export type Variant = {
    name: string;
    sheets: string[];
    appearance?: "light" | "dark";
};

export const variants: Variant[] = [
    { name: "base (light)", sheets: base, appearance: "light" },
    { name: "base (dark)", sheets: base, appearance: "dark" },
];


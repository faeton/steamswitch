import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * `styles/Settings.scss` hides every checkbox that is not `.ss-toggle` / `.ss-checkbox`
 * (`opacity: 0`) and paints a fake box on the `<label>` that must follow it — the inherited
 * `input + label.form-check-label` pattern. The rule is global, and Settings is prefetched at
 * startup, so a checkbox written any other way (`<label><input><span>…`) renders as nothing at
 * all. That is how the plaintext-import acknowledgement and the handoff "single use" box
 * shipped invisible.
 *
 * Every checkbox must therefore either opt out with a class or use the legacy pair.
 */
const SRC = join(__dirname, "..");

function svelteFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) svelteFiles(p, out);
    else if (entry.name.endsWith(".svelte")) out.push(p);
  }
  return out;
}

function tagStarts(src: string, open: string): number[] {
  const out: number[] = [];
  for (let i = src.indexOf(open); i >= 0; i = src.indexOf(open, i + 1)) out.push(i);
  return out;
}

/** Index just past the tag's closing `>`, ignoring any `>` inside a `{…}` expression (`=>`). */
function tagEnd(src: string, start: number): number {
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    const c = src[i];
    if (c === "{") depth++;
    else if (c === "}") depth--;
    else if (c === ">" && depth === 0) return i + 1;
  }
  return src.length;
}

function invisibleCheckboxes(): string[] {
  const offenders: string[] = [];
  for (const file of svelteFiles(SRC)) {
    const src = readFileSync(file, "utf8");
    for (const start of tagStarts(src, "<input")) {
      const end = tagEnd(src, start);
      const tag = src.slice(start, end);
      if (!/type="checkbox"/.test(tag)) continue;
      if (/class="[^"]*\bss-(toggle|checkbox)\b/.test(tag)) continue;
      const after = src.slice(end).trimStart();
      if (/^<label\b[^>]*class="[^"]*\bform-check-label\b/.test(after)) continue;
      const line = src.slice(0, start).split("\n").length;
      offenders.push(`${relative(SRC, file)}:${line}`);
    }
  }
  return offenders;
}

describe("checkbox markup", () => {
  it("never leaves a checkbox for the global legacy rule to hide", () => {
    expect(invisibleCheckboxes()).toEqual([]);
  });
});

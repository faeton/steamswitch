import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Every checkbox is one of the two drawn controls in tokens.scss — `.ss-toggle` (a setting
 * that is on or off) or `.ss-checkbox` (a tick in a form).
 *
 * This used to guard against a global legacy rule that set `opacity: 0` on any other
 * checkbox; seven shipped invisible before it existed. That rule is gone, but a bare
 * checkbox would now render as the browser's native box — a different size, colour and
 * focus ring on each of WebView2, WebKitGTK and WKWebView — so the requirement stands.
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
      const line = src.slice(0, start).split("\n").length;
      offenders.push(`${relative(SRC, file)}:${line}`);
    }
  }
  return offenders;
}

describe("checkbox markup", () => {
  it("draws every checkbox as .ss-toggle or .ss-checkbox", () => {
    expect(invisibleCheckboxes()).toEqual([]);
  });
});

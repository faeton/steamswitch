import { describe, expect, it } from "vitest";
import enUS from "../../Resources/en-US.json";
import { gameRunningConfirm } from "./gameWarning";

const strings = enUS as Record<string, string>;
const t = (key: string, vars: Record<string, string> = {}) =>
  Object.entries(vars).reduce((s, [k, v]) => s.replace(`{${k}}`, v), strings[key] ?? key);

describe("gameRunningConfirm", () => {
  const opts = gameRunningConfirm("Dota 2", t);

  it("names the game in everything that mentions it", () => {
    expect(opts.title).toContain("Dota 2");
    expect(opts.body).toContain("Dota 2");
    expect(opts.positiveLabel).toContain("Dota 2");
  });

  it("uses real strings rather than raw keys", () => {
    for (const text of [opts.title, opts.body, opts.positiveLabel, opts.negativeLabel]) {
      expect(text).not.toMatch(/^[A-Za-z]+_[A-Za-z_]+$/);
      expect(text).not.toContain("{");
    }
  });

  it("uses the style that actually draws a Cancel button", () => {
    expect(opts.style).toBe("yesno");
    expect(opts.negativeLabel).toBe("Cancel");
  });

  it("is marked destructive, because it closes the game", () => {
    expect(opts.destructive).toBe(true);
  });
});

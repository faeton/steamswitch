import { describe, expect, it } from "vitest";
import { isStale, rankedSignals, tileDot, verdictLabelKey, worstVerdict } from "./health";
import type { HealthReport, Signal } from "../../../bindings/steamswitch/internal/vault/models";

function sig(name: string, status: string, blocker = false): Signal {
  return { name, status, detail: "", blocker } as Signal;
}

function report(verdict: string, signals: Signal[] = [], probedAt = "2026-07-26T12:00:00Z"): HealthReport {
  return { verdict, signals, probedAt, deep: false } as HealthReport;
}

describe("worstVerdict", () => {
  it("takes the worse of two", () => {
    expect(worstVerdict("ok", "warn")).toBe("warn");
    expect(worstVerdict("warn", "ok")).toBe("warn");
    expect(worstVerdict("fail", "warn")).toBe("fail");
    expect(worstVerdict("unknown", "ok")).toBe("ok");
  });

  it("treats an unrecognised verdict as unknown rather than trusting it", () => {
    // A verdict string this build does not know must not outrank a real failure. Otherwise
    // a future backend value would silently hide a ban.
    expect(worstVerdict("fail", "something-new")).toBe("fail");
    expect(worstVerdict("something-new", "warn")).toBe("warn");
  });
});

describe("tileDot", () => {
  it("shows nothing for a healthy or unchecked account", () => {
    // A green dot on every healthy account trains people to ignore the colour, and a grey
    // dot for "not checked" would mark every account on a machine that never uses this.
    expect(tileDot(report("ok"))).toBeNull();
    expect(tileDot(report("unknown"))).toBeNull();
    expect(tileDot(null)).toBeNull();
    expect(tileDot(undefined)).toBeNull();
  });

  it("shows the dot for warn and fail", () => {
    expect(tileDot(report("warn"))).toBe("warn");
    expect(tileDot(report("fail"))).toBe("fail");
  });

  it("does not render an unknown verdict string as a dot", () => {
    expect(tileDot(report("catastrophe"))).toBeNull();
  });
});

describe("rankedSignals", () => {
  it("puts blockers first, then by severity, then by name", () => {
    const r = report("fail", [
      sig("token", "warn"),
      sig("steam_profile", "ok"),
      sig("steam_bans", "fail", true),
      sig("last_used", "warn"),
      sig("totp", "unknown"),
    ]);
    expect(rankedSignals(r).map((s) => s.name)).toEqual([
      "steam_bans",
      "last_used",
      "token",
      "steam_profile",
      "totp",
    ]);
  });

  it("does not mutate the report it was given", () => {
    const signals = [sig("b", "ok"), sig("a", "fail")];
    const r = report("fail", signals);
    rankedSignals(r);
    expect(r.signals.map((s) => s.name)).toEqual(["b", "a"]);
  });

  it("handles a report with no signals", () => {
    expect(rankedSignals(report("unknown"))).toEqual([]);
    expect(rankedSignals(null)).toEqual([]);
  });
});

describe("isStale", () => {
  const now = new Date("2026-07-26T12:00:00Z");

  it("is fresh within the window", () => {
    expect(isStale(report("ok", [], "2026-07-26T06:00:00Z"), now)).toBe(false);
  });

  it("is stale beyond it", () => {
    expect(isStale(report("ok", [], "2026-07-24T12:00:00Z"), now)).toBe(true);
  });

  it("treats a missing or unparseable timestamp as stale", () => {
    // Erring towards "offer a re-check" is right: the alternative is showing a verdict of
    // unknown age as though it were current.
    expect(isStale(report("ok", [], ""), now)).toBe(true);
    expect(isStale(report("ok", [], "not a date"), now)).toBe(true);
    expect(isStale(null, now)).toBe(true);
  });
});

describe("verdictLabelKey", () => {
  it("maps each verdict to its key and falls back to unknown", () => {
    expect(verdictLabelKey("ok")).toBe("Vault_Verdict_OK");
    expect(verdictLabelKey("warn")).toBe("Vault_Verdict_Warn");
    expect(verdictLabelKey("fail")).toBe("Vault_Verdict_Fail");
    expect(verdictLabelKey("")).toBe("Vault_Verdict_Unknown");
    expect(verdictLabelKey("weird")).toBe("Vault_Verdict_Unknown");
  });
});

import { describe, expect, it } from "vitest";
import {
  healthState,
  isStale,
  rankedSignals,
  tileDot,
  verdictLabelKey,
  worstVerdict,
} from "./health";
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

describe("healthState", () => {
  function detailSig(detail: string, status: string, blocker = false): Signal {
    return { name: "probe", status, detail, blocker } as Signal;
  }

  it("reports a UI-side check in flight, whatever the stored report says", () => {
    expect(healthState(report("ok"), { checking: true }).id).toBe("checking");
    expect(healthState(null, { checking: true }).id).toBe("checking");
  });

  it("treats a missing or never-probed report as never checked", () => {
    expect(healthState(null).id).toBe("never-checked");
    expect(healthState(report("ok", [], "")).id).toBe("never-checked");
  });

  it("names the specific failure rather than just its severity", () => {
    expect(healthState(report("fail", [detailSig("Vault_Signal_PasswordWrong", "fail")])).id).toBe(
      "password-wrong",
    );
    expect(healthState(report("warn", [detailSig("Vault_Signal_TokenExpired", "warn")])).id).toBe(
      "token-expired",
    );
    expect(healthState(report("warn", [detailSig("Vault_Signal_RateLimited", "warn")])).id).toBe(
      "limited",
    );
    expect(
      healthState(report("warn", [detailSig("Vault_Signal_GuardRejected", "warn")])).id,
    ).toBe("guard-unreachable");
  });

  /*
    Severity ordering is the whole reason this collapses rather than listing: an account that
    is both banned and rate-limited is a banned account, and showing "Rate limited" would tell
    the user to wait for something that is never coming back.
  */
  it("shows the worst state when several signals are unhappy", () => {
    const rep = report("fail", [
      detailSig("Vault_Signal_RateLimited", "warn"),
      detailSig("Vault_Signal_Banned", "fail", true),
      detailSig("Vault_Signal_TokenExpired", "warn"),
    ]);
    expect(healthState(rep).id).toBe("banned");
  });

  it("still flags an unhappy signal it has no dedicated state for", () => {
    // A future backend detail must not read as healthy just because this build predates it.
    expect(healthState(report("warn", [detailSig("Vault_Signal_SomethingNew", "warn")])).id).toBe(
      "attention",
    );
  });

  it("reads a clean probed report as live", () => {
    const rep = report("ok", [detailSig("Vault_Signal_NoBans", "ok"), detailSig("Vault_Signal_TokenValid", "ok")]);
    expect(healthState(rep).id).toBe("ok");
    expect(healthState(rep).tone).toBe("ok");
  });

  /*
    A report that ran but learned nothing is "never checked" from where the user stands.
    Calling it healthy would be the most dangerous possible rounding error here.
  */
  it("does not promote an unknown verdict to healthy", () => {
    expect(healthState(report("unknown", [detailSig("Vault_Signal_BansUnknown", "unknown")])).id).toBe(
      "never-checked",
    );
  });

  it("gives every actionable state a next step", () => {
    for (const detail of [
      "Vault_Signal_PasswordWrong",
      "Vault_Signal_TokenExpired",
      "Vault_Signal_GuardRejected",
      "Vault_Signal_RateLimited",
    ]) {
      const state = healthState(report("warn", [detailSig(detail, "warn")]));
      expect(state.actionKey, `${detail} should offer a next step`).not.toBe("");
    }
  });
});

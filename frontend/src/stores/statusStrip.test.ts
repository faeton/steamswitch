import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  beginSwitch,
  clearError,
  clearKitActive,
  clearRecovery,
  endSwitch,
  narrate,
  resetStatusStrip,
  retryTarget,
  setError,
  setIdleFacts,
  setKitActive,
  setRecovery,
  statusStrip,
  statusTone,
  switchingBlocked,
} from "./statusStrip";

beforeEach(() => {
  resetStatusStrip();
});

describe("idle", () => {
  it("starts idle and adopts the latest facts", () => {
    expect(get(statusStrip).kind).toBe("idle");
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: true });
    expect(get(statusStrip)).toEqual({
      kind: "idle",
      accountLabel: "Faeton",
      isHome: true,
      steamRunning: true,
    });
  });

  it("does not let a fact refresh clobber a switch in progress", () => {
    beginSwitch("SharedAcc");
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    expect(get(statusStrip).kind).toBe("switching");
  });
});

describe("switching", () => {
  it("narrates only while switching", () => {
    narrate("Closing Steam…");
    expect(get(statusStrip).kind).toBe("idle");

    beginSwitch("SharedAcc", "Closing Steam…");
    narrate("Saving their setup…");
    expect(get(statusStrip)).toMatchObject({
      kind: "switching",
      toLabel: "SharedAcc",
      phase: "Saving their setup…",
    });
  });

  it("settles back to idle when the switch ends", () => {
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    beginSwitch("SharedAcc");
    endSwitch();
    expect(get(statusStrip)).toMatchObject({ kind: "idle", accountLabel: "Faeton" });
  });

  it("settles to kit-active when a kit is live", () => {
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: true });
    beginSwitch("SharedAcc");
    endSwitch();
    expect(get(statusStrip)).toMatchObject({ kind: "kit-active", accountLabel: "SharedAcc" });
  });

  it("blocks interaction while switching", () => {
    expect(get(switchingBlocked)).toBe(false);
    beginSwitch("SharedAcc");
    expect(get(switchingBlocked)).toBe(true);
  });

  it("is single-flight: a second switch cannot start while one is running", () => {
    expect(beginSwitch("SharedAcc")).toBe(true);
    // Two rapid clicks must not both reach SwapToSteamAccount.
    expect(beginSwitch("Alt2")).toBe(false);
    expect(get(statusStrip)).toMatchObject({ kind: "switching", toLabel: "SharedAcc" });
  });
});

describe("recovery precedence", () => {
  it("refuses a switch while blocking", () => {
    setRecovery("interrupted-switch", "Last switch didn't finish", [
      { id: "restore", labelKey: "Kit_Restore", primary: true },
    ]);
    expect(beginSwitch("SharedAcc")).toBe(false);
    expect(get(statusStrip).kind).toBe("recovery");
    expect(get(switchingBlocked)).toBe(true);
  });

  it("swallows errors raised while blocking rather than hiding the recovery", () => {
    setRecovery("external-change", "Files changed outside SteamSwitch", []);
    setError("something else broke");
    expect(get(statusStrip).kind).toBe("recovery");
  });

  it("overrides an in-flight switch", () => {
    beginSwitch("SharedAcc");
    setRecovery("steam-running", "Waiting for Steam to close", []);
    expect(get(statusStrip).kind).toBe("recovery");
  });

  it("falls back to the resting state when cleared", () => {
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    setRecovery("interrupted-switch", "…", []);
    clearRecovery();
    expect(get(statusStrip)).toMatchObject({ kind: "idle", accountLabel: "Faeton" });
  });
});

describe("errors and kit state", () => {
  it("restores the kit line after an error clears", () => {
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: false });
    setError("switch failed");
    expect(get(statusStrip).kind).toBe("error");
    clearError();
    expect(get(statusStrip).kind).toBe("kit-active");
  });

  it("drops to idle once the kit is removed", () => {
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: false });
    clearKitActive();
    expect(get(statusStrip)).toMatchObject({ kind: "idle", accountLabel: "Faeton" });
  });

  it("does not resurrect a stale kit line after it is cleared mid-error", () => {
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: false });
    setError("boom");
    clearKitActive();
    clearError();
    expect(get(statusStrip).kind).toBe("idle");
  });
});

describe("retry target", () => {
  it("survives the page that started the switch being unmounted", () => {
    // The strip is app-global; holding the target on the accounts page meant navigating to
    // Tools and pressing Retry cleared the error and retried nothing.
    retryTarget.set("76561190000000002");
    setError("switch failed", { id: "retry-switch", labelKey: "Kit_Action_Retry" });
    expect(get(retryTarget)).toBe("76561190000000002");
  });

  it("is dropped when any new switch starts", () => {
    // Otherwise a later Retry fires at an account the user has since moved on from.
    retryTarget.set("76561190000000002");
    beginSwitch("SomewhereElse");
    expect(get(retryTarget)).toBe("");
  });
});

describe("kit-active facts", () => {
  it("tracks Steam starting and stopping for the life of the kit", () => {
    // A kit can be live for hours, and whether Steam is up decides whether it is safe to
    // touch configs. Only updating the remembered facts would freeze the strip on the state
    // from before the kit was applied.
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: true });
    expect(get(statusStrip)).toMatchObject({ kind: "kit-active", steamRunning: false });

    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: true });
    expect(get(statusStrip)).toMatchObject({ kind: "kit-active", steamRunning: true });
  });

  it("still does not let a fact refresh clobber the kit line itself", () => {
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: true });
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: true });
    expect(get(statusStrip)).toMatchObject({ kind: "kit-active", accountLabel: "SharedAcc" });
  });
});

describe("tone", () => {
  it("maps each state to a severity without relying on colour alone", () => {
    expect(get(statusTone)).toBe("neutral");
    beginSwitch("SharedAcc");
    expect(get(statusTone)).toBe("busy");
    endSwitch();
    setKitActive({ accountLabel: "SharedAcc", modules: [], cloudRisk: false });
    expect(get(statusTone)).toBe("warn");
    setRecovery("steam-running", "…", []);
    expect(get(statusTone)).toBe("warn");
    setRecovery("interrupted-switch", "…", []);
    expect(get(statusTone)).toBe("error");
  });
});

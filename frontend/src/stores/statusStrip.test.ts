import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import {
  beginSwitch,
  clearError,
  clearKitActive,
  clearRecovery,
  dismissSwitch,
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

  /*
    A completed switch holds the result until dismissed rather than settling on its own. Brief
    A13 requires the success state to be unambiguous, and a dock that vanishes the instant the
    engine returns leaves the user staring at the same grid wondering whether anything happened.
  */
  it("holds the result after the switch ends, then settles to idle on dismiss", () => {
    setIdleFacts({ accountLabel: "Faeton", isHome: true, steamRunning: false });
    beginSwitch("SharedAcc");
    endSwitch();
    expect(get(statusStrip)).toMatchObject({ kind: "switching", finished: true });
    dismissSwitch();
    expect(get(statusStrip)).toMatchObject({ kind: "idle", accountLabel: "Faeton" });
  });

  it("settles to kit-active on dismiss when a kit is live", () => {
    setKitActive({ accountLabel: "SharedAcc", modules: ["Dota 2"], cloudRisk: true });
    beginSwitch("SharedAcc");
    endSwitch();
    dismissSwitch();
    expect(get(statusStrip)).toMatchObject({ kind: "kit-active", accountLabel: "SharedAcc" });
  });

  // An undismissed *result* is not work in progress: the next switch must not be refused
  // just because the user has not clicked Done yet.
  it("does not let an undismissed result block the next switch", () => {
    beginSwitch("A");
    endSwitch();
    expect(get(switchingBlocked)).toBe(false);
    expect(beginSwitch("B")).toBe(true);
    expect(get(statusStrip)).toMatchObject({ kind: "switching", toLabel: "B", finished: false });
  });

  it("tracks the account being left, for the dock's 'Saving X\'s login' step", () => {
    beginSwitch("Ash", "Marlow");
    expect(get(statusStrip)).toMatchObject({ toLabel: "Ash", fromLabel: "Marlow" });
  });

  /*
    The engine reports `Status_UpdatingFile` several times across two different writes, so
    progress has to be monotonic or the bar walks backwards mid-switch.
  */
  it("advances the step monotonically from backend status keys", () => {
    beginSwitch("Ash", "Marlow");
    expect(get(statusStrip)).toMatchObject({ step: null });
    narrate("Closing Steam", "Status_ClosingPlatform");
    expect(get(statusStrip)).toMatchObject({ step: "close" });
    narrate("Writing", "Status_UpdatingFile");
    expect(get(statusStrip)).toMatchObject({ step: "login" });
    narrate("Closing Steam", "Status_ClosingPlatform");
    expect(get(statusStrip)).toMatchObject({ step: "login" });
    narrate("Launching", "Status_StartingPlatform");
    expect(get(statusStrip)).toMatchObject({ step: "launch" });
  });

  it("ignores narration with no recognised key rather than resetting progress", () => {
    beginSwitch("Ash", "Marlow");
    narrate("Writing", "Status_UpdatingFile");
    narrate("something the frontend does not know");
    expect(get(statusStrip)).toMatchObject({ step: "login", phase: "something the frontend does not know" });
  });

  /*
    The switcher goes through the session-kit engine, which reports kit work *between*
    closing Steam and writing the login. Mapping those keys is what stopped the dock sitting
    on "Closing Steam" for the whole kit stage and then mislabelling the login write.
  */
  it("maps the session-kit phases the switcher actually emits", () => {
    beginSwitch("Ash", "Marlow");
    narrate("Closing", "Status_ClosingPlatform");
    narrate("Saving their setup", "Status_Kit_SavingTheirSetup");
    expect(get(statusStrip)).toMatchObject({ step: "kit" });
    narrate("Applying", "Status_Kit_ApplyingKit");
    expect(get(statusStrip)).toMatchObject({ step: "kit" });
    narrate("Login", "Status_ActionBar_UpdatingSteamLogin");
    expect(get(statusStrip)).toMatchObject({ step: "login" });
  });

  /*
    The engine emits "starting Steam" even when the AutoStart setting makes the launch a
    no-op, so whether Steam came back has to be observed and passed in — never inferred from
    the step having been reported.
  */
  it("records the observed Steam state rather than assuming the launch worked", () => {
    beginSwitch("Ash", "Marlow");
    narrate("Launching", "Status_StartingPlatform");
    endSwitch(false);
    expect(get(statusStrip)).toMatchObject({ finished: true, launched: false });

    dismissSwitch();
    beginSwitch("Ash", "Marlow");
    narrate("Launching", "Status_StartingPlatform");
    endSwitch(true);
    expect(get(statusStrip)).toMatchObject({ finished: true, launched: true });
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
    // A finished-but-undismissed switch is a result, not work: it stops reading as busy.
    endSwitch();
    expect(get(statusTone)).toBe("neutral");
    dismissSwitch();
    setKitActive({ accountLabel: "SharedAcc", modules: [], cloudRisk: false });
    expect(get(statusTone)).toBe("warn");
    setRecovery("steam-running", "…", []);
    expect(get(statusTone)).toBe("warn");
    setRecovery("interrupted-switch", "…", []);
    expect(get(statusTone)).toBe("error");
  });
});

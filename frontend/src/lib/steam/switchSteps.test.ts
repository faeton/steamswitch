import { describe, expect, it } from "vitest";
import {
  advanceStep,
  stepForStatusKey,
  stepMarks,
  stepPercent,
  visibleSteps,
} from "./switchSteps";

describe("stepForStatusKey", () => {
  /*
    These keys are the contract with `internal/sessionkit/engine.go`. The switcher calls
    `SessionKitService.SwitchTo` → `Engine.Enter`, which is a different emitter from the
    classic `internal/steam/switcher.go` — an earlier version mapped the classic one and so
    labelled the login write "saving".
  */
  it("maps the engine's close, kit, login and launch phases", () => {
    expect(stepForStatusKey("Status_ClosingPlatform")).toBe("close");
    expect(stepForStatusKey("Status_Kit_SavingTheirSetup")).toBe("kit");
    expect(stepForStatusKey("Status_Kit_ApplyingKit")).toBe("kit");
    expect(stepForStatusKey("Status_ActionBar_UpdatingSteamLogin")).toBe("login");
    expect(stepForStatusKey("Status_StartingPlatform")).toBe("launch");
  });

  it("returns null for chatter it does not recognise", () => {
    expect(stepForStatusKey("Status_SomethingNew")).toBeNull();
    expect(stepForStatusKey("")).toBeNull();
  });
});

describe("advanceStep", () => {
  it("moves forward", () => {
    expect(advanceStep(null, "close")).toBe("close");
    expect(advanceStep("close", "login")).toBe("login");
  });

  // `Status_UpdatingFile` fires several times across different writes; a naive assignment
  // would make the bar stutter backwards mid-switch.
  it("never moves backward", () => {
    expect(advanceStep("login", "close")).toBe("login");
    expect(advanceStep("launch", "kit")).toBe("launch");
  });

  it("holds position for an unrecognised key", () => {
    expect(advanceStep("login", null)).toBe("login");
    expect(advanceStep(null, null)).toBeNull();
  });
});

describe("visibleSteps", () => {
  // Most switches move no game settings at all; a chip that stays pending forever would
  // describe work that is never going to happen.
  it("hides the kit step until the engine reports kit work", () => {
    expect(visibleSteps(false)).toEqual(["close", "login", "launch"]);
    expect(visibleSteps(true)).toEqual(["close", "kit", "login", "launch"]);
  });
});

describe("stepMarks", () => {
  it("marks reached steps done and the current one active", () => {
    const m = stepMarks("login", false);
    expect(m.close).toBe("done");
    expect(m.login).toBe("active");
    expect(m.launch).toBe("pending");
  });

  it("marks everything unreached as skipped once finished", () => {
    const m = stepMarks("login", true);
    expect(m.login).toBe("done");
    expect(m.launch).toBe("skipped");
  });

  /*
    The engine reports "starting Steam" even when the AutoStart setting makes the launch a
    no-op. Marking that step done would tell the user Steam reopened when it did not.
  */
  it("marks the launch skipped when Steam did not actually come back", () => {
    expect(stepMarks("launch", true, false).launch).toBe("skipped");
    expect(stepMarks("launch", true, true).launch).toBe("done");
  });
});

describe("stepPercent", () => {
  it("starts at zero and finishes full", () => {
    expect(stepPercent(null, false)).toBe(0);
    expect(stepPercent("close", true)).toBe(100);
  });

  // A bar reading 100% while the last step is still running is the classic progress lie.
  it("sits inside the active step rather than at its end", () => {
    expect(stepPercent("launch", false)).toBeLessThan(100);
    expect(stepPercent("close", false)).toBeGreaterThan(0);
  });
});

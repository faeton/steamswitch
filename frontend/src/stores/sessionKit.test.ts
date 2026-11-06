import { get } from "svelte/store";
import { beforeEach, describe, expect, it, vi } from "vitest";

const ResolveRecovery = vi.fn();
const GetKitStatus = vi.fn();
vi.mock("../../bindings/steamswitch/internal/steam/sessionkitservice.js", () => ({
  ResolveRecovery: (action: string) => ResolveRecovery(action),
  GetKitStatus: () => GetKitStatus(),
}));

const { leavePrompt, recoveryActionClosesSteam, resolveRecovery } = await import("./sessionKit");

const prompt = {
  fromLabel: "B",
  toLabel: "A",
  toSteamId64: "1",
  moduleNames: [],
  onQuit: false,
};

describe("resolveRecovery", () => {
  beforeEach(() => {
    ResolveRecovery.mockReset();
    GetKitStatus.mockReset();
    GetKitStatus.mockResolvedValue({ kind: "none" });
    leavePrompt.set(prompt);
  });

  it("drops the leave prompt the recovery answered, before the status can remount it", async () => {
    ResolveRecovery.mockResolvedValue(undefined);
    let promptWhenStatusAsked: unknown = "not asked";
    GetKitStatus.mockImplementation(async () => {
      promptWhenStatusAsked = get(leavePrompt);
      return { kind: "none" };
    });
    await resolveRecovery("restore-theirs");
    expect(promptWhenStatusAsked).toBeNull();
  });

  it("keeps the leave prompt when the engine refused", async () => {
    ResolveRecovery.mockRejectedValue(new Error("Toast_Kit_ProcessesRunning"));
    await expect(resolveRecovery("restore-theirs")).rejects.toThrow();
    expect(get(leavePrompt)).toEqual(prompt);
  });
});

describe("recoveryActionClosesSteam", () => {
  it("is false only for the answers that write nothing", () => {
    expect(recoveryActionClosesSteam("keep-current", "external-change")).toBe(false);
    expect(recoveryActionClosesSteam("accept-current", "external-change")).toBe(false);
    expect(recoveryActionClosesSteam("restore-theirs", "external-change")).toBe(true);
    expect(recoveryActionClosesSteam("keep-current", "interrupted")).toBe(true);
    expect(recoveryActionClosesSteam("restore-theirs", "interrupted")).toBe(true);
  });
});

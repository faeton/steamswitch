import { describe, expect, it } from "vitest";
import { canCopyConfig, configProfile, partHintKey, selectionHasCloudPart, type GameInfo } from "./gameConfig";

const info = (over: Partial<GameInfo> = {}): GameInfo => ({
  appId: "730",
  name: "Counter-Strike 2",
  parts: [
    { id: "local", cloudSynced: true, machineWide: false },
    { id: "remote", cloudSynced: true, machineWide: false },
  ],
  accounts: [],
  gameRunning: false,
  steamRunning: false,
  ...over,
});

const ready = { busy: false, sourceId: "a", destId: "b", copyParts: ["local"], sourceParts: ["local"] };

describe("canCopyConfig", () => {
  it("allows a copy in either direction", () => {
    expect(canCopyConfig({ ...ready, info: info() })).toBe(true);
    expect(canCopyConfig({ ...ready, sourceId: "b", destId: "a", info: info() })).toBe(true);
  });

  it("refuses while the game runs, with no override", () => {
    expect(canCopyConfig({ ...ready, info: info({ gameRunning: true }) })).toBe(false);
  });

  it("refuses when the source has none of the selected parts", () => {
    expect(canCopyConfig({ ...ready, sourceParts: ["remote"], info: info() })).toBe(false);
  });

  it("refuses the same account, a missing side and a busy page", () => {
    expect(canCopyConfig({ ...ready, destId: "a", info: info() })).toBe(false);
    expect(canCopyConfig({ ...ready, destId: "", info: info() })).toBe(false);
    expect(canCopyConfig({ ...ready, busy: true, info: info() })).toBe(false);
    expect(canCopyConfig({ ...ready, info: null })).toBe(false);
  });
});

describe("selectionHasCloudPart", () => {
  it("goes by what the backend says about this game", () => {
    const dota = info({ parts: [{ id: "local", cloudSynced: false, machineWide: false }, { id: "remote", cloudSynced: true, machineWide: false }] });
    expect(selectionHasCloudPart(dota, ["local"])).toBe(false);
    expect(selectionHasCloudPart(dota, ["local", "remote"])).toBe(true);
    expect(selectionHasCloudPart(info(), ["local"])).toBe(true);
  });
});

describe("configProfile", () => {
  it("treats unknown games as unknown", () => {
    expect(configProfile("570")).toBe("dota");
    expect(configProfile("730")).toBe("cs2");
    expect(configProfile("12345")).toBe("generic");
    expect(partHintKey("cs2", "local")).toBe("GameCfg_Part_local_Hint_cs2");
  });
});

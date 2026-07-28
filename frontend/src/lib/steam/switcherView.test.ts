import { describe, expect, it } from "vitest";
import {
  EMPTY_SWITCHER_FILTERS,
  accountForQuickSwitch,
  filterAccounts,
  quickSwitchDigit,
  quickSwitchIndex,
  shouldShowFilters,
  tagsInUse,
} from "./switcherView";
import type { SteamAccountRow } from "./types";

function acc(over: Partial<SteamAccountRow> = {}): SteamAccountRow {
  return {
    steamId64: "76561198000000001",
    displayName: "Marlow",
    personaName: "Marlow",
    accountName: "marlow_grey",
    currentSession: false,
    ...over,
  } as SteamAccountRow;
}

function tag(name: string) {
  return { name } as SteamAccountRow["tags"] extends (infer T)[] | undefined ? T : never;
}

const ROSTER: SteamAccountRow[] = [
  acc({ steamId64: "1", displayName: "Marlow", accountName: "marlow_grey", tags: [tag("main")] }),
  acc({ steamId64: "2", displayName: "Ash Canyon", accountName: "ashcanyon", tags: [tag("trade")] }),
  acc({ steamId64: "3", displayName: "Quiet River", accountName: "quietriver_7", tags: [tag("farm")] }),
  acc({ steamId64: "4", displayName: "Delta Nine", accountName: "delta_nine", tags: [tag("farm")] }),
  acc({ steamId64: "5", displayName: "Nova", accountName: "nova.kx" }),
];

const names = (rows: SteamAccountRow[]): string[] => rows.map((r) => r.displayName ?? "");

describe("shouldShowFilters", () => {
  /*
    The brief calls a permanently-visible filter bar a "density tax": at two accounts the
    controls are taller than the thing they filter.
  */
  it("stays hidden for a small untagged roster", () => {
    expect(shouldShowFilters(2, 0)).toBe(false);
    expect(shouldShowFilters(7, 0)).toBe(false);
  });

  it("appears once the roster is large", () => {
    expect(shouldShowFilters(8, 0)).toBe(true);
    expect(shouldShowFilters(500, 0)).toBe(true);
  });

  it("appears as soon as tags exist, however few accounts", () => {
    expect(shouldShowFilters(2, 1)).toBe(true);
  });
});

describe("filterAccounts", () => {
  it("returns everything unfiltered", () => {
    expect(filterAccounts(ROSTER, EMPTY_SWITCHER_FILTERS)).toHaveLength(5);
  });

  it("matches display name, login name and SteamID64", () => {
    expect(names(filterAccounts(ROSTER, { query: "quiet", tag: "" }))).toEqual(["Quiet River"]);
    expect(names(filterAccounts(ROSTER, { query: "nova.kx", tag: "" }))).toEqual(["Nova"]);
    expect(names(filterAccounts(ROSTER, { query: "4", tag: "" }))).toEqual(["Delta Nine"]);
  });

  it("is case-insensitive and trims", () => {
    expect(names(filterAccounts(ROSTER, { query: "  ASH ", tag: "" }))).toEqual(["Ash Canyon"]);
  });

  it("filters by tag", () => {
    expect(names(filterAccounts(ROSTER, { query: "", tag: "farm" }))).toEqual([
      "Quiet River",
      "Delta Nine",
    ]);
  });

  it("combines query and tag with AND", () => {
    expect(names(filterAccounts(ROSTER, { query: "delta", tag: "farm" }))).toEqual(["Delta Nine"]);
    expect(filterAccounts(ROSTER, { query: "delta", tag: "trade" })).toEqual([]);
  });

  it("preserves the incoming order, which is the user's ordering", () => {
    expect(names(filterAccounts(ROSTER, { query: "a", tag: "" }))[0]).toBe("Marlow");
  });
});

describe("tagsInUse", () => {
  it("collects distinct tags, sorted", () => {
    expect(tagsInUse(ROSTER)).toEqual(["farm", "main", "trade"]);
  });

  it("is empty for an untagged roster", () => {
    expect(tagsInUse([acc({ tags: undefined })])).toEqual([]);
  });
});

describe("quickSwitchIndex", () => {
  it("numbers visible accounts from 1", () => {
    expect(quickSwitchIndex(ROSTER, "")).toEqual({ "1": 1, "2": 2, "3": 3, "4": 4, "5": 5 });
  });

  /*
    Pressing the current account's number would do nothing. Spending one of nine short keys
    on a guaranteed no-op is the opposite of what the shortcut is for.
  */
  it("skips the account Steam is already signed in as", () => {
    expect(quickSwitchIndex(ROSTER, "2")).toEqual({ "1": 1, "3": 2, "4": 3, "5": 4 });
  });

  // This is the visible-order contract the shortcuts overlay tells the user about: after a
  // filter the digits renumber to match what is on screen.
  it("renumbers to match a filtered view", () => {
    const visible = filterAccounts(ROSTER, { query: "", tag: "farm" });
    expect(quickSwitchIndex(visible, "")).toEqual({ "3": 1, "4": 2 });
  });

  it("stops at nine", () => {
    const many = Array.from({ length: 20 }, (_, i) => acc({ steamId64: String(i) }));
    const index = quickSwitchIndex(many, "");
    expect(Object.keys(index)).toHaveLength(9);
    expect(Math.max(...Object.values(index))).toBe(9);
  });
});

describe("accountForQuickSwitch", () => {
  it("resolves a digit to the account showing it", () => {
    expect(accountForQuickSwitch(ROSTER, "", 3)?.displayName).toBe("Quiet River");
    expect(accountForQuickSwitch(ROSTER, "2", 3)?.displayName).toBe("Delta Nine");
  });

  it("returns nothing for an unbound or out-of-range digit", () => {
    expect(accountForQuickSwitch(ROSTER, "", 9)).toBeUndefined();
    expect(accountForQuickSwitch(ROSTER, "", 0)).toBeUndefined();
    expect(accountForQuickSwitch(ROSTER, "", 10)).toBeUndefined();
  });

  // The tile's rendered keycap and the key handler must agree, or pressing the number shown
  // on a tile switches to a different account — the worst possible failure for this feature.
  it("agrees with the index the tiles render", () => {
    const visible = filterAccounts(ROSTER, { query: "", tag: "farm" });
    const index = quickSwitchIndex(visible, "");
    for (const account of visible) {
      const digit = index[account.steamId64];
      expect(accountForQuickSwitch(visible, "", digit)?.steamId64).toBe(account.steamId64);
    }
  });
});

describe("quickSwitchDigit", () => {
  it("reads a plain digit", () => {
    expect(quickSwitchDigit({ key: "3", code: "Digit3" })).toBe(3);
  });

  /*
    The bug this exists for: `key` is the *typed character*, so Shift+1 on a US layout is "!",
    Shift+2 is "@", and so on. Matching on `key` meant the documented Shift+N shortcut (open
    detail instead of switching) could never fire on any layout.
  */
  it("reads the physical key when Shift changes the character", () => {
    expect(quickSwitchDigit({ key: "!", code: "Digit1" })).toBe(1);
    expect(quickSwitchDigit({ key: "@", code: "Digit2" })).toBe(2);
    expect(quickSwitchDigit({ key: "£", code: "Digit3" })).toBe(3);
  });

  it("accepts the numeric keypad", () => {
    expect(quickSwitchDigit({ key: "5", code: "Numpad5" })).toBe(5);
  });

  it("falls back to the character when there is no usable code", () => {
    expect(quickSwitchDigit({ key: "4", code: "" })).toBe(4);
  });

  it("returns 0 for anything that is not a 1–9 key", () => {
    expect(quickSwitchDigit({ key: "0", code: "Digit0" })).toBe(0);
    expect(quickSwitchDigit({ key: "a", code: "KeyA" })).toBe(0);
    expect(quickSwitchDigit({ key: "!", code: "" })).toBe(0);
  });
});

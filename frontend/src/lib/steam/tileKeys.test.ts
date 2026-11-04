import { describe, expect, it } from "vitest";
import { rowClickIntent, rowDoubleClickSwitches, tileKeyIntent } from "./tileKeys";

type TileKey = Parameters<typeof tileKeyIntent>[0];

/** The parts of a KeyboardEvent the tile reads. */
function key(k: string, opts: { fromChild?: boolean; shiftKey?: boolean } = {}): TileKey {
  const tile = {} as EventTarget;
  return {
    key: k,
    shiftKey: opts.shiftKey ?? false,
    currentTarget: tile,
    target: opts.fromChild ? ({} as EventTarget) : tile,
  };
}

describe("tileKeyIntent", () => {
  it("switches on Enter, but Space only shows details", () => {
    expect(tileKeyIntent(key("Enter"))).toBe("activate");
    expect(tileKeyIntent(key(" "))).toBe("detail");
  });

  // Regression: Enter on the focused ⋯ button bubbled to the tile, which called
  // preventDefault (so the menu never opened) and switched accounts — closing Steam.
  it("ignores Enter and Space that bubble up from a control inside the tile", () => {
    expect(tileKeyIntent(key("Enter", { fromChild: true }))).toBeNull();
    expect(tileKeyIntent(key(" ", { fromChild: true }))).toBeNull();
  });

  it("opens the menu from the ContextMenu key or Shift+F10", () => {
    expect(tileKeyIntent(key("ContextMenu"))).toBe("menu");
    expect(tileKeyIntent(key("F10", { shiftKey: true }))).toBe("menu");
    expect(tileKeyIntent(key("F10"))).toBeNull();
  });

  it("leaves other keys alone", () => {
    expect(tileKeyIntent(key("a"))).toBeNull();
    expect(tileKeyIntent(key("Tab"))).toBeNull();
  });
});

describe("rowClickIntent", () => {
  it("does nothing on a single pointer click: that closed Steam with no warning", () => {
    expect(rowClickIntent({ detail: 1, shiftKey: false })).toBeNull();
    // The two clicks of a double-click: the switch comes from dblclick alone, so it fires once.
    expect(rowClickIntent({ detail: 2, shiftKey: false })).toBeNull();
    expect(rowClickIntent({ detail: 3, shiftKey: false })).toBeNull();
  });

  it("switches on a click that came from a key, a screen reader or a controller", () => {
    expect(rowClickIntent({ detail: 0, shiftKey: false })).toBe("switch");
  });

  it("keeps Shift-click for the detail panel", () => {
    expect(rowClickIntent({ detail: 1, shiftKey: true })).toBe("detail");
    expect(rowClickIntent({ detail: 0, shiftKey: true })).toBe("detail");
  });
});

describe("rowDoubleClickSwitches", () => {
  it("switches, unless Shift is held", () => {
    expect(rowDoubleClickSwitches({ shiftKey: false })).toBe(true);
    expect(rowDoubleClickSwitches({ shiftKey: true })).toBe(false);
  });
});

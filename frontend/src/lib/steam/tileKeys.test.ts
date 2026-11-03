import { describe, expect, it } from "vitest";
import { tileKeyIntent } from "./tileKeys";

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
  it("activates on Enter and Space aimed at the tile itself", () => {
    expect(tileKeyIntent(key("Enter"))).toBe("activate");
    expect(tileKeyIntent(key(" "))).toBe("activate");
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

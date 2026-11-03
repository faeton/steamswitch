import { describe, expect, it } from "vitest";
import { sameRoster } from "./rosterChange";

describe("sameRoster", () => {
  it("is true for the same accounts in any order", () => {
    expect(sameRoster(["a", "b"], ["b", "a"])).toBe(true);
  });

  // A focus refresh reads only the list; a new or removed account needs the full reload
  // (avatars, VAC, tags), which is what this tells the caller.
  it("is false when an account was added or removed", () => {
    expect(sameRoster(["a", "b"], ["a", "b", "c"])).toBe(false);
    expect(sameRoster(["a", "b"], ["a"])).toBe(false);
    expect(sameRoster(["a", "b"], ["a", "c"])).toBe(false);
  });
});

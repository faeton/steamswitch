import { describe, expect, it } from "vitest";
import { sessionChanged, type SessionVerdict } from "./sessionState";

const as = (steamId64: string): SessionVerdict => ({ state: "ok", steamId64 });
const none: SessionVerdict = { state: "none", steamId64: "" };

describe("sessionChanged", () => {
  // "Add another Steam login" signs out first and launches Steam second. When only the launch
  // fails, the call still rejects — but the sign-out is done, and the UI must report that
  // rather than "nothing happened".
  it("is true when an add-login's sign-out landed before the error", () => {
    expect(sessionChanged(as("1"), none)).toBe(true);
  });

  it("is true when the selection moved to another account", () => {
    expect(sessionChanged(as("1"), as("2"))).toBe(true);
  });

  it("is false when the files still say what they said before", () => {
    expect(sessionChanged(as("1"), as("1"))).toBe(false);
    expect(sessionChanged(none, none)).toBe(false);
  });
});

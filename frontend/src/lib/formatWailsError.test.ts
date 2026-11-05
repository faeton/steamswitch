import { describe, expect, it } from "vitest";
import { formatKeyedToast, formatPlainToast, isAppLockedError, keyedErrorMessage } from "./formatWailsError";

const t = (key: string) =>
  key === "Toast_Kit_ProcessesRunning" ? "Steam or a game is still running." : key;

describe("keyedErrorMessage", () => {
  it("translates a key with details after it", () => {
    expect(keyedErrorMessage({ message: "Toast_Kit_ProcessesRunning: [dota2.exe]" }, t)).toBe(
      "Steam or a game is still running.",
    );
  });

  it("unwraps the JSON form Wails rejects with", () => {
    const err = new Error(
      JSON.stringify({ message: "Toast_Kit_ProcessesRunning: [dota2.exe]", cause: {}, kind: "RuntimeError" }),
    );
    expect(keyedErrorMessage(err, t)).toBe("Steam or a game is still running.");
  });

  it("leaves everything else to the caller", () => {
    expect(keyedErrorMessage({ message: "access denied" }, t)).toBeNull();
    expect(keyedErrorMessage({ message: "Toast_NoSuchKey: x" }, t)).toBeNull();
    expect(keyedErrorMessage(null, t)).toBeNull();
  });
});

describe("formatPlainToast", () => {
  it("leaves out the cause and name", () => {
    const err = new Error(
      JSON.stringify({ message: "open C:\\x: Path not found.", cause: { Err: 3 }, name: "RuntimeError" }),
    );
    expect(formatPlainToast("Could not load accounts.", err)).toBe(
      "Could not load accounts. open C:\\x: Path not found.",
    );
  });

  it("still says something when the error has no message", () => {
    expect(formatPlainToast("Failed.", {})).toContain("Failed.");
  });
});

describe("isAppLockedError", () => {
  it("recognises only the lock", () => {
    expect(isAppLockedError(new Error(JSON.stringify({ message: "app is locked", cause: {} })))).toBe(true);
    expect(isAppLockedError({ message: "access denied" })).toBe(false);
    expect(isAppLockedError(null)).toBe(false);
  });
});

describe("formatKeyedToast", () => {
  it("joins the prefix and the sentence", () => {
    expect(formatKeyedToast("Failed.", { message: "Toast_Kit_ProcessesRunning: [dota2.exe]" }, t)).toBe(
      "Failed. Steam or a game is still running.",
    );
  });

  it("falls back to the full error", () => {
    expect(formatKeyedToast("Failed.", { message: "access denied" }, t)).toContain("access denied");
  });
});

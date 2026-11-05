import { describe, expect, it } from "vitest";
import { formatKeyedToast, keyedErrorMessage } from "./formatWailsError";

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

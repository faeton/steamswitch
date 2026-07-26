import { describe, expect, it } from "vitest";
import { accountMetaLine, accountMetaSegments, accountNotePreview } from "./accountMetaLine";

const fmt = (raw: string): string => (raw ? `on ${raw}` : "");

describe("accountMetaSegments", () => {
  it("shows nothing when every display toggle is off", () => {
    expect(
      accountMetaSegments(
        { accountName: "alice", steamId64: "76561198000000001", lastLogin: "2026-07-01T10:00:00Z" },
        fmt,
      ),
    ).toEqual([]);
  });

  it("keeps a fixed order regardless of which toggles are on", () => {
    const account = {
      accountName: "alice",
      steamId64: "76561198000000001",
      lastLogin: "2026-07-01T10:00:00Z",
      showAccUsername: true,
      showSteamId: true,
      showLastLogin: true,
    };
    expect(accountMetaSegments(account, fmt)).toEqual([
      "alice",
      "76561198000000001",
      "on 2026-07-01T10:00:00Z",
    ]);
  });

  it("drops a segment whose value is missing rather than leaving a gap", () => {
    // A never-signed-in account has no last login; the separator must not survive it.
    const line = accountMetaLine(
      {
        accountName: "alice",
        steamId64: "76561198000000001",
        lastLogin: "",
        showAccUsername: true,
        showSteamId: true,
        showLastLogin: true,
      },
      fmt,
    );
    expect(line).toBe("alice · 76561198000000001");
  });

  it("treats whitespace-only values as missing", () => {
    expect(accountMetaSegments({ accountName: "   ", showAccUsername: true }, fmt)).toEqual([]);
  });
});

describe("accountNotePreview", () => {
  it("is empty when note previews are off, even with a note stored", () => {
    expect(accountNotePreview({ showShortNotes: false, note: "shared with Bob" })).toBe("");
  });

  it("collapses newlines so a long note still occupies one row", () => {
    expect(
      accountNotePreview({ showShortNotes: true, note: "  shared with Bob\n\nexpires Friday " }),
    ).toBe("shared with Bob expires Friday");
  });
});

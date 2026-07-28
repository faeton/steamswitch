import { describe, expect, it } from "vitest";
import {
  canCommit,
  changedFields,
  countRows,
  decisionsFrom,
  keptFields,
  rowsWithConflicts,
  type ImportRow,
} from "./importPlan";

function row(partial: Partial<ImportRow>): ImportRow {
  return {
    index: 0,
    steamId64: "76561198000000001",
    action: "create",
    duplicateOfIndex: -1,
    fields: [],
    ...partial,
  };
}

describe("countRows", () => {
  it("accounts for every row", () => {
    const rows = [
      row({ index: 0, action: "create" }),
      row({ index: 1, action: "update" }),
      row({ index: 2, action: "skip" }),
      row({ index: 3, action: "invalid" }),
      row({ index: 4, action: "invalid" }),
    ];
    const counts = countRows(rows);
    expect(counts).toEqual({ create: 1, update: 1, skip: 1, invalid: 2, total: 5 });
    expect(counts.create + counts.update + counts.skip + counts.invalid).toBe(counts.total);
  });
});

describe("canCommit", () => {
  it("is false when nothing would be written", () => {
    expect(canCommit([row({ action: "invalid" }), row({ index: 1, action: "skip" })])).toBe(false);
  });

  it("is true as soon as one row creates or updates", () => {
    expect(canCommit([row({ action: "invalid" }), row({ index: 1, action: "create" })])).toBe(true);
  });

  it("is false for an empty plan", () => {
    expect(canCommit([])).toBe(false);
  });
});

describe("field partitioning", () => {
  const subject = row({
    action: "update",
    fields: [
      { field: "password", incoming: true, existing: true, outcome: "keep" },
      { field: "totp", incoming: true, existing: false, outcome: "fill" },
      { field: "email", incoming: true, existing: true, outcome: "overwrite" },
      { field: "label", incoming: false, existing: false, outcome: "none" },
    ],
  });

  it("counts fill and overwrite as changes", () => {
    expect(changedFields(subject)).toEqual(["totp", "email"]);
  });

  it("reports a kept field separately — it is the opposite of a change", () => {
    expect(keptFields(subject)).toEqual(["password"]);
  });

  it("finds the rows whose incoming value loses", () => {
    const clean = row({ index: 1, action: "create" });
    expect(rowsWithConflicts([subject, clean]).map((r) => r.index)).toEqual([0]);
  });

  it("tolerates a row with no field plan", () => {
    const bare = row({ action: "invalid", fields: null });
    expect(changedFields(bare)).toEqual([]);
    expect(keptFields(bare)).toEqual([]);
  });
});

describe("decisionsFrom", () => {
  it("sends only the rows the user actually changed", () => {
    expect(decisionsFrom({ 0: "default", 2: "overwrite", 5: "skip" })).toEqual([
      { index: 2, mode: "overwrite" },
      { index: 5, mode: "skip" },
    ]);
  });

  it("is empty when nothing was overridden", () => {
    expect(decisionsFrom({ 0: "default", 1: "default" })).toEqual([]);
  });
});

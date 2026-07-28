/**
 * Reading a bulk-import plan (REDESIGN_BRIEF.md A7).
 *
 * Pure functions over the plan the backend returns, kept out of the component so the two
 * things that are easy to get quietly wrong are testable without mounting anything: what the
 * confirm button is allowed to say, and how a row's per-field outcomes collapse into one line.
 *
 * Note what is *not* here. The plan carries no secrets — only whether each side has a value —
 * so nothing in this module can leak one. That is a property of the backend contract, not of
 * this file, but it is the reason the review table can be a plain data render.
 */

export type RowAction = "create" | "update" | "skip" | "invalid";
export type FieldOutcome = "fill" | "keep" | "overwrite" | "none";
export type RowMode = "default" | "overwrite" | "skip";

export type ImportFieldPlan = {
  field: string;
  incoming: boolean;
  existing: boolean;
  outcome: FieldOutcome;
};

export type ImportRow = {
  index: number;
  steamId64: string;
  accountName?: string;
  label?: string;
  action: RowAction;
  invalid?: string;
  duplicateOfIndex: number;
  fields?: ImportFieldPlan[] | null;
};

export type ImportPlan = {
  sessionId: string;
  rows: ImportRow[];
  totalRows: number;
  plaintextPath?: string;
  note?: string;
};

export type PlanCounts = {
  create: number;
  update: number;
  skip: number;
  invalid: number;
  total: number;
};

export function countRows(rows: ImportRow[]): PlanCounts {
  const counts: PlanCounts = { create: 0, update: 0, skip: 0, invalid: 0, total: rows.length };
  for (const row of rows) {
    counts[row.action] += 1;
  }
  return counts;
}

/**
 * Whether committing would do anything.
 *
 * A confirm button that stays enabled over a plan of nothing but invalid rows produces a
 * success toast for an import that wrote nothing, which reads as "it worked" and sends the
 * user looking for accounts that were never added.
 */
export function canCommit(rows: ImportRow[]): boolean {
  return rows.some((row) => row.action === "create" || row.action === "update");
}

/**
 * The fields a row will actually change, for the row's one-line summary.
 *
 * `keep` is excluded on purpose: it means the vault's value survives, so listing it under
 * "will change" would be exactly backwards. It is surfaced separately by [keptFields], because
 * "we did not take the password from your file" is a thing the user must be able to see
 * *before* committing, not deduce afterwards from an unchanged login.
 */
export function changedFields(row: ImportRow): string[] {
  return (row.fields ?? [])
    .filter((f) => f.outcome === "fill" || f.outcome === "overwrite")
    .map((f) => f.field);
}

export function keptFields(row: ImportRow): string[] {
  return (row.fields ?? []).filter((f) => f.outcome === "keep").map((f) => f.field);
}

/** Rows whose incoming value loses to one already in the vault, under the current modes. */
export function rowsWithConflicts(rows: ImportRow[]): ImportRow[] {
  return rows.filter((row) => keptFields(row).length > 0);
}

/**
 * The per-row mode map the backend expects, omitting rows left at the default.
 *
 * Sending only the overrides keeps the wire payload proportional to what the user actually
 * touched rather than to the size of the roster.
 */
export function decisionsFrom(modes: Record<number, RowMode>): { index: number; mode: RowMode }[] {
  return Object.entries(modes)
    .filter(([, mode]) => mode !== "default")
    .map(([index, mode]) => ({ index: Number(index), mode }));
}

/**
 * The monospaced meta line under an account's name (REDESIGN.md §6).
 *
 * The old account list gave each of these its own row, so the display toggles in Settings had
 * one row each to turn on and off. The compact tile has one slot, and the toggles kept being
 * read by Go and written to the row DTO — `showSteamId` and `showLastLogin` survived the
 * redesign as fields nothing rendered. This is where they land again.
 *
 * Order is fixed rather than following the order the toggles appear in Settings: the username
 * is what people recognise an account by, the SteamID is what they copy, and the timestamp is
 * the least specific of the three. A line that reorders itself as options are toggled would be
 * harder to scan than one that just gets shorter.
 */

export type AccountMetaSource = {
  accountName?: string;
  steamId64?: string;
  lastLogin?: string;
  showAccUsername?: boolean;
  showSteamId?: boolean;
  showLastLogin?: boolean;
};

/**
 * Builds the segments for one tile. Empty values are dropped rather than rendered as a gap, so
 * an account with no recorded last login does not show a dangling separator.
 *
 * `formatLastLogin` is injected because the formatter is locale-dependent and lives in a store;
 * keeping it out means this stays a pure function that a test can pin.
 */
export function accountMetaSegments(
  account: AccountMetaSource,
  formatLastLogin: (raw: string) => string,
): string[] {
  const out: string[] = [];
  if (account.showAccUsername) {
    const name = (account.accountName ?? "").trim();
    if (name) out.push(name);
  }
  if (account.showSteamId) {
    const id = (account.steamId64 ?? "").trim();
    if (id) out.push(id);
  }
  if (account.showLastLogin) {
    const when = formatLastLogin((account.lastLogin ?? "").trim());
    if (when) out.push(when);
  }
  return out;
}

/** Separator between segments — a middot, matching the status strip. */
export const META_SEPARATOR = " · ";

export function accountMetaLine(
  account: AccountMetaSource,
  formatLastLogin: (raw: string) => string,
): string {
  return accountMetaSegments(account, formatLastLogin).join(META_SEPARATOR);
}

/**
 * The note preview, on its own line.
 *
 * Notes are free text and can be long or contain newlines; folding one into the meta line
 * would push the identifiers off the end of a 420px tile. Collapsed to a single line here and
 * clipped by CSS, so a three-paragraph note still occupies exactly one row.
 */
export function accountNotePreview(account: { showShortNotes?: boolean; note?: string }): string {
  if (!account.showShortNotes) return "";
  return (account.note ?? "").replace(/\s+/g, " ").trim();
}

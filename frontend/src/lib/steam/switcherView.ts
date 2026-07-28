/**
 * What the switcher grid shows, and in what order (REDESIGN_BRIEF.md A6).
 *
 * Extracted from the page so the two rules that are easy to get quietly wrong are testable
 * without mounting anything: which accounts survive the search and tag filters, and which
 * number key maps to which account once they have.
 */
import type { SteamAccountRow } from "./types";

export type SwitcherFilters = {
  query: string;
  /** Tag name, or "" for all. */
  tag: string;
};

export const EMPTY_SWITCHER_FILTERS: SwitcherFilters = { query: "", tag: "" };

/**
 * Show the search box and tag chips only when they earn their space.
 *
 * The brief is explicit that this must not be a density tax the user pays at two accounts:
 * "search + tag filter appear when the roster is large or tags exist". Below the threshold
 * the controls would be pure chrome above a grid you can read in one glance.
 */
export const FILTER_THRESHOLD = 8;

export function shouldShowFilters(accountCount: number, tagCount: number): boolean {
  return accountCount >= FILTER_THRESHOLD || tagCount > 0;
}

function haystack(account: SteamAccountRow): string {
  return [account.displayName, account.personaName, account.accountName, account.steamId64]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function hasTag(account: SteamAccountRow, tag: string): boolean {
  return (account.tags ?? []).some((t) => t.name === tag);
}

/** Apply the search and tag filters, preserving the incoming order. */
export function filterAccounts(
  accounts: SteamAccountRow[],
  filters: SwitcherFilters,
): SteamAccountRow[] {
  const q = filters.query.trim().toLowerCase();
  return accounts.filter(
    (a) => (!q || haystack(a).includes(q)) && (!filters.tag || hasTag(a, filters.tag)),
  );
}

/** Every distinct tag across the roster, sorted, for the filter chips. */
export function tagsInUse(accounts: SteamAccountRow[]): string[] {
  const seen = new Set<string>();
  for (const a of accounts) {
    for (const tag of a.tags ?? []) {
      if (tag.name) seen.add(tag.name);
    }
  }
  return [...seen].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
}

export const MAX_QUICK_SWITCH = 9;

/**
 * The `1`–`9` quick-switch bindings.
 *
 * **Numbers follow visible order**, which is the answer to the brief's open question (A14) and
 * has to be stated in the UI, because after a filter the numbers visibly renumber. The
 * alternative — sticky pins — would leave gaps in the sequence and mean the digit shown on a
 * tile is not the digit's position, which is worse at the two-to-five accounts most users have.
 *
 * The account Steam is *already* signed in as is skipped: pressing its number would be a no-op,
 * and consuming a digit for a no-op wastes the shortest keys on the least useful target.
 * Returns a 1-based index per SteamID64; unnumbered accounts are simply absent.
 */
export function quickSwitchIndex(
  visible: SteamAccountRow[],
  currentId: string,
): Record<string, number> {
  const out: Record<string, number> = {};
  let n = 0;
  for (const account of visible) {
    if (account.steamId64 === currentId) continue;
    if (n >= MAX_QUICK_SWITCH) break;
    n += 1;
    out[account.steamId64] = n;
  }
  return out;
}

/**
 * The quick-switch digit a key event names, or 0 when it names none.
 *
 * Reads `code` before `key` because `key` is the *typed character*: with Shift held, the
 * top-row digits produce `!`, `@`, `#`… on a US layout and different symbols elsewhere. The
 * documented Shift+N shortcut (open detail instead of switching) therefore never fired when
 * matched on `key` alone. `code` is the physical key, which is what a positional shortcut
 * means. `key` remains the fallback for keypad digits and layouts that report no `code`.
 */
export function quickSwitchDigit(e: Pick<KeyboardEvent, "key" | "code">): number {
  const fromCode = /^(Digit|Numpad)([1-9])$/.exec(e.code ?? "");
  if (fromCode) {
    return Number(fromCode[2]);
  }
  return /^[1-9]$/.test(e.key) ? Number(e.key) : 0;
}

/** The account a given digit switches to, or undefined when that digit is unbound. */
export function accountForQuickSwitch(
  visible: SteamAccountRow[],
  currentId: string,
  digit: number,
): SteamAccountRow | undefined {
  if (!Number.isInteger(digit) || digit < 1 || digit > MAX_QUICK_SWITCH) {
    return undefined;
  }
  const index = quickSwitchIndex(visible, currentId);
  return visible.find((a) => index[a.steamId64] === digit);
}

/**
 * The Vault page's search and filter model (REDESIGN_BRIEF.md A6).
 *
 * Kept out of the component so "which rows does this show" is testable without mounting
 * anything — the filter combination rules are exactly the kind of thing that looks obviously
 * right and quietly drops a row.
 */
import type { Summary } from "../../../bindings/steamswitch/internal/vault/models";
import { healthState, type HealthTone } from "./health";

export type HealthFilter = "any" | "ok" | "attention" | "unchecked";

export type VaultFilters = {
  query: string;
  health: HealthFilter;
  /** Only entries with a stored password. */
  hasPassword: boolean;
  /** Only entries that also have a home tile (the inverse of `standalone`). */
  onSwitcher: boolean;
};

export const EMPTY_FILTERS: VaultFilters = {
  query: "",
  health: "any",
  hasPassword: false,
  onSwitcher: false,
};

export function hasActiveFilters(f: VaultFilters): boolean {
  return f.query.trim() !== "" || f.health !== "any" || f.hasPassword || f.onSwitcher;
}

/**
 * Substring match across the three things a person actually types.
 *
 * Deliberately not fuzzy: a SteamID64 is 17 digits, and fuzzy matching over digit strings
 * makes every account match every query. The switcher's own search is fuzzy because it is
 * matching human names; this one is matching identifiers.
 */
function matchesQuery(entry: Summary, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [entry.label, entry.accountName, entry.steamId64, entry.emailAddress]
    .some((field) => (field ?? "").toLowerCase().includes(q));
}

function matchesHealth(entry: Summary, filter: HealthFilter): boolean {
  if (filter === "any") return true;
  const tone: HealthTone = healthState(entry.health).tone;
  switch (filter) {
    case "ok":
      return tone === "ok";
    // "Needs attention" is the useful bucket: warn and fail are different severities but the
    // same question — which of my accounts do I have to do something about?
    case "attention":
      return tone === "warn" || tone === "fail";
    case "unchecked":
      return tone === "neutral";
    default:
      return true;
  }
}

export function filterVaultEntries(entries: Summary[], f: VaultFilters): Summary[] {
  return entries.filter(
    (entry) =>
      matchesQuery(entry, f.query) &&
      matchesHealth(entry, f.health) &&
      (!f.hasPassword || entry.hasPassword) &&
      (!f.onSwitcher || !entry.standalone),
  );
}

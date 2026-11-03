import { writable } from "svelte/store";
import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";

/**
 * The SteamID64s on this PC's switcher — the accounts Steam itself has signed into here.
 *
 * `null` means not known yet (or the last read failed). Callers must treat that as "don't
 * know", never as "empty": an empty set would tell the Vault page that every account is
 * vault-only.
 */
export const switcherRoster = writable<ReadonlySet<string> | null>(null);

/** Publish the roster from a list the caller already read (the switcher page does). */
export function setSwitcherRoster(ids: Iterable<string>): void {
  switcherRoster.set(new Set(ids));
}

/** Read the roster for pages that are opened without the switcher having loaded it. */
export async function loadSwitcherRoster(): Promise<void> {
  try {
    const payload = await SteamService.GetSteamAccountsList();
    setSwitcherRoster((payload.accounts ?? []).map((r) => r.steamId64));
  } catch {
    // Keep whatever was known; the Vault page falls back to the stored flag on `null`.
  }
}

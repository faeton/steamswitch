/**
 * SteamID64 validation for hand-entered identity fields (REDESIGN_BRIEF.md A6).
 *
 * The vault editor can be opened blank, which means a person types this by hand, which means
 * a typo has to be caught before it becomes an entry keyed on a nonexistent account.
 */

/**
 * Individual SteamID64s are the 64-bit value `0x0110000100000000 + accountId`, so every one
 * of them is exactly 17 digits and starts `7656119`. That prefix check is what separates a
 * real ID from a 17-digit number someone pasted out of the wrong column.
 */
const STEAM_ID64_RE = /^7656119\d{10}$/;

export function isValidSteamId64(value: string): boolean {
  return STEAM_ID64_RE.test(value.trim());
}

/**
 * Pull a SteamID64 out of whatever the user pasted — the bare ID, or a profile URL.
 *
 * Vanity URLs (`/id/somename`) resolve only through Valve's API and are deliberately not
 * handled here: this module is offline-safe, and silently returning nothing for a vanity URL
 * is better than pretending to parse one.
 */
export function extractSteamId64(input: string): string {
  const trimmed = input.trim();
  if (isValidSteamId64(trimmed)) {
    return trimmed;
  }
  const fromUrl = trimmed.match(/profiles\/(7656119\d{10})/);
  return fromUrl ? fromUrl[1] : "";
}

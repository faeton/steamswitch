/**
 * Home / Shared roles (REDESIGN.md §2), and the ordering they imply for the compact list.
 *
 * Pure functions over a role map so the tile list can be unit-tested without mounting a
 * component or touching the backend.
 */

export type AccountRoleMap = {
  homeSteamId64: string;
  sharedIds: readonly string[];
};

export type AccountRole = "home" | "shared" | "plain";

export const EMPTY_ROLES: AccountRoleMap = { homeSteamId64: "", sharedIds: [] };

export function roleOf(roles: AccountRoleMap, steamId64: string): AccountRole {
  const id = (steamId64 ?? "").trim();
  if (!id) {
    return "plain";
  }
  if ((roles.homeSteamId64 ?? "").trim() === id) {
    return "home";
  }
  return roles.sharedIds.some((s) => (s ?? "").trim() === id) ? "shared" : "plain";
}

/** True when switching *to* this account should carry the Home kit along. */
export function kitTravelsTo(roles: AccountRoleMap, steamId64: string): boolean {
  return roleOf(roles, steamId64) === "shared" && (roles.homeSteamId64 ?? "").trim() !== "";
}

const ROLE_RANK: Record<AccountRole, number> = { home: 0, shared: 1, plain: 2 };

/**
 * Order the compact list: Home first, then shared accounts, then the rest — each group
 * keeping the user's saved order (REDESIGN.md §3 shows exactly this arrangement).
 *
 * Stable within a group, so a saved drag order still means something.
 */
export function orderAccountIds(roles: AccountRoleMap, ids: readonly string[]): string[] {
  return ids
    .map((id, index) => ({ id, index, rank: ROLE_RANK[roleOf(roles, id)] }))
    .sort((a, b) => (a.rank !== b.rank ? a.rank - b.rank : a.index - b.index))
    .map((entry) => entry.id);
}

/** Roles are mutually exclusive: the kit's source cannot also be one of its destinations. */
export function canMarkShared(roles: AccountRoleMap, steamId64: string): boolean {
  return roleOf(roles, steamId64) !== "home";
}

/** What the game-config page shows about a game. Mirrors `steam.GameConfigInfo`. */
export type GameInfo = {
  appId: string;
  name: string;
  parts: { id: string; cloudSynced: boolean; machineWide: boolean }[];
  accounts: {
    steamId64: string;
    parts: { id: string; files: number; sizeBytes: number }[];
    other: string[];
  }[];
  gameRunning: boolean;
  steamRunning: boolean;
};

export type ConfigAccount = { steamId64: string; accountName: string; displayName: string; currentSession?: boolean };

/** The machine-wide folder: saved in snapshots, never copied between accounts. */
export const PART_GLOBAL = "globalcfg";

type Translate = (key: string, vars?: Record<string, string | number>) => string;

export function configAccountLabel(accounts: ConfigAccount[], id: string): string {
  const a = accounts.find((x) => x.steamId64 === id);
  return a ? a.displayName?.trim() || a.accountName?.trim() || id : id;
}

export function configPartsSummary(t: Translate, parts: string[]): string {
  return parts.map((p) => t(`GameCfg_Part_${p}`)).join(", ");
}

/** Backend errors carry an i18n key on the first line. */
export function configErrorMessage(t: Translate, e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e);
  const key = raw.split("\n")[0]?.trim() ?? raw;
  const translated = t(key);
  return translated === key ? raw : translated;
}

export type ConfigProfile = "dota" | "cs2" | "generic";

/** Which description of a game's folders is true. Unknown games get the cautious one. */
export function configProfile(appId: string): ConfigProfile {
  if (appId === "570") return "dota";
  if (appId === "730") return "cs2";
  return "generic";
}

export function partHintKey(profile: ConfigProfile, part: string): string {
  return `GameCfg_Part_${part}_Hint_${profile}`;
}

export function selectionHasCloudPart(info: GameInfo | null, parts: string[]): boolean {
  return (info?.parts ?? []).some((p) => p.cloudSynced && parts.includes(p.id));
}

/**
 * Whether a write to this account could meet Steam Cloud: only the account signed in to a running
 * Steam syncs. Mirrors `destCloudGuard`, so nobody is warned about a write that will not be
 * refused. An account the list does not know counts as signed in, as it does there.
 */
export function cloudRiskFor(
  info: GameInfo | null,
  parts: string[],
  accounts: { steamId64: string; currentSession?: boolean }[],
  destId: string,
): boolean {
  if (!info?.steamRunning || !selectionHasCloudPart(info, parts)) return false;
  const someoneSignedIn = accounts.some((a) => a.currentSession);
  return !someoneSignedIn || accounts.some((a) => a.steamId64 === destId && a.currentSession);
}

/**
 * Whether Copy may be pressed. The backend refuses all of this too. Disabling first means nobody
 * accepts a cloud warning for a copy that was never going to run.
 */
export function canCopyConfig(s: {
  busy: boolean;
  info: GameInfo | null;
  sourceId: string;
  destId: string;
  copyParts: string[];
  sourceParts: string[];
}): boolean {
  return (
    !s.busy &&
    !!s.info &&
    !s.info.gameRunning &&
    !!s.sourceId &&
    !!s.destId &&
    s.sourceId !== s.destId &&
    s.copyParts.some((p) => s.sourceParts.includes(p))
  );
}

export function formatBytes(n: number): string {
  if (!n) return "0 KB";
  const kb = n / 1024;
  if (kb < 1024) return `${Math.max(1, Math.round(kb))} KB`;
  return `${(kb / 1024).toFixed(1)} MB`;
}

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

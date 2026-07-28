/**
 * The backend's verdict on who Steam is signed in as (REDESIGN_BRIEF.md A4).
 *
 * Mirrors `steam.SessionState` in Go. The strings are frozen on both sides — they cross the
 * bindings — and the reason this is a named type rather than a bare string is that exactly one
 * of the four values licenses the UI to make a claim.
 */
export type SessionState = "ok" | "none" | "mismatch" | "unknown";

export type SessionVerdict = {
  state: SessionState;
  steamId64: string;
  conflictAccountName?: string;
};

/** What the UI holds before the first list load returns. */
export const UNLOADED_SESSION: SessionVerdict = { state: "none", steamId64: "" };

/**
 * Normalise whatever the bindings hand back.
 *
 * An unrecognised state resolves to "unknown" rather than "ok", because the failure mode this
 * whole feature exists to prevent is a confident claim the app cannot support — and a build
 * whose backend grew a fifth state it has never heard of is precisely a case where it cannot.
 */
export function asSessionVerdict(raw: unknown): SessionVerdict {
  const value = (raw ?? {}) as Partial<SessionVerdict>;
  const state: SessionState =
    value.state === "ok" ||
    value.state === "none" ||
    value.state === "mismatch" ||
    value.state === "unknown"
      ? value.state
      : "unknown";
  return {
    state,
    steamId64: typeof value.steamId64 === "string" ? value.steamId64 : "",
    conflictAccountName:
      typeof value.conflictAccountName === "string" ? value.conflictAccountName : "",
  };
}

/**
 * Whether the switcher may present an account as *the* current one.
 *
 * Kept as a function, and used everywhere the question is asked, so a new surface cannot
 * quietly reintroduce "there is an account object, therefore it is current".
 */
export function assertsCurrentAccount(state: SessionState): boolean {
  return state === "ok";
}

/**
 * Steam's answer on whether an account actually signed in.
 *
 * Mirrors `steam.SignInState` in Go (signin_watch.go). The strings cross the bindings. Only
 * "confirmed" licenses the word "signed in"; everything else says what is actually known.
 */
export type SignInState = "none" | "waiting" | "confirmed" | "needsSignIn" | "unconfirmed" | "steamExited";

export type SignInStatus = { steamId64: string; state: SignInState; reason?: string };

export const NO_SIGN_IN: SignInStatus = { steamId64: "", state: "none", reason: "" };

const STATES: readonly SignInState[] = ["none", "waiting", "confirmed", "needsSignIn", "unconfirmed", "steamExited"];

/** An unknown state from a newer backend must not read as a success. */
export function asSignInStatus(raw: unknown): SignInStatus {
  const v = (raw ?? {}) as Partial<SignInStatus>;
  const state = v.state as SignInState;
  return {
    steamId64: typeof v.steamId64 === "string" ? v.steamId64 : "",
    state: STATES.includes(state) ? state : v.state ? "unconfirmed" : "none",
    reason: typeof v.reason === "string" ? v.reason : "",
  };
}

export type DockTone = "waiting" | "ok" | "attention" | "neutral";

export type DockResult = { titleKey: string; subKey: string; tone: DockTone; reason: string };

/** What the dock may say once the engine is done. */
export function dockResult(state: SignInState, reason: string, launched: boolean): DockResult {
  const r = (titleKey: string, subKey: string, tone: DockTone): DockResult => ({ titleKey, subKey, tone, reason });
  switch (state) {
    case "confirmed":
      return r("Switch_Done_Title", "Switch_Done_Sub", "ok");
    case "waiting":
      return r("Switch_SignIn_Waiting_Title", "Switch_SignIn_Waiting_Sub", "waiting");
    case "needsSignIn":
      if (reason === "noStoredLogin") {
        return r("Switch_SignIn_Needs_Title", "Switch_SignIn_NoStored_Sub", "attention");
      }
      if (!reason || reason === "tokenRejected") {
        return r("Switch_SignIn_Needs_Title", "Switch_SignIn_Rejected_Sub", "attention");
      }
      return r("Switch_SignIn_Needs_Title", "Switch_SignIn_RejectedReason_Sub", "attention");
    case "steamExited":
      return r("Switch_SignIn_Exited_Title", "Switch_SignIn_Exited_Sub", "attention");
    case "unconfirmed":
      return r("Switch_SignIn_Unconfirmed_Title", "Switch_SignIn_Unconfirmed_Sub", "attention");
    default:
      return launched
        ? r("Switch_SignIn_Unconfirmed_Title", "Switch_SignIn_Unconfirmed_Sub", "attention")
        : r("Switch_Done_TitleNoLaunch", "Switch_Done_SubNoLaunch", "neutral");
  }
}

export type SignInMark = "done" | "active" | "pending" | "attention";

export function signInMark(state: SignInState, finished: boolean): SignInMark {
  if (!finished) return "pending";
  if (state === "confirmed") return "done";
  return state === "waiting" ? "active" : "attention";
}

/** The login line under the current account's name. */
export function heroLoginCopy(status: SignInStatus, accountId: string): { key: string; warn: boolean } {
  if (status.steamId64 && status.steamId64 === accountId) {
    switch (status.state) {
      case "waiting":
        return { key: "Hero_SigningInAs", warn: false };
      case "needsSignIn":
        return { key: "Hero_NeedsPassword", warn: true };
      case "unconfirmed":
      case "steamExited":
        return { key: "Hero_SignInUnconfirmed", warn: true };
    }
  }
  return { key: "Hero_SignedInAs", warn: false };
}

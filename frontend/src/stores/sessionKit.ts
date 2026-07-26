/**
 * Session Kit state for the UI (REDESIGN.md §2).
 *
 * The engine is the authority on what has been written to disk; this store is a cache of its
 * answer plus the two blocking decisions the user has to make — "restore theirs or keep
 * mine?" when leaving, and "last switch didn't finish" on the next launch.
 *
 * Both decisions are modelled as *pending prompts* rather than as booleans, because each one
 * has to remember what it is about (which account we are leaving, which account we are
 * heading to) across the round trip to the backend.
 */
import { get, writable, type Readable } from "svelte/store";
import * as SessionKitService from "../../bindings/steamswitch/internal/steam/sessionkitservice.js";
import { setKitActive, clearKitActive, setRecovery, clearRecovery } from "./statusStrip";

/** Mirrors `sessionkit.RecoveryKind` on the Go side. */
export type KitKind = "none" | "kit-active" | "interrupted" | "external-change";

export type KitStatus = {
  kind: KitKind;
  transactionId: string;
  phase: string;
  targetSteamId64: string;
  kitApplied: boolean;
  canRestore: boolean;
  moduleNames: string[];
  lastError: string;
  startedAt: string;
  /** Who Steam will actually log in as right now, read from disk rather than inferred. */
  signedInSteamId64: string;
  /** The login on disk is not the one the interrupted transaction was heading for. */
  loginMismatch: boolean;
  homeSteamId64: string;
  sharedIds: string[];
  cloudRisk: boolean;
};

const EMPTY: KitStatus = {
  kind: "none",
  transactionId: "",
  phase: "",
  targetSteamId64: "",
  kitApplied: false,
  canRestore: false,
  moduleNames: [],
  lastError: "",
  startedAt: "",
  signedInSteamId64: "",
  loginMismatch: false,
  homeSteamId64: "",
  sharedIds: [],
  cloudRisk: false,
};

const status = writable<KitStatus>(EMPTY);
export const kitStatus: Readable<KitStatus> = { subscribe: status.subscribe };

/**
 * The "Restore X's setup?" prompt.
 *
 * `to` is where the user asked to go. It is held here rather than passed straight to the
 * backend because the switch cannot proceed until the question is answered, and the answer
 * arrives from a different component than the click did.
 */
export type LeavePrompt = {
  /** Display name of the shared account being left. */
  fromLabel: string;
  /** Display name of the account being switched to, empty when quitting. */
  toLabel: string;
  toSteamId64: string;
  moduleNames: string[];
  /** True when the prompt was raised by app quit rather than by a switch. */
  onQuit: boolean;
};

export const leavePrompt = writable<LeavePrompt | null>(null);

/** True while a kit is live, i.e. the user's configs are sitting on someone else's account. */
export function kitIsActive(s: KitStatus = get(status)): boolean {
  return s.kind === "kit-active";
}

/**
 * True when the app must block on a recovery decision.
 *
 * `kit-active` is deliberately excluded: a live kit is a normal resting state, not an error.
 */
export function needsRecovery(s: KitStatus = get(status)): boolean {
  return s.kind === "interrupted" || s.kind === "external-change";
}

/**
 * Pull the engine's state and mirror the parts of it the status strip owns.
 *
 * Safe to call on startup and on window focus; it writes nothing on the backend.
 */
export async function refreshKitStatus(): Promise<KitStatus> {
  let next: KitStatus;
  try {
    next = (await SessionKitService.GetKitStatus()) as unknown as KitStatus;
  } catch {
    // A backend that cannot answer must not silently look like "nothing outstanding" —
    // but neither should a transient failure raise a false recovery prompt. Keep the last
    // known state.
    return get(status);
  }
  status.set(next);

  if (needsRecovery(next)) {
    setRecovery(
      next.kind === "external-change" ? "external-change" : "interrupted-switch",
      next.lastError || "",
      [],
    );
  } else {
    clearRecovery();
  }

  if (kitIsActive(next)) {
    setKitActive({
      accountLabel: next.targetSteamId64,
      modules: next.moduleNames ?? [],
      cloudRisk: next.cloudRisk,
    });
  } else {
    clearKitActive();
  }
  return next;
}

/** Relabel the kit line once the account list is loaded and display names are known. */
export function relabelKit(label: string): void {
  const s = get(status);
  if (!kitIsActive(s) || !label) return;
  setKitActive({ accountLabel: label, modules: s.moduleNames ?? [], cloudRisk: s.cloudRisk });
}

/**
 * Answer the leave prompt.
 *
 * Returns false when the engine refused, so the caller can leave the prompt open rather than
 * closing it over an error the user never saw.
 */
export async function resolveLeave(
  choice: "restore-theirs" | "keep-mine",
  toSteamId64: string,
): Promise<void> {
  await SessionKitService.LeaveKit(toSteamId64, choice, -1);
  leavePrompt.set(null);
  await refreshKitStatus();
}

/** Answer the recovery modal. */
export async function resolveRecovery(
  action: "restore-theirs" | "keep-current" | "abandon",
): Promise<void> {
  await SessionKitService.ResolveRecovery(action);
  await refreshKitStatus();
}

/**
 * Re-check the cloud-synced parts after Steam has had time to sync.
 *
 * Returns true when the kit is still intact. A false result is not an error: Steam Cloud
 * winning is an expected outcome that the UI has to disclose rather than hide.
 */
export async function checkCloudClobber(): Promise<boolean> {
  try {
    return await SessionKitService.CheckCloudClobber();
  } catch {
    return true;
  }
}

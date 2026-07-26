/**
 * The status strip — the single source of truth for what the app is doing (REDESIGN.md §3).
 *
 * It replaces both `ActionBar`'s status text and the toast-on-everything pattern. Toasts are
 * reserved for rare async results the user has already navigated away from; anything on the
 * main path narrates here instead.
 *
 * State is a discriminated union rather than a bag of flags so that impossible combinations
 * (mid-switch *and* blocked on recovery) cannot be represented. `setX` helpers apply the
 * priority rules below rather than overwriting blindly.
 *
 *   recovery  >  switching  >  error  >  kitActive  >  idle
 *
 * Recovery wins outright: REDESIGN.md §2 requires an unfinished switch to block switching
 * until the user resolves it.
 */
import { derived, get, writable, type Readable } from "svelte/store";

/** A button rendered on the strip. `id` is dispatched to the host page, not handled here. */
export type StatusAction = {
  id: string;
  labelKey: string;
  primary?: boolean;
};

export type RecoveryReason =
  /** A transaction journal was left mid-flight by a crash or forced shutdown. */
  | "interrupted-switch"
  /** Live files no longer match the hashes recorded at apply time. */
  | "external-change"
  /** Steam or a game is still running and a mutation is queued behind it. */
  | "steam-running";

export type StatusStripState =
  | {
      kind: "idle";
      /** Display name of the account Steam is currently set to log in as. */
      accountLabel: string;
      isHome: boolean;
      steamRunning: boolean;
    }
  | {
      kind: "switching";
      /** Target account display name, shown for the whole run. */
      toLabel: string;
      /** Already-localised narration for the current step ("Saving their setup…"). */
      phase: string;
    }
  | {
      kind: "kit-active";
      /** The shared account currently carrying the user's kit. */
      accountLabel: string;
      /** Display names of the game modules in the kit, e.g. ["Dota 2"]. */
      modules: string[];
      /** True once a cloud-risk part was written — status must not claim durability. */
      cloudRisk: boolean;
      /** Whether Steam is up right now. Kept current for the life of the kit. */
      steamRunning?: boolean;
      /** Optional monospace nerd line, e.g. `kit:dota local+cloud · snap:a3f2 · 14:32`. */
      nerd?: string;
    }
  | {
      kind: "recovery";
      reason: RecoveryReason;
      /** Already-localised one-line explanation. */
      detail: string;
      actions: StatusAction[];
    }
  | {
      kind: "error";
      /** Already-localised message. */
      message: string;
      action?: StatusAction;
    };

/**
 * A Steam Guard code fetched for the account being switched to (VAULT.md §4.4).
 *
 * Deliberately a separate slot rather than another `kind`: it has to be able to appear
 * *during* a switch, so making it compete for the single status line would either hide the
 * narration or hide the code.
 */
export type GuardCodeSlot =
  | { state: "fetching"; accountLabel: string; elapsedSeconds: number }
  | { state: "ready"; accountLabel: string; code: string }
  | { state: "failed"; accountLabel: string; message: string };

const IDLE: StatusStripState = {
  kind: "idle",
  accountLabel: "",
  isHome: false,
  steamRunning: false,
};

const internal = writable<StatusStripState>(IDLE);

/** Current strip state. Read-only by design; mutate through the helpers below. */
export const statusStrip: Readable<StatusStripState> = { subscribe: internal.subscribe };

export const guardCode = writable<GuardCodeSlot | null>(null);

/**
 * The id of the last strip button the user pressed.
 *
 * The strip renders actions but must not know what they do — recovery is owned by the
 * Session Kit service and retry by whichever page started the operation. `App.svelte` funnels
 * the component's event in here and pages react to it, which keeps the strip mountable
 * anywhere without wiring handlers through the tree.
 */
export const statusStripAction = writable<string>("");

/**
 * The account a failed switch was aimed at, so "Retry" still works after navigating away.
 *
 * The strip is mounted app-wide but the switch is started by the accounts page. Holding this
 * on the page meant failing a switch, opening Tools and pressing Retry cleared the error and
 * retried nothing, because the page that knew the target had been unmounted.
 */
export const retryTarget = writable<string>("");

/**
 * Idle facts, remembered separately so that settling out of a switch or an error can restore
 * the "Now: X (Home) · Steam running" line without the caller having to re-supply it.
 */
const idleFacts = writable<{ accountLabel: string; isHome: boolean; steamRunning: boolean }>({
  accountLabel: "",
  isHome: false,
  steamRunning: false,
});

/** Kit state, remembered for the same reason: it outlives individual switches. */
const kitFacts = writable<Extract<StatusStripState, { kind: "kit-active" }> | null>(null);

function isRecovery(s: StatusStripState): boolean {
  return s.kind === "recovery";
}

/** The state to fall back to when nothing more urgent is happening. */
function restingState(): StatusStripState {
  const kit = get(kitFacts);
  if (kit) {
    return kit;
  }
  return { kind: "idle", ...get(idleFacts) };
}

/** Update the "Now: …" facts. Applies immediately only if nothing more urgent is showing. */
export function setIdleFacts(facts: {
  accountLabel: string;
  isHome: boolean;
  steamRunning: boolean;
}): void {
  idleFacts.set(facts);
  const cur = get(internal);
  if (cur.kind === "idle") {
    internal.set({ kind: "idle", ...facts });
    return;
  }
  // A kit can stay active for hours, and Steam starting or stopping during that time is
  // exactly what the user needs to know before touching configs. Updating the remembered
  // facts alone would leave the strip showing a Steam state from before the kit was applied.
  if (cur.kind === "kit-active") {
    const kit = get(kitFacts);
    if (kit) {
      internal.set({ ...kit, steamRunning: facts.steamRunning });
      kitFacts.set({ ...kit, steamRunning: facts.steamRunning });
    }
  }
}

/**
 * Enter the narrated switching state, returning false if the switch must not start.
 *
 * This is the single-flight gate for switching, not just a display update: it refuses while
 * a recovery is blocking *and* while a switch is already running. Two rapid clicks would
 * otherwise both pass a caller-side check and issue concurrent swaps against the same live
 * Steam files.
 */
export function beginSwitch(toLabel: string, initialPhase = ""): boolean {
  const cur = get(internal);
  if (cur.kind === "recovery" || cur.kind === "switching") {
    return false;
  }
  // Starting any switch retires the previous failure: the Retry button is about to be
  // replaced by narration, and leaving the target set would let a later Retry fire at an
  // account the user has since moved on from.
  retryTarget.set("");
  internal.set({ kind: "switching", toLabel, phase: initialPhase });
  return true;
}

/**
 * Feed an already-localised backend status line in as narration.
 *
 * Only meaningful during a switch — outside one the backend's `action-bar-status` events are
 * noise from tools and background work, which belong in toasts, not on the strip.
 */
export function narrate(phase: string): void {
  const cur = get(internal);
  if (cur.kind !== "switching") {
    return;
  }
  internal.set({ ...cur, phase });
}

/** Leave the switching state successfully, settling to kit-active or idle. */
export function endSwitch(): void {
  const cur = get(internal);
  if (cur.kind !== "switching") {
    return;
  }
  internal.set(restingState());
}

/** Show a failure. Sticky until retried, dismissed, or superseded by something more urgent. */
export function setError(message: string, action?: StatusAction): void {
  if (isRecovery(get(internal))) {
    return;
  }
  internal.set({ kind: "error", message, action });
}

export function clearError(): void {
  if (get(internal).kind !== "error") {
    return;
  }
  internal.set(restingState());
}

/** Record that the user's kit is live on a shared account. Persists across switches. */
export function setKitActive(kit: {
  accountLabel: string;
  modules: string[];
  cloudRisk: boolean;
  nerd?: string;
}): void {
  const next: Extract<StatusStripState, { kind: "kit-active" }> = {
    kind: "kit-active",
    steamRunning: get(idleFacts).steamRunning,
    ...kit,
  };
  kitFacts.set(next);
  const cur = get(internal);
  if (cur.kind === "idle" || cur.kind === "kit-active") {
    internal.set(next);
  }
}

export function clearKitActive(): void {
  kitFacts.set(null);
  const cur = get(internal);
  if (cur.kind === "kit-active") {
    internal.set(restingState());
  }
}

/** Block the app on an unresolved transaction. Overrides everything. */
export function setRecovery(reason: RecoveryReason, detail: string, actions: StatusAction[]): void {
  internal.set({ kind: "recovery", reason, detail, actions });
}

export function clearRecovery(): void {
  if (!isRecovery(get(internal))) {
    return;
  }
  internal.set(restingState());
}

/** Test seam: drop every remembered fact and return to a bare idle strip. */
export function resetStatusStrip(): void {
  idleFacts.set({ accountLabel: "", isHome: false, steamRunning: false });
  kitFacts.set(null);
  guardCode.set(null);
  internal.set(IDLE);
}

/**
 * True while the account list must not accept a switch: mid-flight, or blocked on a recovery
 * decision the user has not made yet.
 */
export const switchingBlocked = derived(
  statusStrip,
  ($s) => $s.kind === "switching" || $s.kind === "recovery",
);

/** Severity for styling. Never the *only* signal — the strip always carries text too. */
export const statusTone = derived(statusStrip, ($s): "neutral" | "busy" | "warn" | "error" => {
  switch ($s.kind) {
    case "switching":
      return "busy";
    case "kit-active":
      return "warn";
    case "recovery":
      return $s.reason === "steam-running" ? "warn" : "error";
    case "error":
      return "error";
    default:
      return "neutral";
  }
});

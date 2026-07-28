/**
 * The switch, as steps a person can watch (REDESIGN_BRIEF.md A5 flow 2, A6 "Switch experience").
 *
 * The brief asks for "the steps as they happen" during a multi-second operation that closes
 * and relaunches Steam. The steps below are **the ones the engine actually reports** — each
 * maps to a real `e.phase(...)` call in `internal/sessionkit/engine.go`, which is the path the
 * switcher takes (`SessionKitService.SwitchTo` → `Engine.Enter`). That constraint is
 * deliberate: inventing finer-grained steps than the backend emits produces a progress bar
 * that jumps or stalls, which is worse than a coarse one that is true.
 *
 * What the engine emits, in order:
 *
 *   plain switch   Status_ClosingPlatform → Status_ActionBar_UpdatingSteamLogin
 *                  → Status_StartingPlatform
 *   with a kit     …ClosingPlatform → Status_Kit_SavingTheirSetup → Status_Kit_ApplyingKit
 *                  → …UpdatingSteamLogin → …StartingPlatform
 *
 * So the kit work is *conditional*, and the login write is a single reported phase rather than
 * the separate save/clear/restore the design doc sketches. Drawing four fixed steps meant the
 * dock sat on "Closing Steam" through the whole kit stage and then labelled the login write
 * "Saving …" — mechanically monotonic, but describing the wrong operation.
 *
 * A step is never marked done because time passed; it is marked done because a later step was
 * reported.
 */

export type SwitchStepId = "close" | "kit" | "login" | "launch";

/** Every step, in order. `kit` is hidden until the engine reports it — see `visibleSteps`. */
export const SWITCH_STEPS: SwitchStepId[] = ["close", "kit", "login", "launch"];

/** i18n key for each step's label. `{to}` is filled by the caller where present. */
export const SWITCH_STEP_LABEL_KEYS: Record<SwitchStepId, string> = {
  close: "Switch_Step_Close",
  kit: "Switch_Step_Kit",
  login: "Switch_Step_Login",
  launch: "Switch_Step_Launch",
};

/**
 * Which step a backend status key belongs to.
 *
 * Keys not listed leave the current step alone rather than resetting it — the engine emits
 * per-file chatter (`Status_UpdatingFile` for several files) inside a single step, and
 * treating an unknown key as "step 0" would make the bar walk backwards.
 */
const KEY_TO_STEP: Record<string, SwitchStepId> = {
  Status_Init: "close",
  Status_ClosingPlatform: "close",
  Status_ClosingPlatformFailed: "close",

  Status_Kit_SavingTheirSetup: "kit",
  Status_Kit_ApplyingKit: "kit",
  Status_Kit_CheckingChanges: "kit",
  Status_Kit_RestoringTheirSetup: "kit",

  Status_ActionBar_UpdatingSteamLogin: "login",
  Status_UpdatingFile: "login",
  Status_UpdatingRegistry: "login",
  Status_ActionBar_UpdatingSteamPersona: "login",

  Status_StartingPlatform: "launch",
};

export function stepForStatusKey(key: string): SwitchStepId | null {
  return KEY_TO_STEP[key] ?? null;
}

export function stepIndex(id: SwitchStepId): number {
  return SWITCH_STEPS.indexOf(id);
}

/**
 * Advance monotonically.
 *
 * The engine can report a step it already passed — `Status_UpdatingFile` fires several times
 * across two different writes — and a naive assignment would make the bar stutter backwards.
 * Progress only ever moves forward within one switch.
 */
export function advanceStep(current: SwitchStepId | null, next: SwitchStepId | null): SwitchStepId | null {
  if (!next) return current;
  if (!current) return next;
  return stepIndex(next) > stepIndex(current) ? next : current;
}

/**
 * Which steps to draw.
 *
 * `kit` only appears once the engine has actually reported kit work. Most switches move no
 * game settings at all, and a permanently-pending "Moving your game settings" chip on those
 * would describe work that is never going to happen.
 */
export function visibleSteps(sawKit: boolean): SwitchStepId[] {
  return sawKit ? SWITCH_STEPS : SWITCH_STEPS.filter((id) => id !== "kit");
}

export type StepMark = "done" | "active" | "pending" | "skipped";

/**
 * Render marks for the whole strip.
 *
 * `skipped` exists because of a real behaviour rather than tidiness: with `AutoStart` off the
 * lifecycle's `LaunchSteam` returns without launching, so Steam is genuinely not relaunched.
 * The engine reports `Status_StartingPlatform` either way, so the *step* cannot tell them
 * apart — `launched` is passed in from an actual `IsSteamRunning` check after the switch, and
 * a finished switch that did not leave Steam running marks the launch step skipped rather
 * than claiming it happened.
 */
export function stepMarks(
  current: SwitchStepId | null,
  finished: boolean,
  launched = true,
): Record<SwitchStepId, StepMark> {
  const reached = current ? stepIndex(current) : -1;
  const out = {} as Record<SwitchStepId, StepMark>;
  for (const id of SWITCH_STEPS) {
    const i = stepIndex(id);
    if (id === "launch" && finished && !launched) {
      out[id] = "skipped";
    } else if (i < reached) {
      out[id] = "done";
    } else if (i === reached) {
      out[id] = finished ? "done" : "active";
    } else {
      out[id] = finished ? "skipped" : "pending";
    }
  }
  return out;
}

/** 0–100 for the progress bar. A finished switch is always full, however far it got. */
export function stepPercent(current: SwitchStepId | null, finished: boolean): number {
  if (finished) return 100;
  const reached = current ? stepIndex(current) : -1;
  if (reached < 0) return 0;
  // Sits *inside* the active step rather than at its end: a bar that reads 100% while the
  // last step is still running is the classic progress-bar lie.
  return Math.round(((reached + 0.5) / SWITCH_STEPS.length) * 100);
}

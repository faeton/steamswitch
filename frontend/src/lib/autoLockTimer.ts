/**
 * The pure half of idle auto-lock (REDESIGN_BRIEF.md A10).
 *
 * Split out from the store so the arithmetic — which is the part that can be quietly wrong —
 * is testable without timers, a DOM, or a backend.
 */

/** Auto-lock is off when the interval is 0 ("never") or the vault is not lockable. */
export function autoLockEnabled(minutes: number, appPasswordSet: boolean, appLocked: boolean): boolean {
  return minutes > 0 && appPasswordSet && !appLocked;
}

/**
 * Milliseconds left before the app should lock, given when input was last seen.
 *
 * Clamped at 0 rather than allowed to go negative: callers branch on `<= 0` to fire the lock,
 * and a large negative would still read as "fire", but the label would render a nonsense
 * countdown in the window between the deadline and the next tick.
 */
export function remainingMs(lastActivityMs: number, nowMs: number, minutes: number): number {
  if (minutes <= 0) {
    return 0;
  }
  const deadline = lastActivityMs + minutes * 60_000;
  return Math.max(0, deadline - nowMs);
}

/**
 * The `m:ss` countdown shown in the sidebar.
 *
 * Rounds *up*, so the label never shows "0:00" while the app is still unlocked — a lock that
 * has visibly expired but has not happened is exactly the kind of small lie that makes people
 * stop trusting the indicator. Beyond an hour it degrades to whole minutes, because a
 * second-by-second countdown at that range is noise on a control nobody is watching.
 */
export function formatRemaining(ms: number): string {
  if (ms <= 0) {
    return "0:00";
  }
  const totalSeconds = Math.ceil(ms / 1000);
  if (totalSeconds >= 3600) {
    return `${Math.ceil(totalSeconds / 60)}m`;
  }
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

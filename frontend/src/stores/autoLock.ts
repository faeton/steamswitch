/**
 * Idle auto-lock and the vault security preferences behind it (REDESIGN_BRIEF.md A10).
 *
 * The brief asks for auto-lock rules to be *specified*, not implied — so this owns all three:
 * the idle interval, the reveal auto-hide delay, and the clipboard-clear policy. They travel
 * together because they are one promise to the user ("secrets do not linger"), and splitting
 * them across three stores is how one of them ends up unwired.
 *
 * The timer lives here rather than in a component because it has to survive navigation: an
 * auto-lock that resets every time you open Settings is not an auto-lock.
 */
import { derived, get, writable } from "svelte/store";
import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
import { autoLockEnabled, formatRemaining, remainingMs } from "../lib/autoLockTimer";
import { lockApp, securityStatus } from "./security";

export type VaultSecurityPrefs = {
  autoLockMinutes: number;
  revealSeconds: number;
  clearClipboard: boolean;
};

/** Mirrors the Go defaults in `internal/platform/settings.go`. */
export const DEFAULT_PREFS: VaultSecurityPrefs = {
  autoLockMinutes: 5,
  revealSeconds: 8,
  clearClipboard: true,
};

export const vaultSecurityPrefs = writable<VaultSecurityPrefs>(DEFAULT_PREFS);

/** Milliseconds until auto-lock, or 0 when auto-lock is off or already due. */
const remaining = writable(0);

/** `m:ss` for the sidebar, or "" when there is nothing to count down. */
export const autoLockRemainingLabel = derived(remaining, ($ms) =>
  $ms > 0 ? formatRemaining($ms) : "",
);

export async function loadVaultSecurityPrefs(): Promise<VaultSecurityPrefs> {
  try {
    const prefs = await PlatformService.GetVaultSecurityPrefs();
    const next: VaultSecurityPrefs = {
      autoLockMinutes: prefs.autoLockMinutes ?? DEFAULT_PREFS.autoLockMinutes,
      revealSeconds: prefs.revealSeconds ?? DEFAULT_PREFS.revealSeconds,
      clearClipboard: prefs.clearClipboard ?? DEFAULT_PREFS.clearClipboard,
    };
    vaultSecurityPrefs.set(next);
    return next;
  } catch {
    // An older backend or an early-boot read: the defaults are the safe side of every one of
    // these (lock sooner, hide sooner, clear the clipboard), so falling back is not a downgrade.
    vaultSecurityPrefs.set(DEFAULT_PREFS);
    return DEFAULT_PREFS;
  }
}

export async function saveVaultSecurityPrefs(next: VaultSecurityPrefs): Promise<void> {
  await PlatformService.SetVaultSecurityPrefs(next as never);
  // Re-read rather than trusting the value we sent: Go clamps the interval and the reveal
  // delay, and the settings page must show what was actually stored.
  await loadVaultSecurityPrefs();
}

/** How long a revealed secret stays on screen, in ms. */
export function revealMs(): number {
  return get(vaultSecurityPrefs).revealSeconds * 1000;
}

export function clipboardClearEnabled(): boolean {
  return get(vaultSecurityPrefs).clearClipboard;
}

const ACTIVITY_EVENTS = ["pointerdown", "keydown", "wheel", "focus"] as const;
const TICK_MS = 1000;

/**
 * Start watching for idleness. Idempotent; returns a teardown.
 *
 * Deliberately *not* driven by a single long `setTimeout`: the machine can sleep, and a
 * timeout scheduled for five minutes' time may fire five hours later having left the vault
 * unlocked throughout. A one-second tick that re-reads the clock treats a sleep as elapsed
 * idle time, which is the behaviour the brief asks for ("auto-lock … on sleep?").
 */
export function installAutoLock(): () => void {
  let lastActivity = Date.now();
  let locking = false;

  const bump = (): void => {
    lastActivity = Date.now();
  };

  const tick = async (): Promise<void> => {
    const { autoLockMinutes } = get(vaultSecurityPrefs);
    const status = get(securityStatus);
    if (!autoLockEnabled(autoLockMinutes, status.appPasswordSet, status.appLocked)) {
      remaining.set(0);
      // Keep the clock fresh while auto-lock cannot fire, so unlocking does not immediately
      // re-lock on idle time accrued while the app was already locked.
      bump();
      return;
    }

    const left = remainingMs(lastActivity, Date.now(), autoLockMinutes);
    remaining.set(left);
    if (left > 0 || locking) {
      return;
    }

    locking = true;
    try {
      // A refusal (a security operation is mid-flight) is not an error worth surfacing, but
      // it must not be mistaken for success either: only a lock that actually happened
      // restarts the idle clock. Resetting it on a refusal would silently postpone locking by
      // a whole interval every time the timer landed mid-encrypt, which is the opposite of
      // what the setting promises.
      if (await lockApp()) {
        bump();
      }
    } finally {
      locking = false;
    }
  };

  for (const name of ACTIVITY_EVENTS) {
    window.addEventListener(name, bump, true);
  }
  const timer = setInterval(() => void tick(), TICK_MS);
  void tick();

  return () => {
    clearInterval(timer);
    for (const name of ACTIVITY_EVENTS) {
      window.removeEventListener(name, bump, true);
    }
    remaining.set(0);
  };
}

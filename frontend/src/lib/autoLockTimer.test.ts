import { describe, expect, it } from "vitest";
import { autoLockEnabled, formatRemaining, remainingMs } from "./autoLockTimer";

describe("autoLockEnabled", () => {
  it("is off when the interval is 0 — the explicit 'never'", () => {
    expect(autoLockEnabled(0, true, false)).toBe(false);
  });

  it("is off without an app password: there is nothing to lock behind", () => {
    expect(autoLockEnabled(5, false, false)).toBe(false);
  });

  /*
    The third argument is `vaultLocked`, not `appLocked`. The timer locks the *vault* and
    leaves the app running, so an app-locked check would read false for a vault that is
    already sealed and let the timer keep firing against it. `appLocked` implies
    `vaultLocked`, so passing the vault flag still covers the app gate.
  */
  it("is off while the vault is already locked, so it cannot re-fire in a loop", () => {
    expect(autoLockEnabled(5, true, true)).toBe(false);
  });

  it("is on for an unlocked vault with an interval", () => {
    expect(autoLockEnabled(5, true, false)).toBe(true);
  });
});

describe("remainingMs", () => {
  it("counts down from the last activity", () => {
    expect(remainingMs(1_000_000, 1_000_000, 5)).toBe(300_000);
    expect(remainingMs(1_000_000, 1_060_000, 5)).toBe(240_000);
  });

  it("clamps at zero rather than going negative", () => {
    expect(remainingMs(1_000_000, 9_000_000, 5)).toBe(0);
  });

  it("returns 0 when auto-lock is off", () => {
    expect(remainingMs(1_000_000, 1_000_000, 0)).toBe(0);
  });

  /*
    The reason this is a per-tick calculation against the wall clock rather than one long
    setTimeout: a machine that sleeps past the deadline must come back already due.
  */
  it("treats a sleep across the deadline as fully elapsed", () => {
    const lastActivity = 1_000_000;
    const afterFiveHoursAsleep = lastActivity + 5 * 3600 * 1000;
    expect(remainingMs(lastActivity, afterFiveHoursAsleep, 5)).toBe(0);
  });
});

describe("formatRemaining", () => {
  it("renders m:ss with a padded seconds field", () => {
    expect(formatRemaining(272_000)).toBe("4:32");
    expect(formatRemaining(65_000)).toBe("1:05");
  });

  /*
    Rounds up, so an unlocked vault never displays "0:00". A countdown that has visibly
    expired while the app is still open is the kind of small lie that makes the whole
    indicator untrustworthy.
  */
  it("never shows 0:00 while time remains", () => {
    expect(formatRemaining(1)).toBe("0:01");
    expect(formatRemaining(999)).toBe("0:01");
    expect(formatRemaining(0)).toBe("0:00");
  });

  it("degrades to whole minutes beyond an hour", () => {
    expect(formatRemaining(3_600_000)).toBe("60m");
    expect(formatRemaining(4 * 3_600_000)).toBe("240m");
  });
});

/*
  Regression guard for the interaction between the timer and a refused lock.

  `installAutoLock` used to `bump()` in a `finally`, so a lock the backend refused (a security
  operation was mid-flight) reset the idle clock exactly as if it had succeeded — silently
  postponing the lock by a whole interval instead of retrying on the next one-second tick.
  These assertions pin the arithmetic that decision rests on.
*/
describe("a refused lock must not look like a fresh interval", () => {
  it("keeps reporting due once the deadline has passed", () => {
    const lastActivity = 1_000_000;
    const past = lastActivity + 5 * 60_000 + 1;
    expect(remainingMs(lastActivity, past, 5)).toBe(0);
    // One tick later it is still due — nothing about time passing un-dues it.
    expect(remainingMs(lastActivity, past + 1000, 5)).toBe(0);
  });

  it("only a new activity timestamp restarts the interval", () => {
    const now = 2_000_000;
    expect(remainingMs(now - 10 * 60_000, now, 5)).toBe(0);
    expect(remainingMs(now, now, 5)).toBe(300_000);
  });
});

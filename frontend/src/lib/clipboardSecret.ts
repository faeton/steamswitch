/**
 * Copying a secret to the clipboard, and taking it back out again.
 *
 * Extracted from VaultEntryEditor once a second caller appeared (the sign-in assist panel).
 * The first version of that panel re-implemented the copy and left out the scrub, which is
 * the failure this module exists to make impossible: the policy lives in one place, so a new
 * caller gets it by construction rather than by remembering.
 *
 * The scrub is not best-effort decoration. `clearClipboard` defaults on, and on Windows the
 * clipboard feeds Win+V history and cloud sync — a Steam password left there outlives the
 * app, the session and the machine.
 */

/** How long a copied secret is allowed to sit in the clipboard. */
export const CLIPBOARD_CLEAR_SECONDS = 20;

export type ClipboardDeps = {
  /** Whether the user's clipboard-clear policy is on. */
  clearEnabled: () => boolean;
  writeText: (value: string) => Promise<void>;
  readText: () => Promise<string>;
  setTimer: (fn: () => void, ms: number) => unknown;
};

function browserDeps(): ClipboardDeps {
  return {
    clearEnabled: () => true,
    writeText: (v) => navigator.clipboard.writeText(v),
    readText: () => navigator.clipboard.readText(),
    setTimer: (fn, ms) => setTimeout(fn, ms),
  };
}

export type CopyOutcome =
  /** Written, and scheduled to be wiped in CLIPBOARD_CLEAR_SECONDS. */
  | "copied-will-clear"
  /** Written and left there, because the user turned the policy off. */
  | "copied"
  /** The clipboard refused us; the value is still on screen. */
  | "refused";

/**
 * Write a secret to the clipboard and, unless the user has opted out, schedule its removal.
 *
 * Returns what happened so the caller can tell the user which of the two it was — "copied"
 * and "copied, clears in 20s" are different promises and must not be shown interchangeably.
 */
export async function copySecret(
  value: string,
  deps: ClipboardDeps = browserDeps(),
): Promise<CopyOutcome> {
  if (!value) return "refused";
  try {
    await deps.writeText(value);
  } catch {
    return "refused";
  }
  if (!deps.clearEnabled()) return "copied";

  deps.setTimer(() => {
    /*
      Prefer to clear only what we put there — overwriting whatever the user copied since
      would be its own small betrayal. But the app denies clipboard *read* permission
      (`PermissionClipboardRead: PermissionDeny` in gui.go), so on the real build that read
      rejects; when it does, clear anyway. The user turned this setting on, and leaving a
      password on the clipboard indefinitely is the worse failure.
    */
    void deps
      .readText()
      .then((current) => (current === value ? deps.writeText("") : undefined))
      .catch(() => deps.writeText("").catch(() => {}));
  }, CLIPBOARD_CLEAR_SECONDS * 1000);

  return "copied-will-clear";
}

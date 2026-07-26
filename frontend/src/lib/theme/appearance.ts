/**
 * Appearance mode — System / Light / Dark (REDESIGN.md §6).
 *
 * This is an axis *above* the classic theme packs in `catalog.ts`, not a member of them.
 * When no classic pack is active the built-in base in `styles/appearance.scss` provides the
 * palette, keyed on the `data-base-theme` + `data-appearance` attributes this module stamps
 * onto `<html>`. Selecting a classic pack drops `data-base-theme` so the pack's `:root`
 * overlay regains control, while `data-appearance` stays for components that branch on scheme.
 */
import { derived, writable, get } from "svelte/store";

export type AppearanceMode = "system" | "light" | "dark";
export type ResolvedAppearance = "light" | "dark";

export const APPEARANCE_MODE_STORAGE_KEY = "steamswitch:appearance-mode";
export const DEFAULT_APPEARANCE_MODE: AppearanceMode = "system";

/** What the user picked. */
export const appearanceMode = writable<AppearanceMode>(DEFAULT_APPEARANCE_MODE);

/** What the OS currently reports, tracked live. */
export const systemAppearance = writable<ResolvedAppearance>("dark");

/** What is actually painted. */
export const resolvedAppearance = derived(
  [appearanceMode, systemAppearance],
  ([$mode, $system]): ResolvedAppearance => ($mode === "system" ? $system : $mode),
);

export function normalizeAppearanceMode(value: unknown): AppearanceMode {
  const v = String(value ?? "").trim().toLowerCase();
  return v === "light" || v === "dark" || v === "system" ? v : DEFAULT_APPEARANCE_MODE;
}

function darkSchemeQuery(): MediaQueryList | null {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return null;
  }
  return window.matchMedia("(prefers-color-scheme: dark)");
}

let schemeSubscribed = false;

/** Start tracking `prefers-color-scheme`. Idempotent; safe to call before the app mounts. */
export function ensureSystemSchemeSubscription(): void {
  if (schemeSubscribed) {
    return;
  }
  const mq = darkSchemeQuery();
  if (!mq) {
    // No matchMedia (tests, headless): keep the dark default rather than guessing.
    schemeSubscribed = true;
    return;
  }
  schemeSubscribed = true;
  systemAppearance.set(mq.matches ? "dark" : "light");
  const onChange = (ev: MediaQueryListEvent): void => {
    systemAppearance.set(ev.matches ? "dark" : "light");
  };
  if (typeof mq.addEventListener === "function") {
    mq.addEventListener("change", onChange);
  } else if (typeof (mq as unknown as { addListener?: unknown }).addListener === "function") {
    // Safari < 14 / older WebView2.
    (mq as unknown as { addListener: (cb: (e: MediaQueryListEvent) => void) => void }).addListener(onChange);
  }
}

/**
 * Stamp the resolved scheme onto `<html>`.
 *
 * `baseActive` is false while a classic theme pack is loaded, which removes `data-base-theme`
 * and therefore disables every rule in `appearance.scss`.
 */
export function applyAppearanceToDom(resolved: ResolvedAppearance, baseActive: boolean): void {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.documentElement;
  root.setAttribute("data-appearance", resolved);
  if (baseActive) {
    root.setAttribute("data-base-theme", "");
    // Tell the UA which scheme to render native widgets and scrollbars in.
    root.style.colorScheme = resolved;
  } else {
    root.removeAttribute("data-base-theme");
    root.style.removeProperty("color-scheme");
  }
}

export function readStoredAppearanceMode(): AppearanceMode {
  if (typeof localStorage === "undefined") {
    return DEFAULT_APPEARANCE_MODE;
  }
  try {
    return normalizeAppearanceMode(localStorage.getItem(APPEARANCE_MODE_STORAGE_KEY));
  } catch {
    return DEFAULT_APPEARANCE_MODE;
  }
}

export function writeStoredAppearanceMode(mode: AppearanceMode): void {
  if (typeof localStorage === "undefined") {
    return;
  }
  try {
    localStorage.setItem(APPEARANCE_MODE_STORAGE_KEY, mode);
  } catch {
    /* private mode / quota */
  }
}

/** Current resolved scheme without subscribing. */
export function currentResolvedAppearance(): ResolvedAppearance {
  return get(resolvedAppearance);
}

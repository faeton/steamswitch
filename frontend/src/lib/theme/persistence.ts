import { get } from "svelte/store";
import * as PlatformService from "../../../bindings/steamswitch/internal/platform/platformservice.js";
import { offlineMode } from "../../stores/offlineMode";
import { setUserOverride } from "../../stores/backgroundImage";
import { scheduleUpdaterThemeSync } from "../updaterTheme";
import {
  BASE_THEME_ID,
  CUSTOM_THEME_ACCENT_KEY,
  WINDOWS_THEME_ACCENT_KEY,
} from "./types";
import type { ResolvedThemeAccent } from "./types";
import {
  getThemeOptionById,
  isBaseThemeId,
  isKnownThemeId,
  styleLoaderPathForId,
  themeStyles,
} from "./catalog";
import {
  appearanceMode,
  applyAppearanceToDom,
  ensureSystemSchemeSubscription,
  normalizeAppearanceMode,
  readStoredAppearanceMode,
  resolvedAppearance,
  writeStoredAppearanceMode,
  type AppearanceMode,
} from "./appearance";
import { normalizeHexColor } from "./color";
import {
  syncThemeGoogleFonts,
  removeThemeOverlay,
  removeAccentOverlay,
  removeThemeGoogleFontLinks,
  applyAccentOverlay,
  applyResolvedAccent,
  defaultAccentColorFor,
  validateAccentKey,
  clearThemeAccentState,
  ensureWindowsAccentSubscription,
} from "./dom";
import { supportsWindowsThemeAccent } from "./dom";
import { currentThemeId, currentThemeBgUrl, currentThemeAccentKey, currentThemeCustomAccentColor, currentWindowsThemeAccentColor } from "./stores";

const THEME_STORAGE_KEY = "steamswitch:theme";
const THEME_ACCENT_STORAGE_KEY = "steamswitch:theme-accent";
const THEME_ACCENT_CUSTOM_STORAGE_KEY = "steamswitch:theme-accent-custom";

// Pre-rename builds stored these under "tcno:*"; fall back once so users keep their theme.
function getStoredWithLegacy(key: string): string | null {
  return localStorage.getItem(key) ?? localStorage.getItem(key.replace(/^steamswitch:/, "tcno:"));
}

let activeThemeRequestId = 0;

export function resolveThemeAccent(
  themeId: string,
  accentKey = get(currentThemeAccentKey),
  customColor = get(currentThemeCustomAccentColor),
): ResolvedThemeAccent {
  const theme = getThemeOptionById(themeId);
  if (accentKey === CUSTOM_THEME_ACCENT_KEY) {
    return {
      id: CUSTOM_THEME_ACCENT_KEY,
      label: "Custom",
      color: normalizeHexColor(customColor) ?? theme.defaultAccentColor,
      isCustom: true,
    };
  }
  if (accentKey === WINDOWS_THEME_ACCENT_KEY && supportsWindowsThemeAccent()) {
    return {
      id: WINDOWS_THEME_ACCENT_KEY,
      label: "Windows Accent",
      color: get(currentWindowsThemeAccentColor) || theme.defaultAccentColor,
      isCustom: false,
    };
  }
  const explicit = theme.accents.find((option) => option.id === accentKey);
  if (explicit) {
    return { ...explicit, isCustom: false };
  }
  // No explicit pick: the base theme's default depends on the scheme, so ask `dom.ts` rather
  // than reading `defaultAccentColor` — otherwise the Appearance picker would mark Instrument
  // Blue as current while Ledger Green is what is actually painted.
  const color = defaultAccentColorFor(theme);
  const matching = theme.accents.find((option) => option.color === color);
  return matching
    ? { ...matching, isCustom: false }
    : { id: theme.defaultAccentKey, label: "Accent", color, isCustom: false };
}

async function refreshWindowsThemeAccentColor(): Promise<string> {
  if (!supportsWindowsThemeAccent()) {
    currentWindowsThemeAccentColor.set("");
    return "";
  }
  return get(currentWindowsThemeAccentColor);
}

async function loadStoredThemeId(): Promise<string> {
  // An empty persisted value means "no classic pack" — the built-in base (BASE_THEME_ID).
  try {
    return String((await PlatformService.GetTheme()) ?? "").trim();
  } catch {
    return getStoredWithLegacy(THEME_STORAGE_KEY)?.trim() || BASE_THEME_ID;
  }
}

async function loadStoredAppearanceMode(): Promise<AppearanceMode> {
  try {
    return normalizeAppearanceMode(await PlatformService.GetAppearanceMode());
  } catch {
    return readStoredAppearanceMode();
  }
}

async function loadStoredAccentState(): Promise<{ accentKey: string; customColor: string }> {
  try {
    const [accentKey, customColor] = await Promise.all([
      PlatformService.GetThemeAccentPreset(),
      PlatformService.GetThemeAccentCustom(),
    ]);
    return {
      accentKey: String(accentKey ?? "").trim(),
      customColor: String(customColor ?? "").trim(),
    };
  } catch {
    return {
      accentKey: getStoredWithLegacy(THEME_ACCENT_STORAGE_KEY)?.trim() || "",
      customColor: getStoredWithLegacy(THEME_ACCENT_CUSTOM_STORAGE_KEY)?.trim() || "",
    };
  }
}

async function persistAccentState(accentKey: string, customColor: string): Promise<void> {
  try {
    await Promise.all([
      PlatformService.SetThemeAccentPreset(accentKey),
      PlatformService.SetThemeAccentCustom(customColor),
    ]);
  } catch {
    /* offline / early boot */
  }

  if (accentKey) {
    localStorage.setItem(THEME_ACCENT_STORAGE_KEY, accentKey);
  } else {
    localStorage.removeItem(THEME_ACCENT_STORAGE_KEY);
  }

  if (customColor) {
    localStorage.setItem(THEME_ACCENT_CUSTOM_STORAGE_KEY, customColor);
  } else {
    localStorage.removeItem(THEME_ACCENT_CUSTOM_STORAGE_KEY);
  }
}

/** Re-stamp `<html>` for the current scheme + whether a classic pack is loaded. */
function syncAppearanceDom(): void {
  applyAppearanceToDom(get(resolvedAppearance), isBaseThemeId(get(currentThemeId)));
}

function applyBaseTheme(): void {
  currentThemeId.set(BASE_THEME_ID);
  currentThemeBgUrl.set("");
  syncThemeGoogleFonts(BASE_THEME_ID);
  syncAppearanceDom();
  scheduleUpdaterThemeSync();
}

async function applyTheme(id: string): Promise<void> {
  const requestId = ++activeThemeRequestId;
  removeThemeOverlay();
  removeAccentOverlay();
  removeThemeGoogleFontLinks();

  if (isBaseThemeId(id)) {
    applyBaseTheme();
    return;
  }

  const key = styleLoaderPathForId(id);
  if (!key) {
    console.warn("[themes] Unknown or missing style for theme:", id);
    applyBaseTheme();
    return;
  }

  const load = themeStyles[key];
  const css = await load();
  if (requestId !== activeThemeRequestId) {
    return;
  }

  removeThemeOverlay();
  const style = document.createElement("style");
  style.id = "steamswitch-theme-overlay";
  style.setAttribute("data-steamswitch-theme-overlay", "");
  style.textContent = css;
  document.head.appendChild(style);
  currentThemeId.set(id);
  currentThemeBgUrl.set(getThemeOptionById(id).backgroundUrl ?? "");
  syncThemeGoogleFonts(id);
  // Drops `data-base-theme`, which is what lets the pack's `:root` overlay win. Leaving it
  // set would keep the higher-specificity rules in `appearance.scss` on top of the pack.
  syncAppearanceDom();
  scheduleUpdaterThemeSync();
}

export async function initTheme(): Promise<void> {
  let [id, storedAccent, mode] = await Promise.all([
    loadStoredThemeId(),
    loadStoredAccentState(),
    loadStoredAppearanceMode(),
  ]);
  if (!isKnownThemeId(id)) {
    id = BASE_THEME_ID;
  }

  appearanceMode.set(mode);
  ensureSystemSchemeSubscription();

  ensureWindowsAccentSubscription();
  await refreshWindowsThemeAccentColor();
  await applyTheme(id);
  applyResolvedAccent(id, storedAccent.accentKey, storedAccent.customColor);

  /*
    From here on, an OS scheme flip (or a mode change) re-stamps the attributes *and*
    re-resolves the accent. The second half matters because the base theme's default accent
    is per-scheme (Instrument Blue / Ledger Green): without it, switching to Light would paint
    Ledger's paper surfaces but keep Instrument's blue.

    Subscribed after the initial apply on purpose — Svelte stores emit immediately, and doing
    this before `applyTheme` would run the accent path against half-initialised state. A user
    who *has* picked an accent keeps it: `applyResolvedAccent` only falls through to the
    scheme default when the stored key is empty.
  */
  resolvedAppearance.subscribe(() => {
    syncAppearanceDom();
    applyResolvedAccent(
      get(currentThemeId),
      get(currentThemeAccentKey),
      get(currentThemeCustomAccentColor),
    );
  });
}

/** Switch between System / Light / Dark. No-op for the palette while a classic pack is active. */
export async function setUserAppearanceMode(mode: AppearanceMode): Promise<void> {
  const next = normalizeAppearanceMode(mode);
  appearanceMode.set(next);
  writeStoredAppearanceMode(next);
  try {
    await PlatformService.SetAppearanceMode(next);
  } catch {
    /* older backend / early boot: localStorage still carries it */
  }
  syncAppearanceDom();
}

export async function setUserTheme(id: string): Promise<void> {
  const next = isKnownThemeId(id) ? id : BASE_THEME_ID;
  const previous = get(currentThemeId);

  try {
    await PlatformService.SetTheme(next);
  } catch {
    /* offline / early boot */
  }

  localStorage.setItem(THEME_STORAGE_KEY, next);

  if (next === previous) {
    return;
  }

  await persistAccentState("", "");
  clearThemeAccentState();
  await applyTheme(next);
  await setUserOverride(false);
}

/** Leave the classic-theme section and return to the built-in base. */
export async function clearUserTheme(): Promise<void> {
  await setUserTheme(BASE_THEME_ID);
}

export async function setUserThemeAccentPreset(accentKey: string): Promise<void> {
  const theme = getThemeOptionById(get(currentThemeId));
  const validAccentKey = validateAccentKey(theme, accentKey);
  const customColor = normalizeHexColor(get(currentThemeCustomAccentColor)) ?? "";

  if (validAccentKey === WINDOWS_THEME_ACCENT_KEY) {
    await refreshWindowsThemeAccentColor();
  }

  if (!validAccentKey || validAccentKey === theme.defaultAccentKey) {
    await persistAccentState("", customColor);
    applyResolvedAccent(theme.id, "", customColor);
    return;
  }

  await persistAccentState(validAccentKey, customColor);
  applyResolvedAccent(theme.id, validAccentKey, customColor);
}

export async function setUserThemeAccentCustom(color: string): Promise<void> {
  const theme = getThemeOptionById(get(currentThemeId));
  const normalized =
    normalizeHexColor(color) ?? resolveThemeAccent(theme.id).color ?? theme.defaultAccentColor;
  await persistAccentState(CUSTOM_THEME_ACCENT_KEY, normalized);
  applyResolvedAccent(theme.id, CUSTOM_THEME_ACCENT_KEY, normalized);
}

offlineMode.subscribe(() => {
  syncThemeGoogleFonts(get(currentThemeId));
});

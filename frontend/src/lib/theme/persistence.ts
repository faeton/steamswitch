import { get } from "svelte/store";
import * as PlatformService from "../../../bindings/steamswitch/internal/platform/platformservice.js";
import {
  BASE_THEME_ID,
  CUSTOM_THEME_ACCENT_KEY,
  WINDOWS_THEME_ACCENT_KEY,
} from "./types";
import type { ResolvedThemeAccent } from "./types";
import { getThemeOptionById } from "./catalog";
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
  applyResolvedAccent,
  defaultAccentColorFor,
  validateAccentKey,
  ensureWindowsAccentSubscription,
} from "./dom";
import { supportsWindowsThemeAccent } from "./dom";
import { currentThemeId, currentThemeAccentKey, currentThemeCustomAccentColor, currentWindowsThemeAccentColor } from "./stores";

const THEME_ACCENT_STORAGE_KEY = "steamswitch:theme-accent";
const THEME_ACCENT_CUSTOM_STORAGE_KEY = "steamswitch:theme-accent-custom";

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
      accentKey: localStorage.getItem(THEME_ACCENT_STORAGE_KEY)?.trim() || "",
      customColor: localStorage.getItem(THEME_ACCENT_CUSTOM_STORAGE_KEY)?.trim() || "",
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

function syncAppearanceDom(): void {
  applyAppearanceToDom(get(resolvedAppearance), true);
}

export async function initTheme(): Promise<void> {
  const [storedAccent, mode] = await Promise.all([loadStoredAccentState(), loadStoredAppearanceMode()]);

  appearanceMode.set(mode);
  ensureSystemSchemeSubscription();

  ensureWindowsAccentSubscription();
  await refreshWindowsThemeAccentColor();
  currentThemeId.set(BASE_THEME_ID);
  syncAppearanceDom();
  applyResolvedAccent(BASE_THEME_ID, storedAccent.accentKey, storedAccent.customColor);

  /*
    From here on, an OS scheme flip (or a mode change) re-stamps the attributes *and*
    re-resolves the accent. The second half matters because the base theme's default accent
    is per-scheme (Instrument Blue / Ledger Green): without it, switching to Light would paint
    Ledger's paper surfaces but keep Instrument's blue.

    Subscribed after the initial apply on purpose — Svelte stores emit immediately, and doing
    this before the first apply would run the accent path against half-initialised state. A user
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

/** Switch between System / Light / Dark. */
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

export async function setUserThemeAccentPreset(accentKey: string): Promise<void> {
  const theme = getThemeOptionById(get(currentThemeId));
  const validAccentKey = validateAccentKey(theme, accentKey);
  const customColor = normalizeHexColor(get(currentThemeCustomAccentColor)) ?? "";

  if (validAccentKey === WINDOWS_THEME_ACCENT_KEY) {
    await refreshWindowsThemeAccentColor();
  }

  // "The default" means what this scheme would paint with nothing chosen, not the theme's fixed
  // default key: the base theme's default differs by scheme, so comparing keys made Instrument
  // Blue impossible to pick in light mode — the click cleared the choice back to Ledger Green.
  const preset = theme.accents.find((option) => option.id === validAccentKey);
  const isSchemeDefault = preset?.color === defaultAccentColorFor(theme);
  if (!validAccentKey || isSchemeDefault) {
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

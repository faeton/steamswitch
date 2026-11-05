import { Events } from "@wailsio/runtime";
import { get } from "svelte/store";
import modalPrimaryCss from "../../styles/modal-primary.scss?inline";
import {
  BASE_DEFAULT_ACCENT_BY_SCHEME,
  WINDOWS_THEME_ACCENT_KEY,
  CUSTOM_THEME_ACCENT_KEY,
} from "./types";
import type { ThemeOption } from "./types";
import { getThemeOptionById } from "./catalog";
import { currentResolvedAppearance } from "./appearance";
import { normalizeHexColor, buildAccentOverlayCss } from "./color";
import {
  currentWindowsThemeAccentColor,
  currentThemeAccentKey,
  currentThemeId,
  currentThemeCustomAccentColor,
} from "./stores";

const ACCENT_OVERLAY_STYLE_ID = "steamswitch-theme-accent-overlay";
const ACCENT_OVERLAY_STYLE_ATTR = "data-steamswitch-theme-accent-overlay";
const MODAL_PRIMARY_STYLE_ID = "steamswitch-modal-primary-overlay";
const MODAL_PRIMARY_STYLE_ATTR = "data-steamswitch-modal-primary-overlay";

const WINDOWS_ACCENT_CHANGED_EVENT = "windows-accent-changed";

let windowsAccentSubscribed = false;

export function supportsWindowsThemeAccent(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  return /windows/i.test(navigator.userAgent) || /win32/i.test(navigator.userAgent);
}

export function removeAccentOverlay(): void {
  if (typeof document === "undefined") {
    return;
  }
  document.querySelectorAll(`style[${ACCENT_OVERLAY_STYLE_ATTR}]`).forEach((node) => node.remove());
}

export function applyAccentOverlay(color: string | null): void {
  removeAccentOverlay();
  if (typeof document === "undefined" || !color) {
    syncModalPrimaryStyles();
    return;
  }
  const style = document.createElement("style");
  style.id = ACCENT_OVERLAY_STYLE_ID;
  style.setAttribute(ACCENT_OVERLAY_STYLE_ATTR, "");
  style.textContent = buildAccentOverlayCss(color);
  document.head.appendChild(style);
  syncModalPrimaryStyles();
}

function syncModalPrimaryStyles(): void {
  if (typeof document === "undefined") {
    return;
  }
  let style = document.getElementById(MODAL_PRIMARY_STYLE_ID) as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement("style");
    style.id = MODAL_PRIMARY_STYLE_ID;
    style.setAttribute(MODAL_PRIMARY_STYLE_ATTR, "");
    style.textContent = modalPrimaryCss;
  }
  document.head.appendChild(style);
}

export function ensureWindowsAccentSubscription(): void {
  if (!supportsWindowsThemeAccent() || windowsAccentSubscribed) {
    return;
  }
  windowsAccentSubscribed = true;
  Events.On(WINDOWS_ACCENT_CHANGED_EVENT, (event) => {
    const color = normalizeHexColor(event.data) ?? "";
    currentWindowsThemeAccentColor.set(color);
    if (get(currentThemeAccentKey) === WINDOWS_THEME_ACCENT_KEY) {
      applyResolvedAccent(get(currentThemeId), WINDOWS_THEME_ACCENT_KEY, get(currentThemeCustomAccentColor));
    }
  });
}

export function validateAccentKey(theme: ThemeOption, key: string): string {
  const trimmed = key.trim();
  if (!trimmed) {
    return "";
  }
  if (trimmed === CUSTOM_THEME_ACCENT_KEY) {
    return CUSTOM_THEME_ACCENT_KEY;
  }
  if (trimmed === WINDOWS_THEME_ACCENT_KEY && supportsWindowsThemeAccent()) {
    return WINDOWS_THEME_ACCENT_KEY;
  }
  return theme.accents.some((option) => option.id === trimmed) ? trimmed : "";
}

/**
 * The colour "no accent chosen" resolves to.
 *
 * For the built-in base that depends on the scheme, because the two schemes are two visual
 * languages (see `BASE_DEFAULT_ACCENT_BY_SCHEME`). Classic packs carry one fixed accent in
 * their own stylesheet, so theirs is scheme-independent.
 */
export function defaultAccentColorFor(theme: ThemeOption): string {
  return BASE_DEFAULT_ACCENT_BY_SCHEME[currentResolvedAppearance()] ?? theme.defaultAccentColor;
}

export function applyResolvedAccent(themeId: string, accentKey: string, customColor: string): void {
  const theme = getThemeOptionById(themeId);
  const validCustomColor = normalizeHexColor(customColor) ?? "";
  const validAccentKey = validateAccentKey(theme, accentKey);

  currentThemeCustomAccentColor.set(validCustomColor);

  if (validAccentKey === CUSTOM_THEME_ACCENT_KEY && validCustomColor) {
    currentThemeAccentKey.set(CUSTOM_THEME_ACCENT_KEY);
    applyAccentOverlay(validCustomColor);
    return;
  }

  if (validAccentKey === WINDOWS_THEME_ACCENT_KEY) {
    currentThemeAccentKey.set(WINDOWS_THEME_ACCENT_KEY);
    applyAccentOverlay(get(currentWindowsThemeAccentColor) || defaultAccentColorFor(theme));
    return;
  }

  // Every preset can be an explicit choice, including the nominal default: that key is only the
  // default in one of the two schemes.
  if (validAccentKey) {
    const preset = theme.accents.find((option) => option.id === validAccentKey);
    if (preset) {
      currentThemeAccentKey.set(validAccentKey);
      applyAccentOverlay(preset.color);
      return;
    }
  }

  currentThemeAccentKey.set("");
  // `appearance.scss` omits accent tokens on purpose (its attribute selectors would out-rank this
  // overlay), so the default has to be written out, and it depends on the current scheme.
  applyAccentOverlay(defaultAccentColorFor(theme));
}


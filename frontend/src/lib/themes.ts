export { WINDOWS_THEME_ACCENT_KEY, BASE_THEME_ID } from "./theme/types";
export type { ThemeAccentOption, ResolvedThemeAccent, ThemeOption } from "./theme/types";
export { getThemeOptionById } from "./theme/catalog";
export { supportsWindowsThemeAccent } from "./theme/dom";
export {
  currentThemeId,
  currentThemeAccentKey,
  currentThemeCustomAccentColor,
  currentWindowsThemeAccentColor,
} from "./theme/stores";
export {
  appearanceMode,
  resolvedAppearance,
  systemAppearance,
  normalizeAppearanceMode,
} from "./theme/appearance";
export type { AppearanceMode, ResolvedAppearance } from "./theme/appearance";
export {
  resolveThemeAccent,
  initTheme,
  setUserAppearanceMode,
  setUserThemeAccentPreset,
  setUserThemeAccentCustom,
} from "./theme/persistence";

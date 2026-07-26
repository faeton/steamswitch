export const DEFAULT_THEME_ID = "default";
export const CUSTOM_THEME_ACCENT_KEY = "custom";
export const WINDOWS_THEME_ACCENT_KEY = "windows";

/**
 * Sentinel for "no classic theme pack" — the product default (REDESIGN.md §6). The palette
 * then comes from `styles/appearance.scss`, driven by the System/Light/Dark axis in
 * `lib/theme/appearance.ts`. Persisted as the empty string, which is also what pre-redesign
 * builds wrote for "user never picked a theme", so existing installs land here.
 */
export const BASE_THEME_ID = "";

export type ThemeAccentOption = {
  id: string;
  label: string;
  color: string;
};

export type ResolvedThemeAccent = ThemeAccentOption & {
  isCustom: boolean;
};

export type ThemeOption = {
  id: string;
  label: string;
  googleFontsCss: string | null;
  backgroundUrl: string | null;
  defaultAccentColor: string;
  defaultAccentKey: string;
  accents: ThemeAccentOption[];
  /** True for the built-in base; classic packs are grouped separately in Appearance. */
  isBase?: boolean;
};

/** The built-in base. Its colours live in SCSS; only the accent list is data. */
export const BASE_THEME_OPTION: ThemeOption = {
  id: BASE_THEME_ID,
  label: "Default",
  googleFontsCss: null,
  backgroundUrl: null,
  defaultAccentColor: "#1a9fff",
  defaultAccentKey: "steam",
  accents: [
    { id: "steam", label: "Steam Blue", color: "#1a9fff" },
    { id: "mint", label: "Mint", color: "#2dd4bf" },
    { id: "violet", label: "Violet", color: "#8b5cf6" },
    { id: "amber", label: "Amber", color: "#f59e0b" },
    { id: "rose", label: "Rose", color: "#fb7185" },
  ],
  isBase: true,
};

export const DEFAULT_THEME_OPTION: ThemeOption = {
  id: DEFAULT_THEME_ID,
  label: "Dracula Cyan",
  googleFontsCss: null,
  backgroundUrl: null,
  defaultAccentColor: "#80ffea",
  defaultAccentKey: "cyan",
  accents: [
    { id: "cyan", label: "Cyan", color: "#80ffea" },
    { id: "green", label: "Green", color: "#8aff80" },
    { id: "orange", label: "Orange", color: "#ffca80" },
    { id: "pink", label: "Pink", color: "#ff80bf" },
    { id: "purple", label: "Purple", color: "#9580ff" },
    { id: "red", label: "Red", color: "#ff9580" },
    { id: "yellow", label: "Yellow", color: "#ffff80" },
  ],
};

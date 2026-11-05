export const CUSTOM_THEME_ACCENT_KEY = "custom";
export const WINDOWS_THEME_ACCENT_KEY = "windows";

/** The built-in theme. Its palette comes from `styles/appearance.scss`, per `lib/theme/appearance.ts`. */
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
  defaultAccentColor: string;
  defaultAccentKey: string;
  accents: ThemeAccentOption[];
  isBase?: boolean;
};

export const BASE_ACCENT_INSTRUMENT = "#3f9bf5";
export const BASE_ACCENT_LEDGER = "#2f6b52";

/**
 * The base theme's default accent, per scheme.
 *
 * The two schemes are two visual languages, not one palette tinted twice: Instrument (dark)
 * is built around a cool blue, Ledger (light) around a deep green. So "the user has not
 * picked an accent" resolves differently in each, and flipping Light/Dark has to re-resolve
 * it — see `applyResolvedAccent`. A single default would put Instrument's blue on Ledger's
 * paper, where it is both off-language and short of contrast.
 *
 * Keyed by the plain scheme strings rather than importing `ResolvedAppearance`, so this
 * module stays dependency-free.
 */
export const BASE_DEFAULT_ACCENT_BY_SCHEME: Record<"light" | "dark", string> = {
  dark: BASE_ACCENT_INSTRUMENT,
  light: BASE_ACCENT_LEDGER,
};

/** The built-in base. Its colours live in SCSS; only the accent list is data. */
export const BASE_THEME_OPTION: ThemeOption = {
  id: BASE_THEME_ID,
  label: "Default",
  defaultAccentColor: BASE_ACCENT_INSTRUMENT,
  defaultAccentKey: "instrument",
  accents: [
    { id: "instrument", label: "Instrument Blue", color: BASE_ACCENT_INSTRUMENT },
    { id: "ledger", label: "Ledger Green", color: BASE_ACCENT_LEDGER },
    { id: "steam", label: "Steam Blue", color: "#1a9fff" },
    { id: "mint", label: "Mint", color: "#2dd4bf" },
    { id: "violet", label: "Violet", color: "#8b5cf6" },
    { id: "amber", label: "Amber", color: "#f59e0b" },
    { id: "rose", label: "Rose", color: "#fb7185" },
  ],
  isBase: true,
};

import { BASE_THEME_OPTION } from "./types";
import type { ThemeOption } from "./types";

/** The app has one theme. Light or dark and the accent are choices within it. */
export function getThemeOptionById(_id: string): ThemeOption {
  return BASE_THEME_OPTION;
}

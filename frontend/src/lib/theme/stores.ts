import { writable } from "svelte/store";
import { BASE_THEME_ID } from "./types";

export const currentThemeId = writable<string>(BASE_THEME_ID);
export const currentThemeAccentKey = writable<string>("");
export const currentThemeCustomAccentColor = writable<string>("");
export const currentWindowsThemeAccentColor = writable<string>("");

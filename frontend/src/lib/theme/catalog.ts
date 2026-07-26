import { parse as parseYaml } from "yaml";
import { BASE_THEME_ID, BASE_THEME_OPTION, DEFAULT_THEME_OPTION } from "./types";
import type { ThemeOption, ThemeAccentOption } from "./types";
import { normalizeHexColor } from "./color";

const themeInfos = import.meta.glob("../../styles/themes/*/info.yaml", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

export const themeStyles = import.meta.glob("../../styles/themes/*/style.scss", {
  query: "?inline",
  import: "default",
}) as Record<string, () => Promise<string>>;

const themeBackgrounds = import.meta.glob(
  "../../styles/themes/*/*.{jpg,jpeg,png,webp,gif}",
  { eager: true, import: "default" }
) as Record<string, string>;

type ThemeInfoYaml = {
  name?: unknown;
  accent?: unknown;
  accents?: unknown;
  googleFontsCss?: unknown;
  background?: unknown;
};

function normPath(p: string): string {
  return p.replace(/\\/g, "/");
}

function folderFromInfoPath(path: string): string {
  const m = normPath(path).match(/themes\/([^/]+)\/info\.yaml$/i);
  return m?.[1] ?? "";
}

export function styleLoaderPathForId(themeId: string): string | null {
  const suffix = `themes/${themeId}/style.scss`;
  for (const k of Object.keys(themeStyles)) {
    if (normPath(k).endsWith(suffix)) {
      return k;
    }
  }
  return null;
}

function sanitizeThemeBackgroundFilename(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (!/^[a-zA-Z0-9_-]+\.(jpe?g|png|webp|gif)$/i.test(trimmed)) return null;
  return trimmed;
}

function findThemeBackgroundUrl(themeId: string, filename: string): string | null {
  const suffix = normPath(`themes/${themeId}/${filename}`);
  for (const [path, url] of Object.entries(themeBackgrounds)) {
    if (normPath(path).endsWith(suffix)) {
      return url;
    }
  }
  return null;
}

function sanitizeGoogleFontsCssUrl(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  try {
    const url = new URL(trimmed);
    if (url.protocol !== "https:") {
      return null;
    }
    if (!/^fonts\.googleapis\.com$/i.test(url.hostname)) {
      return null;
    }
    return url.href;
  } catch {
    return null;
  }
}

function accentKeyFromLabel(label: string): string {
  return (
    label
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "accent"
  );
}

function dedupKey(baseKey: string, seen: Set<string>): string {
  let key = baseKey;
  let suffix = 2;
  while (seen.has(key)) {
    key = `${baseKey}-${suffix++}`;
  }
  return key;
}

function parseAccentOptions(value: unknown, fallbackColor: string): ThemeAccentOption[] {
  const fallback = () => [{ id: "accent", label: "Accent", color: fallbackColor }];
  if (!Array.isArray(value)) return fallback();

  const options: ThemeAccentOption[] = [];
  const seen = new Set<string>();
  for (const entry of value) {
    if (!entry || typeof entry !== "object") continue;
    const record = entry as Record<string, unknown>;
    const label = typeof record.name === "string" ? record.name.trim() : "";
    const color = normalizeHexColor(record.color);
    if (!label || !color) continue;

    const key = dedupKey(accentKeyFromLabel(label), seen);
    seen.add(key);
    options.push({ id: key, label, color });
  }
  return options.length ? options : fallback();
}

function parseThemeInfo(raw: string, id: string): ThemeOption | null {
  let parsed: ThemeInfoYaml;
  try {
    const next = parseYaml(raw);
    if (!next || typeof next !== "object") {
      return null;
    }
    parsed = next as ThemeInfoYaml;
  } catch {
    return null;
  }

  const label = typeof parsed.name === "string" && parsed.name.trim() ? parsed.name.trim() : id;
  const accent = normalizeHexColor(parsed.accent) ?? DEFAULT_THEME_OPTION.defaultAccentColor;
  let accents = parseAccentOptions(parsed.accents, accent);
  let defaultAccentKey = accents.find((option) => option.color === accent)?.id ?? "";
  if (!defaultAccentKey) {
    accents = [{ id: "accent", label: "Accent", color: accent }, ...accents];
    defaultAccentKey = "accent";
  }

  return {
    id,
    label,
    googleFontsCss: sanitizeGoogleFontsCssUrl(parsed.googleFontsCss),
    backgroundUrl: (() => {
      const fn = sanitizeThemeBackgroundFilename(parsed.background);
      return fn ? findThemeBackgroundUrl(id, fn) : null;
    })(),
    defaultAccentColor: accent,
    defaultAccentKey,
    accents,
  };
}

function buildClassicCatalog(): ThemeOption[] {
  const rest: ThemeOption[] = [];
  for (const [path, raw] of Object.entries(themeInfos)) {
    const id = folderFromInfoPath(path);
    if (!id || !styleLoaderPathForId(id)) {
      continue;
    }
    const theme = parseThemeInfo(raw, id);
    if (theme) {
      rest.push(theme);
    }
  }
  rest.sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }));
  // Dracula Cyan was the pre-redesign default; it is now just the first classic pack.
  return [DEFAULT_THEME_OPTION, ...rest];
}

const CLASSIC_THEMES = buildClassicCatalog();
const THEMES = [BASE_THEME_OPTION, ...CLASSIC_THEMES];
const THEMES_BY_ID = new Map(THEMES.map((theme) => [theme.id, theme] as const));

/** Every selectable theme, base first. */
export function listThemes(): ThemeOption[] {
  return THEMES;
}

/** Only the inherited theme packs — the collapsed "Classic themes" section (REDESIGN.md §6). */
export function listClassicThemes(): ThemeOption[] {
  return CLASSIC_THEMES;
}

export function getThemeOptionById(id: string): ThemeOption {
  return THEMES_BY_ID.get(id) ?? BASE_THEME_OPTION;
}

export function isKnownThemeId(id: string): boolean {
  return THEMES_BY_ID.has(id);
}

/** True when `id` selects the built-in base rather than a classic pack. */
export function isBaseThemeId(id: string): boolean {
  return (id ?? "").trim() === BASE_THEME_ID;
}

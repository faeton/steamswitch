import type { PlatformStartup } from "../../bindings/steamswitch/internal/platform/models.js";

/**
 * Routes after the redesign (REDESIGN_BRIEF.md A6 "App chrome & global nav").
 *
 * `home` is the Steam account list itself, not a platform grid — this fork ships one platform,
 * so a chooser in front of it was pure ceremony. The brief fixes the top-level destinations as
 * a small closed set, which is what the sidebar renders: **Switcher / Vault / Tools / Settings
 * / About**. Everything else is a sub-route reached from one of those.
 *
 * `settings` carries its category in the route so a settings page is linkable and the back
 * button walks categories rather than jumping out of Settings entirely (brief A11).
 *
 * `platform` and `platform-settings` survive only as aliases: the tray, CLI and
 * `steamswitch://` URLs of older builds still emit them, and silently landing the user on a
 * dead route would be worse than redirecting.
 */
export const SETTINGS_CATEGORIES = [
  "appearance",
  "steam",
  "game-modules",
  "vault",
  "system",
  "language",
] as const;

export type SettingsCategory = (typeof SETTINGS_CATEGORIES)[number];

export const DEFAULT_SETTINGS_CATEGORY: SettingsCategory = "appearance";

export function isSettingsCategory(value: string): value is SettingsCategory {
  return (SETTINGS_CATEGORIES as readonly string[]).includes(value);
}

export type Route =
  | { page: "home" }
  | { page: "vault" }
  | { page: "tools" }
  | { page: "settings"; category: SettingsCategory }
  | { page: "about" }
  | { page: "preview-css" }
  | { page: "steam-advanced-clearing" }
  | { page: "dota-configs" };

/** The five sidebar destinations, in the order the brief lists them. */
export const TOP_LEVEL_PAGES = ["home", "vault", "tools", "settings", "about"] as const;

export type TopLevelPage = (typeof TOP_LEVEL_PAGES)[number];

export function serializeRoute(r: Route): string {
  switch (r.page) {
    case "home":
      return "#/";
    case "vault":
      return "#/vault";
    case "settings":
      return `#/settings/${r.category}`;
    case "about":
      return "#/about";
    case "tools":
      return "#/tools";
    case "preview-css":
      return "#/preview-css";
    case "steam-advanced-clearing":
      return "#/tools/advanced-clearing";
    case "dota-configs":
      return "#/tools/dota-configs";
    default:
      return "#/";
  }
}

type RouteParser = (parts: string[]) => Route | null;

const TOOLS_SUBROUTES: Record<string, Route> = {
  "advanced-clearing": { page: "steam-advanced-clearing" },
  "dota-configs": { page: "dota-configs" },
};

/**
 * `#/settings` with no category is the Appearance page, not an error: older links, the tray
 * and `steamswitch://…/settings` all emit the bare form, and the category is a view detail.
 * An unknown category *is* rejected, so a typo falls back through `parseHash`'s null path
 * rather than silently landing somewhere arbitrary.
 */
function parseSettings(parts: string[]): Route | null {
  const raw = decodeURIComponent(parts[1] ?? "").toLowerCase();
  if (!raw) {
    return { page: "settings", category: DEFAULT_SETTINGS_CATEGORY };
  }
  return isSettingsCategory(raw) ? { page: "settings", category: raw } : null;
}

const ROUTE_PARSERS: Record<string, RouteParser> = {
  "": () => ({ page: "home" }),
  home: () => ({ page: "home" }),
  vault: () => ({ page: "vault" }),
  about: () => ({ page: "about" }),
  settings: parseSettings,
  "preview-css": () => ({ page: "preview-css" }),
  test: () => ({ page: "preview-css" }),
  tools: (p) => (p[1] ? (TOOLS_SUBROUTES[p[1].toLowerCase()] ?? null) : { page: "tools" }),

  // --- Aliases kept for links minted by older builds -------------------------------
  platform: () => ({ page: "home" }),
  "platform-settings": () => ({ page: "settings", category: DEFAULT_SETTINGS_CATEGORY }),
  steam: (p) =>
    p[1]?.toLowerCase() === "advanced-clearing" ? { page: "steam-advanced-clearing" } : null,
  "dota-configs": () => ({ page: "dota-configs" }),
};

export function parseHash(hash: string): Route | null {
  let h = (hash.startsWith("#") ? hash.slice(1) : hash).trim();
  if (!h || h === "/") return { page: "home" };

  const parts = h.split("/").filter((p) => p.length > 0);
  const head = decodeURIComponent(parts[0] || "").toLowerCase();

  const parser = ROUTE_PARSERS[head];
  return parser ? parser(parts) : null;
}

export function validateRoute(r: Route, startup: PlatformStartup): Route {
  if (startup.platformsFileMissing) return { page: "home" };

  const disabled = new Set(startup.disabledPlatformNames || []);
  const steamAvailable =
    (startup.allPlatformNames || []).includes("Steam") && !disabled.has("Steam");

  switch (r.page) {
    // All Steam tools; without Steam there is nothing for them to operate on. The vault keys
    // its entries by SteamID64, so it is in the same boat.
    case "steam-advanced-clearing":
    case "dota-configs":
    case "vault":
      return steamAvailable ? r : { page: "home" };
    default:
      return r;
  }
}

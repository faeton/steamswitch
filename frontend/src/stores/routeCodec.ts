import type { PlatformStartup } from "../../bindings/steamswitch/internal/platform/models.js";

/**
 * Routes after the redesign (REDESIGN.md §§3–4).
 *
 * `home` is now the Steam account list itself, not a platform grid — this fork ships one
 * platform, so a chooser in front of it was pure ceremony. `tools` is the hub for everything
 * demoted off the main path.
 *
 * `platform` and `platform-settings` survive only as aliases: the tray, CLI and
 * `steamswitch://` URLs of older builds still emit them, and silently landing the user on a
 * dead route would be worse than redirecting.
 */
export type Route =
  | { page: "home" }
  | { page: "settings" }
  | { page: "tools" }
  | { page: "preview-css" }
  | { page: "steam-advanced-clearing" }
  | { page: "dota-configs" };

export function serializeRoute(r: Route): string {
  switch (r.page) {
    case "home":
      return "#/";
    case "settings":
      return "#/settings";
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

const ROUTE_PARSERS: Record<string, RouteParser> = {
  "": () => ({ page: "home" }),
  home: () => ({ page: "home" }),
  settings: () => ({ page: "settings" }),
  "preview-css": () => ({ page: "preview-css" }),
  test: () => ({ page: "preview-css" }),
  tools: (p) => (p[1] ? (TOOLS_SUBROUTES[p[1].toLowerCase()] ?? null) : { page: "tools" }),

  // --- Aliases kept for links minted by older builds -------------------------------
  platform: () => ({ page: "home" }),
  "platform-settings": () => ({ page: "settings" }),
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
    // Both are Steam tools; without Steam there is nothing for them to operate on.
    case "steam-advanced-clearing":
    case "dota-configs":
      return steamAvailable ? r : { page: "home" };
    default:
      return r;
  }
}

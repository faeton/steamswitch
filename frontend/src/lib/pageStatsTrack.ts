import { get } from "svelte/store";
import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
import { route } from "../stores/nav";
import type { Route } from "../stores/routeCodec";

/** Stable path key for Statistics.json PageStats (matches server normPageKey). */
function routeToStatsPath(r: Route): string {
  switch (r.page) {
    case "home":
      return "/";
    // Settings categories collapse to one key: the stat is "how long in Settings", and
    // six sub-paths would fragment it without answering a question anyone asks.
    case "settings":
      return "/settings";
    case "vault":
      return "/vault";
    case "about":
      return "/about";
    case "preview-css":
      return "/preview-css";
    case "dota-configs":
      return "/dota-configs";
    case "tools":
      return "/tools";
    case "steam-advanced-clearing":
      return "/steam/advanced-clearing";
    default:
      return "/";
  }
}

const tickMs = 60_000;

/**
 * Records SPA page visits and dwell time via the platform service (Statistics.json).
 * Call once after the route store is initialised.
 */
export function installPageStatsTracking(): () => void {
  let path = "";
  let started = 0;

  const flushSeconds = (usePath: string, minSeconds: number): void => {
    const elapsed = Math.max(0, Math.floor((Date.now() - started) / 1000));
    if (usePath && elapsed >= minSeconds) {
      void PlatformService.StatsAddPageTime(usePath, elapsed);
      started = Date.now();
    }
  };

  const initPath = routeToStatsPath(get(route));
  path = initPath;
  started = Date.now();
  void PlatformService.StatsRecordPageVisit(path);

  const tick = setInterval(() => {
    flushSeconds(path, 5);
  }, tickMs);

  const onVisibility = (): void => {
    if (document.visibilityState === "hidden") {
      flushSeconds(path, 1);
    }
  };
  const onPageHide = (): void => {
    flushSeconds(path, 1);
  };
  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("pagehide", onPageHide);

  const unsub = route.subscribe((r) => {
    const next = routeToStatsPath(r);
    if (next === path) {
      return;
    }
    flushSeconds(path, 1);
    path = next;
    started = Date.now();
    void PlatformService.StatsRecordPageVisit(next);
  });

  return () => {
    clearInterval(tick);
    document.removeEventListener("visibilitychange", onVisibility);
    window.removeEventListener("pagehide", onPageHide);
    flushSeconds(path, 1);
    unsub();
  };
}

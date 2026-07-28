import type { ComponentType, SvelteComponent } from "svelte";
import type { Route } from "../stores/routeCodec";

type PageModule = { default: ComponentType<SvelteComponent> };

function loaderFor(route: Route): () => Promise<PageModule> {
  switch (route.page) {
    case "home":
      return () => import("../pages/Accounts.svelte");
    case "vault":
      return () => import("../pages/Vault.svelte");
    case "settings":
      return () => import("../pages/Settings.svelte");
    case "about":
      return () => import("../pages/About.svelte");
    case "tools":
      return () => import("../pages/Tools.svelte");
    case "preview-css":
      return () => import("../pages/PreviewCss.svelte");
    case "steam-advanced-clearing":
      return () => import("../pages/SteamAdvancedClearing.svelte");
    case "dota-configs":
      return () => import("../pages/DotaConfigs.svelte");
  }
}

const pageCache = new Map<string, Promise<PageModule>>();

/** Load a page module, deduplicating concurrent requests. */
export function loadPageModule(route: Route): Promise<PageModule> {
  // Settings' category selects a section *inside* one module, so the page id is still the
  // whole cache key — keying on the category too would re-import the same chunk per tab.
  let cached = pageCache.get(route.page);
  if (!cached) {
    cached = loaderFor(route)();
    pageCache.set(route.page, cached);
  }
  return cached;
}

/** Warm the cache for a route without navigating. */
export function prefetchPage(route: Route): void {
  void loadPageModule(route);
}

/** Prefetch the destinations the sidebar makes one click away. */
export function prefetchCommonPages(): void {
  prefetchPage({ page: "settings", category: "appearance" });
  prefetchPage({ page: "tools" });
  prefetchPage({ page: "vault" });
}

import { writable } from "svelte/store";
import type { PlatformStartup } from "../../bindings/steamswitch/internal/platform/models.js";

export const homeScreenData = writable<PlatformStartup | null>(null);

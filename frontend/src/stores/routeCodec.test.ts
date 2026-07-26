import { describe, expect, it } from "vitest";
import type { PlatformStartup } from "../../bindings/steamswitch/internal/platform/models.js";
import { parseHash, serializeRoute, validateRoute, type Route } from "./routeCodec";

function startup(overrides: Partial<PlatformStartup> = {}): PlatformStartup {
  return {
    homePlatformOrder: ["Steam"],
    allPlatformNames: ["Steam"],
    disabledPlatformNames: [],
    platformsFileMissing: false,
    ...overrides,
  } as PlatformStartup;
}

const ROUTES: Route[] = [
  { page: "home" },
  { page: "settings" },
  { page: "tools" },
  { page: "preview-css" },
  { page: "steam-advanced-clearing" },
  { page: "dota-configs" },
];

describe("serialize/parse round trip", () => {
  it.each(ROUTES)("round-trips %j", (route) => {
    expect(parseHash(serializeRoute(route))).toEqual(route);
  });

  it("treats an empty hash as home", () => {
    expect(parseHash("")).toEqual({ page: "home" });
    expect(parseHash("#/")).toEqual({ page: "home" });
  });

  it("rejects unknown routes so the caller can fall back", () => {
    expect(parseHash("#/nope")).toBeNull();
    expect(parseHash("#/tools/nope")).toBeNull();
  });
});

describe("legacy aliases", () => {
  it("lands old platform links on the account list", () => {
    expect(parseHash("#/platform/Steam")).toEqual({ page: "home" });
    expect(parseHash("#/platform")).toEqual({ page: "home" });
  });

  it("lands old platform-settings links on the single Settings page", () => {
    expect(parseHash("#/platform-settings/Steam")).toEqual({ page: "settings" });
  });

  it("keeps the pre-redesign hashes for the two Steam tools working", () => {
    expect(parseHash("#/steam/advanced-clearing")).toEqual({ page: "steam-advanced-clearing" });
    expect(parseHash("#/dota-configs")).toEqual({ page: "dota-configs" });
  });
});

describe("validateRoute", () => {
  it("sends everything home when Platforms.json is missing", () => {
    expect(validateRoute({ page: "tools" }, startup({ platformsFileMissing: true }))).toEqual({
      page: "home",
    });
  });

  it.each([{ page: "steam-advanced-clearing" }, { page: "dota-configs" }] as Route[])(
    "drops %j when Steam is unavailable",
    (route) => {
      expect(validateRoute(route, startup({ disabledPlatformNames: ["Steam"] }))).toEqual({
        page: "home",
      });
      expect(validateRoute(route, startup({ allPlatformNames: [] }))).toEqual({ page: "home" });
    },
  );

  it("keeps Steam tools when Steam is available", () => {
    const route: Route = { page: "dota-configs" };
    expect(validateRoute(route, startup())).toBe(route);
  });

  it("passes platform-independent routes through untouched", () => {
    const route: Route = { page: "settings" };
    expect(validateRoute(route, startup({ allPlatformNames: [] }))).toBe(route);
  });
});

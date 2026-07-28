import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS, filterVaultEntries, hasActiveFilters } from "./filter";
import type { HealthReport, Summary } from "../../../bindings/steamswitch/internal/vault/models";

function ok(): HealthReport {
  return {
    verdict: "ok",
    probedAt: "2026-07-26T12:00:00Z",
    deep: false,
    signals: [{ name: "token", status: "ok", detail: "Vault_Signal_TokenValid", blocker: false }],
  } as HealthReport;
}

function expired(): HealthReport {
  return {
    verdict: "warn",
    probedAt: "2026-07-26T12:00:00Z",
    deep: false,
    signals: [{ name: "token", status: "warn", detail: "Vault_Signal_TokenExpired", blocker: false }],
  } as HealthReport;
}

function entry(over: Partial<Summary> = {}): Summary {
  return {
    steamId64: "76561198044219337",
    accountName: "quietriver_7",
    label: "Quiet River",
    standalone: false,
    hasPassword: true,
    hasSharedSecret: false,
    hasIdentitySecret: false,
    hasRefreshToken: false,
    emailSource: "none",
    hasEmailAuth: false,
    ...over,
  } as Summary;
}

const ROSTER: Summary[] = [
  entry({ steamId64: "76561198000000001", label: "Marlow", accountName: "marlow_grey", health: ok() }),
  entry({
    steamId64: "76561198000000002",
    label: "Ash Canyon",
    accountName: "ashcanyon",
    hasPassword: false,
  }),
  entry({ steamId64: "76561198000000003", label: "Quiet River", accountName: "quietriver_7", health: expired() }),
  entry({ steamId64: "76561198000000004", label: "Nova", accountName: "nova.kx", standalone: true, health: ok() }),
];

const ids = (rows: Summary[]): string[] => rows.map((r) => r.label ?? "");

describe("filterVaultEntries", () => {
  it("returns everything with no filters", () => {
    expect(filterVaultEntries(ROSTER, EMPTY_FILTERS)).toHaveLength(4);
  });

  it("matches on label, login name and SteamID64", () => {
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, query: "quiet" }))).toEqual(["Quiet River"]);
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, query: "nova.kx" }))).toEqual(["Nova"]);
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, query: "0000002" }))).toEqual(["Ash Canyon"]);
  });

  it("is case-insensitive and ignores surrounding whitespace", () => {
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, query: "  MARLOW " }))).toEqual(["Marlow"]);
  });

  it("splits health into healthy, needs-attention and never-checked", () => {
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, health: "ok" }))).toEqual(["Marlow", "Nova"]);
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, health: "attention" }))).toEqual([
      "Quiet River",
    ]);
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, health: "unchecked" }))).toEqual([
      "Ash Canyon",
    ]);
  });

  it("filters on stored password", () => {
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, hasPassword: true }))).not.toContain(
      "Ash Canyon",
    );
  });

  // `standalone` is the stored flag; "on switcher" is its inverse, and getting the negation
  // backwards would show exactly the rows the user asked to hide.
  it("treats 'on switcher' as the inverse of standalone", () => {
    expect(ids(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, onSwitcher: true }))).not.toContain("Nova");
    expect(filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, onSwitcher: true })).toHaveLength(3);
  });

  it("combines filters with AND, not OR", () => {
    const rows = filterVaultEntries(ROSTER, { ...EMPTY_FILTERS, health: "ok", onSwitcher: true });
    expect(ids(rows)).toEqual(["Marlow"]);
  });
});

describe("hasActiveFilters", () => {
  it("is false for the empty set", () => {
    expect(hasActiveFilters(EMPTY_FILTERS)).toBe(false);
  });

  it("ignores a whitespace-only query, which is not a filter the user set", () => {
    expect(hasActiveFilters({ ...EMPTY_FILTERS, query: "   " })).toBe(false);
  });

  it("is true for each individual filter", () => {
    expect(hasActiveFilters({ ...EMPTY_FILTERS, query: "a" })).toBe(true);
    expect(hasActiveFilters({ ...EMPTY_FILTERS, health: "ok" })).toBe(true);
    expect(hasActiveFilters({ ...EMPTY_FILTERS, hasPassword: true })).toBe(true);
    expect(hasActiveFilters({ ...EMPTY_FILTERS, onSwitcher: true })).toBe(true);
  });
});

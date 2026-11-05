import { get } from "svelte/store";
import { beforeEach, describe, expect, it, vi } from "vitest";

const ListEntries = vi.fn();
vi.mock("../../bindings/steamswitch/internal/vault/vaultservice.js", () => ({
  ListEntries: () => ListEntries(),
}));

const { loadVaultEntries, vaultEntries, vaultLoadError, vaultStatus } = await import("./vault");

describe("loadVaultEntries", () => {
  beforeEach(() => {
    ListEntries.mockReset();
    vaultStatus.update((s) => ({ ...s, locked: false, appPasswordSet: true }));
  });

  it("keeps a failed read apart from an empty vault", async () => {
    const damaged = new Error("Toast_Vault_Corrupt");
    ListEntries.mockRejectedValue(damaged);
    await expect(loadVaultEntries()).rejects.toBe(damaged);
    expect(get(vaultLoadError)).toBe(damaged);
    expect(get(vaultEntries)).toEqual([]);
  });

  it("clears the failure once the file reads again", async () => {
    ListEntries.mockRejectedValueOnce(new Error("Toast_Vault_Corrupt"));
    await loadVaultEntries().catch(() => {});
    ListEntries.mockResolvedValue([]);
    await loadVaultEntries();
    expect(get(vaultLoadError)).toBeNull();
  });
});

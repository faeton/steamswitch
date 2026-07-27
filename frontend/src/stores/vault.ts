/**
 * Account vault state for the UI (VAULT.md).
 *
 * Two rules this store exists to enforce, both of which are easy to break by accident in a
 * component:
 *
 *  - **A secret is never held in a store.** `reveal()` returns a value to the caller and
 *    nothing here keeps it. A revealed password living in a writable would survive the
 *    component that showed it, and would be readable by every other component.
 *  - **Summaries only.** Everything cached here — the entry list, the health reports — comes
 *    from the Go side's redacted `Summary`, which carries "has a password", never the
 *    password.
 */
import { get, writable } from "svelte/store";
import * as VaultService from "../../bindings/steamswitch/internal/vault/vaultservice.js";
import type { CodeResult, HealthReport, Status, Summary } from "../../bindings/steamswitch/internal/vault/models";

export type VaultStatus = Status;
export type VaultEntry = Summary;
export type VaultHealth = HealthReport;
export type VaultCode = CodeResult;

/** Field names the Go side will accept for a reveal. Anything else is rejected there too. */
export const VAULT_FIELDS = {
  password: "password",
  sharedSecret: "sharedSecret",
  identitySecret: "identitySecret",
  refreshToken: "refreshToken",
  guardData: "guardData",
  emailPassword: "emailPassword",
  mailboxToken: "mailboxToken",
} as const;

export type VaultField = (typeof VAULT_FIELDS)[keyof typeof VAULT_FIELDS];

const EMPTY_STATUS = {
  appPasswordSet: false,
  locked: false,
  initialised: false,
  entryCount: 0,
  rateLimited: false,
  hasApiKey: false,
} as VaultStatus;

export const vaultStatus = writable<VaultStatus>(EMPTY_STATUS);
export const vaultEntries = writable<VaultEntry[]>([]);
export const vaultLoaded = writable(false);
export const vaultBusy = writable(false);

/**
 * Health verdicts keyed by SteamID64, so a tile can show a dot without every tile asking the
 * backend. Seeded from the entry list, which already carries the last stored report.
 */
export const vaultHealth = writable<Record<string, VaultHealth>>({});

export async function loadVaultStatus(): Promise<VaultStatus> {
  const status = await VaultService.GetStatus();
  vaultStatus.set(status);
  return status;
}

export async function loadVaultEntries(): Promise<VaultEntry[]> {
  const status = get(vaultStatus);
  if (status.locked || !status.appPasswordSet) {
    // Nothing to load and nothing to report: a locked vault is a normal state, not an error
    // worth toasting every time a page mounts.
    vaultEntries.set([]);
    vaultLoaded.set(true);
    return [];
  }
  const list = await VaultService.ListEntries();
  vaultEntries.set(list);
  const health: Record<string, VaultHealth> = {};
  for (const e of list) {
    if (e.health) {
      health[e.steamId64] = e.health;
    }
  }
  vaultHealth.set(health);
  vaultLoaded.set(true);
  return list;
}

export async function refreshVault(): Promise<void> {
  await loadVaultStatus();
  await loadVaultEntries();
}

export function vaultEntryFor(steamId64: string): VaultEntry | undefined {
  return get(vaultEntries).find((e) => e.steamId64 === steamId64);
}

/**
 * Reveal one secret. The value is returned, never stored — see the note at the top.
 *
 * Callers are expected to clear whatever local variable they put it in; the components here
 * do that on a timer and on unmount.
 */
export async function revealField(steamId64: string, field: VaultField): Promise<string> {
  return VaultService.RevealField(steamId64, field);
}

export async function saveEntry(draft: Record<string, unknown>): Promise<void> {
  vaultBusy.set(true);
  try {
    await VaultService.SaveEntry(draft as never);
    await refreshVault();
  } finally {
    vaultBusy.set(false);
  }
}

export async function deleteEntry(steamId64: string): Promise<void> {
  vaultBusy.set(true);
  try {
    await VaultService.DeleteEntry(steamId64);
    await refreshVault();
  } finally {
    vaultBusy.set(false);
  }
}

export async function guardCode(steamId64: string): Promise<VaultCode> {
  return VaultService.GetGuardCode(steamId64);
}

/** Hand a user-typed Guard code to a verification login that is waiting for one (an account
 *  whose inbox cannot be IMAP-checked). requestId is the id from the vault-guard-code-needed
 *  event this answers. */
export async function submitManualGuardCode(
  steamId64: string,
  requestId: string,
  code: string,
): Promise<void> {
  return VaultService.SubmitManualGuardCode(steamId64, requestId, code);
}

export async function quickCheck(steamId64: string): Promise<VaultHealth> {
  const rep = await VaultService.RunQuickCheck(steamId64);
  vaultHealth.update((h) => ({ ...h, [steamId64]: rep }));
  return rep;
}

export async function quickCheckAll(): Promise<void> {
  vaultBusy.set(true);
  try {
    const reports = await VaultService.RunQuickCheckAll();
    vaultHealth.update((h) => {
      const next = { ...h };
      for (const [id, rep] of Object.entries(reports)) {
        if (rep) {
          next[id] = rep;
        }
      }
      return next;
    });
    await loadVaultEntries();
  } finally {
    vaultBusy.set(false);
  }
}

/**
 * Verify credentials against Steam. This one logs in for real and usually costs the user a
 * Steam Guard email, so it is never called on a timer or in a loop from the UI.
 */
export async function deepCheck(steamId64: string): Promise<VaultHealth> {
  vaultBusy.set(true);
  try {
    const rep = await VaultService.RunDeepCheck(steamId64);
    vaultHealth.update((h) => ({ ...h, [steamId64]: rep }));
    await loadVaultEntries();
    return rep;
  } finally {
    vaultBusy.set(false);
  }
}

export async function testEmailBinding(steamId64: string): Promise<void> {
  await VaultService.TestEmailBinding(steamId64);
}

export async function discoverIMAP(address: string, password: string): Promise<string> {
  return VaultService.DiscoverIMAP(address, password);
}

export async function tokenDetails(steamId64: string) {
  return VaultService.GetTokenDetails(steamId64);
}

/**
 * Handoff (VAULT.md §9). The bundle bytes never come through here — Export returns the path
 * it wrote, and the import calls address a file by name inside the handoff folder. Putting a
 * passphrase-sealed blob of full account access into JavaScript would be a strictly worse
 * trade than passing a filename.
 */
export async function exportHandoff(req: {
  steamId64: string;
  mode: string;
  label: string;
  passphrase: string;
  expiresInDays: number;
  singleUse: boolean;
  confirm: string;
}) {
  vaultBusy.set(true);
  try {
    return await VaultService.ExportHandoff(req as never);
  } finally {
    vaultBusy.set(false);
  }
}

export async function listHandoffBundles() {
  return VaultService.ListHandoffBundles();
}

export async function inspectHandoffBundle(name: string, passphrase: string) {
  return VaultService.InspectHandoffBundle(name, passphrase);
}

export async function acceptHandoffBundle(name: string, passphrase: string) {
  vaultBusy.set(true);
  try {
    const info = await VaultService.AcceptHandoffBundle(name, passphrase);
    await refreshVault();
    return info;
  } finally {
    vaultBusy.set(false);
  }
}

export async function handoffLog() {
  return VaultService.GetHandoffLog();
}

export async function handoffFolder(): Promise<string> {
  return VaultService.GetHandoffFolder();
}

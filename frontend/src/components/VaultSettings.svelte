<script lang="ts">
  /**
   * Settings → Account vault (VAULT.md §7).
   *
   * Two things live here and nothing else. The rest of the vault is per-account and belongs
   * on the account menu.
   *
   *  - **Enabling it**, which means setting an app password if there is not one. There is no
   *    master key without a password, so this is a precondition, not a preference — the
   *    section says so rather than offering a toggle that would silently do nothing.
   *  - **The Steam Web API key**, which is optional: without it the ban and profile signals
   *    report "unknown" instead of failing.
   *  - **Scheduled password checks**, off by default. This is the one setting in the app
   *    that makes it log in to a Steam account on its own, so it says so plainly rather
   *    than reading as a convenience toggle.
   */
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { route } from "../stores/nav";
  import { loadSecurityStatus, securityStatus, setAppPassword } from "../stores/security";
  import { openConfirm, openPasswordSetupModal, openPrompt } from "../stores/modal";
  import { deleteEntry, refreshVault, vaultEntries, vaultStatus } from "../stores/vault";
  import {
    DEFAULT_PREFS,
    loadVaultSecurityPrefs,
    saveVaultSecurityPrefs,
    vaultSecurityPrefs,
    type VaultSecurityPrefs,
  } from "../stores/autoLock";
  import { lockApp } from "../stores/security";
  import SettingsCard from "./settings/SettingsCard.svelte";
  import SettingRow from "./settings/SettingRow.svelte";
  import DangerZone from "./settings/DangerZone.svelte";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import type { Settings } from "../../bindings/steamswitch/internal/steam/models";
  import { sanitizeSettingsPayload } from "../lib/platformSettingsShared";

  /** Matches vault.MinDeepCheckInterval. The Go side clamps regardless; this only stops the
   * input offering a value that would be silently raised. */
  const MIN_CHECK_DAYS = 1;
  const DEFAULT_CHECK_DAYS = 14;

  let apiKey = "";
  let apiKeyDirty = false;
  let busy = false;
  let scheduledChecks = false;
  let checkDays = DEFAULT_CHECK_DAYS;

  onMount(async () => {
    await refreshVault();
    prefs = await loadVaultSecurityPrefs();
    prefsLoaded = true;
    try {
      const st = (await SteamService.GetSteamSettings()) as Settings;
      const raw = st as unknown as Record<string, unknown>;
      apiKey = (raw["Steam_WebApiKey"] as string) ?? "";
      const days = Number(raw["Steam_VaultDeepCheckDays"] ?? 0);
      scheduledChecks = days > 0;
      checkDays = days > 0 ? days : DEFAULT_CHECK_DAYS;
    } catch {
      apiKey = "";
    }
  });

  /** Re-read before writing, for the same reason SteamSettings does: this component owns a
   * couple of fields and must not write back a whole struct it read minutes ago. */
  async function patchSettings(apply: (s: Record<string, unknown>) => void): Promise<void> {
    busy = true;
    try {
      const fresh = (await SteamService.GetSteamSettings()) as unknown as Record<string, unknown>;
      apply(fresh);
      await SteamService.SaveSteamSettings(sanitizeSettingsPayload(fresh) as unknown as Settings);
      await refreshVault();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
      throw e;
    } finally {
      busy = false;
    }
  }

  async function saveApiKey(): Promise<void> {
    try {
      await patchSettings((s) => {
        s["Steam_WebApiKey"] = apiKey.trim();
      });
      apiKeyDirty = false;
    } catch {
      /* reported by patchSettings */
    }
  }

  async function saveSchedule(): Promise<void> {
    const days = scheduledChecks ? Math.max(MIN_CHECK_DAYS, Math.round(checkDays) || DEFAULT_CHECK_DAYS) : 0;
    try {
      await patchSettings((s) => {
        s["Steam_VaultDeepCheckDays"] = days;
      });
      if (days > 0) {
        checkDays = days;
      }
    } catch {
      /* reported by patchSettings */
    }
  }

  async function enableVault(): Promise<void> {
    const result = await openPasswordSetupModal({ title: $t("Vault_Settings_SetPasswordTitle") });
    if (!result?.password) {
      return;
    }
    busy = true;
    try {
      await setAppPassword(result.password);
      await loadSecurityStatus();
      await refreshVault();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_EnableFailed"), e), duration: 8000 });
    } finally {
      busy = false;
    }
  }

  /* ---------------------------------------------- app-lock behaviour (brief A10) */

  /**
   * The three "secrets do not linger" settings.
   *
   * They are here rather than under System because they are the *terms* of the vault: how
   * long it stays open, how long a revealed secret stays on screen, and what happens to a
   * copied password. The brief asks for trust settings to be prominent, not buried among
   * refresh intervals.
   */
  const AUTO_LOCK_CHOICES = [0, 1, 5, 15, 30, 60, 240];

  let prefs: VaultSecurityPrefs = { ...DEFAULT_PREFS };
  let prefsLoaded = false;

  async function savePrefs(next: Partial<VaultSecurityPrefs>): Promise<void> {
    const merged = { ...prefs, ...next };
    busy = true;
    try {
      await saveVaultSecurityPrefs(merged);
      // Read back rather than trusting what was sent: Go clamps both intervals, and the row
      // has to show what was actually stored.
      prefs = { ...get(vaultSecurityPrefs) };
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      busy = false;
    }
  }

  function autoLockLabel(minutes: number): string {
    return minutes === 0 ? $t("Vault_AutoLock_Never") : $t("Vault_AutoLock_After", { minutes });
  }

  async function lockNow(): Promise<void> {
    const ok = await lockApp();
    if (!ok) {
      pushToast({ type: "info", message: $t("Vault_Lock_Busy"), duration: 5000 });
    }
  }

  /**
   * Delete the vault and every stored credential.
   *
   * Two gates, and the second is deliberately a typed confirmation rather than another
   * "Are you sure?": this destroys data that has no backup by construction, so the user has
   * to demonstrate they read the sentence, not just that they can click twice.
   */
  async function deleteVault(): Promise<void> {
    const first = await openConfirm({
      title: $t("Vault_Delete_All_Title"),
      body: $t("Vault_Delete_All_Body"),
      positiveLabel: $t("Vault_Delete_All_Continue"),
      style: "yesno",
    });
    if (!first) return;

    const typed = await openPrompt({
      title: $t("Vault_Delete_All_Title"),
      body: $t("Vault_Delete_All_TypeToConfirm", { word: $t("Vault_Delete_All_Word") }),
      inputType: "text",
      positiveLabel: $t("Vault_Delete_All_Confirm"),
    });
    if ((typed ?? "").trim().toLowerCase() !== $t("Vault_Delete_All_Word").toLowerCase()) {
      return;
    }

    busy = true;
    try {
      // Every entry, one at a time — there is no batch delete, and inventing one here would
      // put a destructive loop outside the store that owns vault writes.
      for (const entry of get(vaultEntries)) {
        await deleteEntry(entry.steamId64);
      }
      await refreshVault();
      pushToast({ type: "success", message: $t("Vault_Delete_All_Done"), duration: 6000 });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_DeleteFailed"), e),
        duration: 8000,
      });
    } finally {
      busy = false;
    }
  }
</script>

<SettingsCard title={$t("Vault_Settings_Header")} description={$t("Vault_Settings_Blurb")}>
  {#if !$securityStatus.appPasswordSet}
    <SettingRow label={$t("Vault_Settings_Enable")} description={$t("Vault_Settings_NeedsPassword")}>
      <button type="button" class="ss-btn ss-btn--primary" disabled={busy} on:click={enableVault}>
        {$t("Vault_Settings_Enable")}
      </button>
    </SettingRow>
  {:else}
    <SettingRow
      label={$t("Vault_Settings_State")}
      description={$vaultStatus.locked
        ? $t("Vault_Settings_Locked")
        : $t("Vault_Settings_EntryCount", { count: String($vaultStatus.entryCount) })}
    >
      {#if !$vaultStatus.locked}
        <button type="button" class="ss-btn" disabled={busy} on:click={lockNow}>
          {$t("Sidebar_Vault_LockNow")}
        </button>
      {/if}
      <button type="button" class="ss-btn" on:click={() => route.set({ page: "vault" })}>
        {$t("Vault_Settings_OpenVault")}
      </button>
    </SettingRow>
  {/if}

  {#if $securityStatus.appPasswordSet}
    <SettingRow label={$t("Vault_AutoLock_Label")} description={$t("Vault_AutoLock_Desc")}>
      <select
        class="ss-field"
        disabled={busy || !prefsLoaded}
        value={prefs.autoLockMinutes}
        on:change={(e) => void savePrefs({ autoLockMinutes: Number(e.currentTarget.value) })}
      >
        {#each AUTO_LOCK_CHOICES as minutes (minutes)}
          <option value={minutes}>{autoLockLabel(minutes)}</option>
        {/each}
      </select>
    </SettingRow>

    <SettingRow label={$t("Vault_Reveal_Label")} description={$t("Vault_Reveal_Desc")}>
      <input
        type="number"
        class="ss-field vault-num"
        min="3"
        max="60"
        step="1"
        disabled={busy || !prefsLoaded}
        value={prefs.revealSeconds}
        on:change={(e) => void savePrefs({ revealSeconds: Number(e.currentTarget.value) })}
      />
    </SettingRow>

    <SettingRow label={$t("Vault_Clipboard_Label")} description={$t("Vault_Clipboard_Desc")}>
      <input
        type="checkbox"
        class="ss-toggle"
        disabled={busy || !prefsLoaded}
        checked={prefs.clearClipboard}
        aria-label={$t("Vault_Clipboard_Label")}
        on:change={(e) => void savePrefs({ clearClipboard: e.currentTarget.checked })}
      />
    </SettingRow>
  {/if}
</SettingsCard>

<SettingsCard title={$t("Vault_Settings_Checks")} description={$t("Vault_Settings_Checks_Desc")}>
  <SettingRow
    label={$t("Vault_Settings_APIKey")}
    description={$t("Vault_Settings_APIKeyHint")}
    controlId="vault-api-key"
  >
    <input
      id="vault-api-key"
      type="password"
      class="ss-field vault-key"
      bind:value={apiKey}
      on:input={() => (apiKeyDirty = true)}
      autocomplete="off"
      spellcheck="false"
    />
    <button type="button" class="ss-btn" disabled={busy || !apiKeyDirty} on:click={saveApiKey}>
      {$t("Vault_Settings_SaveKey")}
    </button>
  </SettingRow>

  <SettingRow
    label={$t("Vault_Settings_ScheduledChecks")}
    description={$t("Vault_Settings_ScheduledChecksHint")}
  >
    <input
      type="checkbox"
      class="ss-toggle"
      bind:checked={scheduledChecks}
      disabled={busy || $vaultStatus.locked}
      aria-label={$t("Vault_Settings_ScheduledChecks")}
      on:change={saveSchedule}
    />
  </SettingRow>

  {#if scheduledChecks}
    <SettingRow
      label={$t("Vault_Settings_CheckEveryDays")}
      description={$t("Vault_Settings_ScheduledChecksWarning")}
      controlId="vault-check-days"
    >
      <input
        id="vault-check-days"
        type="number"
        class="ss-field vault-num"
        min={MIN_CHECK_DAYS}
        step="1"
        bind:value={checkDays}
        disabled={busy}
      />
      <button type="button" class="ss-btn" disabled={busy} on:click={saveSchedule}>
        {$t("Vault_Settings_SaveKey")}
      </button>
    </SettingRow>
  {/if}

  {#if $vaultStatus.rateLimited}
    <SettingRow label={$t("Vault_Health_RateLimited")} />
  {/if}
</SettingsCard>

{#if $securityStatus.appPasswordSet}
  <DangerZone>
    <SettingRow label={$t("Vault_Delete_All_Title")} description={$t("Vault_Delete_All_Row")} danger>
      <button type="button" class="ss-btn ss-btn--danger" disabled={busy} on:click={deleteVault}>
        {$t("Vault_Delete_All_Button")}
      </button>
    </SettingRow>
  </DangerZone>
{/if}

<style>
  .vault-num {
    width: 92px;
  }

  .vault-key {
    width: 260px;
  }

</style>

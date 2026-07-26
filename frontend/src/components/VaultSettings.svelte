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
   */
  import { onMount } from "svelte";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { route } from "../stores/nav";
  import { loadSecurityStatus, securityStatus, setAppPassword } from "../stores/security";
  import { openPasswordSetupModal } from "../stores/modal";
  import { refreshVault, vaultStatus } from "../stores/vault";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import type { Settings } from "../../bindings/steamswitch/internal/steam/models";
  import { sanitizeSettingsPayload } from "../lib/platformSettingsShared";

  let apiKey = "";
  let apiKeyDirty = false;
  let busy = false;

  onMount(async () => {
    await refreshVault();
    try {
      const st = (await SteamService.GetSteamSettings()) as Settings;
      apiKey = (st as unknown as Record<string, string>)["Steam_WebApiKey"] ?? "";
    } catch {
      apiKey = "";
    }
  });

  async function saveApiKey(): Promise<void> {
    busy = true;
    try {
      // Re-read before writing, for the same reason SteamSettings does: this component owns
      // exactly one field and must not write back a whole struct it read minutes ago.
      const fresh = (await SteamService.GetSteamSettings()) as unknown as Record<string, unknown>;
      fresh["Steam_WebApiKey"] = apiKey.trim();
      await SteamService.SaveSteamSettings(sanitizeSettingsPayload(fresh) as unknown as Settings);
      apiKeyDirty = false;
      await refreshVault();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      busy = false;
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
</script>

<section class="vaultSettings">
  <h2>{$t("Vault_Settings_Header")}</h2>
  <p class="hint">{$t("Vault_Settings_Blurb")}</p>

  {#if !$securityStatus.appPasswordSet}
    <p class="hint">{$t("Vault_Settings_NeedsPassword")}</p>
    <button type="button" disabled={busy} on:click={enableVault}>{$t("Vault_Settings_Enable")}</button>
  {:else if $vaultStatus.locked}
    <p class="hint">{$t("Vault_Settings_Locked")}</p>
  {:else}
    <p class="hint">
      {$t("Vault_Settings_EntryCount", { count: String($vaultStatus.entryCount) })}
    </p>
    <button type="button" on:click={() => route.set({ page: "tools" })}>
      {$t("Vault_Settings_OpenTools")}
    </button>
  {/if}

  <label class="row">
    <span>{$t("Vault_Settings_APIKey")}</span>
    <span class="withAction">
      <input
        type="password"
        bind:value={apiKey}
        on:input={() => (apiKeyDirty = true)}
        autocomplete="off"
        spellcheck="false"
      />
      <button type="button" disabled={busy || !apiKeyDirty} on:click={saveApiKey}>
        {$t("Vault_Settings_SaveKey")}
      </button>
    </span>
  </label>
  <p class="hint small">{$t("Vault_Settings_APIKeyHint")}</p>

  {#if $vaultStatus.rateLimited}
    <p class="hint">{$t("Vault_Health_RateLimited")}</p>
  {/if}
</section>

<style lang="scss">
  .vaultSettings {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    margin-top: 1rem;
  }

  h2 {
    margin: 0;
    font-size: 1rem;
  }

  .hint {
    margin: 0;
    font-size: 0.85rem;
    opacity: 0.85;
  }

  .small {
    font-size: 0.8rem;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    margin-top: 0.4rem;
  }

  .row > span:first-child {
    font-size: 0.85rem;
    opacity: 0.85;
  }

  .withAction {
    display: flex;
    gap: 0.35rem;
    align-items: center;
  }

  .withAction input {
    flex: 1 1 auto;
    min-width: 0;
  }

  button {
    align-self: flex-start;
  }
</style>

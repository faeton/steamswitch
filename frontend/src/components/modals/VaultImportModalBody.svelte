<script lang="ts">
  /**
   * Accepting a handoff bundle (VAULT.md §9).
   *
   * Two steps, and the split is the point: **inspect before accept.** The label and mode live
   * inside the ciphertext, so nothing can be said about a bundle until the passphrase proves
   * the recipient was meant to have it — and once it is readable, they see exactly what is
   * about to be written before anything is.
   *
   * Inspect writes nothing. Accept is the only button here that touches the vault.
   */
  import { onDestroy, onMount } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { acceptHandoffBundle, handoffFolder, inspectHandoffBundle, listHandoffBundles, vaultBusy } from "../../stores/vault";
  import type { AvailableBundle, BundleInfo } from "../../../bindings/steamswitch/internal/vault/models";

  let bundles: AvailableBundle[] = [];
  let selected = "";
  let passphrase = "";
  let info: BundleInfo | null = null;
  let accepted = false;
  let folder = "";

  onMount(async () => {
    await refresh();
    try {
      folder = await handoffFolder();
    } catch {
      folder = "";
    }
  });

  onDestroy(() => {
    passphrase = "";
    info = null;
  });

  async function refresh(): Promise<void> {
    try {
      bundles = await listHandoffBundles();
      if (!bundles.some((b) => b.name === selected)) {
        selected = bundles[0]?.name ?? "";
      }
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Handoff_ListFailed"), e), duration: 8000 });
    }
  }

  async function doInspect(): Promise<void> {
    info = null;
    accepted = false;
    try {
      info = await inspectHandoffBundle(selected, passphrase);
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Handoff_InspectFailed"), e), duration: 8000 });
    }
  }

  async function doAccept(): Promise<void> {
    try {
      info = await acceptHandoffBundle(selected, passphrase);
      accepted = true;
      passphrase = "";
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Handoff_AcceptFailed"), e), duration: 8000 });
    }
  }

  $: blocked = !!info && (info.expired || (info.singleUse && info.alreadyImported));
</script>

<div class="import">
  {#if bundles.length === 0}
    <p class="small">{$t("Handoff_Import_NoBundles")}</p>
    {#if folder}
      <p class="path">{folder}</p>
    {/if}
    <button type="button" on:click={refresh}>{$t("Handoff_Import_Rescan")}</button>
  {:else}
    <label class="row">
      <span>{$t("Handoff_Import_Pick")}</span>
      <select bind:value={selected} on:change={() => ((info = null), (accepted = false))}>
        {#each bundles as b (b.path)}
          <option value={b.name}>{b.name}</option>
        {/each}
      </select>
    </label>

    <label class="row">
      <span>{$t("Handoff_Import_Passphrase")}</span>
      <input type="password" bind:value={passphrase} autocomplete="off" spellcheck="false" />
    </label>
    <p class="small">{$t("Handoff_Import_PassphraseHint")}</p>

    <button type="button" disabled={$vaultBusy || !selected || !passphrase} on:click={doInspect}>
      {$t("Handoff_Import_Inspect")}
    </button>

    {#if info}
      <div class="preview">
        <p class="small">
          {info.mode === "transfer" ? $t("Handoff_Import_IsTransfer") : $t("Handoff_Import_IsGrant")}
        </p>
        <dl>
          {#if info.label}
            <dt>{$t("Handoff_Import_Label")}</dt>
            <dd>{info.label}</dd>
          {/if}
          {#if info.accountName}
            <dt>{$t("Handoff_Import_Account")}</dt>
            <dd>{info.accountName}</dd>
          {/if}
          <dt>{$t("Handoff_Import_Issued")}</dt>
          <dd>{info.issuedAt}</dd>
          {#if info.expiresAt}
            <dt>{$t("Handoff_Import_Expires")}</dt>
            <dd>{info.expiresAt}</dd>
          {/if}
          <dt>{$t("Handoff_Import_Carries")}</dt>
          <dd>
            {[
              info.hasPassword ? $t("Handoff_Import_CarriesPassword") : "",
              info.hasSharedSecret ? $t("Handoff_Import_CarriesSeed") : "",
              info.hasRefreshToken ? $t("Handoff_Import_CarriesToken") : "",
              info.hasEmail ? $t("Handoff_Import_CarriesEmail") : "",
            ]
              .filter(Boolean)
              .join(", ")}
          </dd>
        </dl>

        {#if info.expired}
          <p class="warn">{$t("Handoff_Import_Expired")}</p>
        {:else if info.singleUse && info.alreadyImported}
          <p class="warn">{$t("Handoff_Import_AlreadyUsed")}</p>
        {:else if info.replaces}
          <!-- Said before the write, not discovered after it — and it has to say the right
               one: a transfer replaces the entry, a grant only adds its session to it. -->
          <p class="warn">
            {info.mode === "transfer" ? $t("Handoff_Import_WillReplace") : $t("Handoff_Import_WillMerge")}
          </p>
        {/if}

        {#if accepted}
          <p class="small">{$t("Handoff_Import_Done")}</p>
        {:else}
          <button type="button" disabled={$vaultBusy || blocked} on:click={doAccept}>
            {$t("Handoff_Import_Accept")}
          </button>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style lang="scss">
  .import {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-width: 100%;
  }

  .small {
    margin: 0;
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .warn {
    margin: 0.3rem 0 0;
    padding: 0.4rem 0.5rem;
    border-radius: 0.3rem;
    font-size: var(--fs-meta);
    background: rgba(200, 140, 0, 0.15);
  }

  .path {
    margin: 0;
    font-family: monospace;
    font-size: var(--fs-meta);
    overflow-wrap: anywhere;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    margin-top: 0.3rem;
  }

  .row > span:first-child {
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .preview {
    margin-top: 0.5rem;
    padding-top: 0.4rem;
    border-top: 1px solid rgba(128, 128, 128, 0.35);
  }

  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.15rem 0.5rem;
    margin: 0.3rem 0 0;
    font-size: var(--fs-meta);
  }

  dt {
    opacity: 0.75;
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  select,
  input[type="password"] {
    width: 100%;
    min-width: 0;
  }

  button {
    align-self: flex-start;
    margin-top: 0.4rem;
  }
</style>

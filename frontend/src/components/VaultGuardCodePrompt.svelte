<script lang="ts">
  /**
   * Manual Steam Guard code prompt.
   *
   * A verification login for an account whose inbox cannot be IMAP-checked cannot fetch the code
   * itself, so the backend fires a `vault-guard-code-needed` event and blocks until the user types
   * the code here. Submitting hands it back through SubmitManualGuardCode; the login then continues.
   * Cancelling just closes the prompt — the backend login times out on its own.
   */
  import { onMount, tick } from "svelte";
  import { fly } from "svelte/transition";
  import { Events } from "@wailsio/runtime";
  import { t } from "../stores/i18n";
  import { submitManualGuardCode } from "../stores/vault";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";

  let visible = false;
  let steamId64 = "";
  let requestId = "";
  let hint = "";
  let code = "";
  let busy = false;
  let inputEl: HTMLInputElement | null = null;

  async function show(id: string, reqId: string, h: string): Promise<void> {
    steamId64 = id;
    requestId = reqId;
    hint = h;
    code = "";
    busy = false;
    visible = true;
    await tick();
    inputEl?.focus();
  }

  function hide(): void {
    visible = false;
    steamId64 = "";
    requestId = "";
    hint = "";
    code = "";
    busy = false;
  }

  async function submit(): Promise<void> {
    const value = code.trim().toUpperCase();
    if (!value || busy) return;
    busy = true;
    try {
      await submitManualGuardCode(steamId64, requestId, value);
      hide();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_ManualCodeFailed"), e) });
      busy = false;
    }
  }

  onMount(() => {
    const off = Events.On("vault-guard-code-needed", (ev) => {
      const data = ev.data as { steamId64?: string; requestId?: string; hint?: string } | undefined;
      const id = typeof data?.steamId64 === "string" ? data.steamId64 : "";
      const reqId = typeof data?.requestId === "string" ? data.requestId : "";
      if (id && reqId) void show(id, reqId, typeof data?.hint === "string" ? data.hint : "");
    });
    return () => off();
  });
</script>

{#if visible}
  <div class="gc-overlay" role="dialog" aria-modal="true" aria-label={$t("Vault_ManualCode_Title")}>
    <div class="gc-card" in:fly={{ y: 12, duration: 200 }}>
      <h3>{$t("Vault_ManualCode_Title")}</h3>
      <p class="gc-hint">
        {hint ? $t("Vault_ManualCode_PromptTo", { hint }) : $t("Vault_ManualCode_Prompt")}
      </p>
      <form on:submit|preventDefault={submit}>
        <input
          bind:this={inputEl}
          type="text"
          bind:value={code}
          maxlength="10"
          autocomplete="one-time-code"
          spellcheck="false"
          placeholder={$t("Vault_ManualCode_Placeholder")}
        />
        <div class="gc-actions">
          <button type="button" disabled={busy} on:click={hide}>{$t("Vault_Action_Cancel")}</button>
          <button type="submit" disabled={busy || !code.trim()}>{$t("Vault_Action_Submit")}</button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style lang="scss">
  .gc-overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.5);
    z-index: 1000;
  }

  .gc-card {
    background: var(--notification-main-bg, #1e1e1e);
    color: var(--notification-message-fg, #eee);
    border: 1px solid var(--modal-border, transparent);
    border-radius: var(--notification-border-radius, 0.6rem);
    padding: 1.25rem 1.5rem;
    width: min(90vw, 22rem);
    box-shadow: 0 8px 30px rgba(0, 0, 0, 0.4);
  }

  h3 {
    margin: 0 0 0.5rem;
  }

  .gc-hint {
    font-size: var(--fs-secondary);
    opacity: 0.85;
    margin: 0 0 0.75rem;
  }

  input {
    width: 100%;
    box-sizing: border-box;
    font-family: monospace;
    font-size: 1.4rem;
    letter-spacing: 0.2rem;
    text-align: center;
    text-transform: uppercase;
    padding: 0.5rem;
  }

  .gc-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.9rem;
  }
</style>

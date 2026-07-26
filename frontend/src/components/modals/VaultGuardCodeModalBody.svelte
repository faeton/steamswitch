<script lang="ts">
  /**
   * "Get me the Guard code, now" (VAULT.md §4).
   *
   * The whole screen is one value and a copy button. Deliberately: reading mail is not the
   * feature, getting one code is, and anything else here is between the user and the Steam
   * prompt they are staring at.
   *
   * The countdown is only shown for authenticator codes, because only those have a fixed
   * expiry. An emailed code has no clock this app could honestly display.
   */
  import { onDestroy, onMount } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { guardCode, type VaultCode } from "../../stores/vault";

  export let steamId64: string;

  let code: VaultCode | null = null;
  let error = "";
  let loading = true;
  let remaining = 0;
  let ticker: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    void fetchCode();
  });

  onDestroy(() => {
    if (ticker) {
      clearInterval(ticker);
      ticker = null;
    }
  });

  async function fetchCode(): Promise<void> {
    loading = true;
    error = "";
    try {
      code = await guardCode(steamId64);
      remaining = code.remainingSeconds ?? 0;
      if (remaining > 0 && !ticker) {
        ticker = setInterval(() => {
          remaining = Math.max(0, remaining - 1);
        }, 1000);
      }
      // Copying straight away is the point: the user is at a prompt with a cursor in it.
      await copy();
    } catch (e) {
      error = formatToastWithError($t("Toast_Vault_CodeFailed"), e);
      code = null;
    } finally {
      loading = false;
    }
  }

  async function copy(): Promise<void> {
    if (!code?.code) {
      return;
    }
    try {
      await navigator.clipboard.writeText(code.code);
      pushToast({ type: "success", message: $t("Toast_Copied"), duration: 2000 });
    } catch {
      // The code is on screen; a refused clipboard is not a failure worth an error toast.
    }
  }
</script>

<div class="guard">
  {#if loading}
    <p class="status">{$t("Vault_Code_Fetching")}</p>
  {:else if error}
    <p class="status">{error}</p>
    <p class="small">{$t("Vault_Code_ManualFallback")}</p>
  {:else if code}
    <p class="code">{code.code}</p>
    <p class="small">
      {code.method === "totp" ? $t("Vault_Code_FromAuthenticator") : $t("Vault_Code_FromInbox")}
      {#if remaining > 0}
        · {$t("Vault_Code_ExpiresIn", { seconds: String(remaining) })}
      {/if}
    </p>
  {/if}

  <div class="actions">
    <button type="button" disabled={loading || !code} on:click={copy}>{$t("Vault_Action_Copy")}</button>
    <button type="button" disabled={loading} on:click={fetchCode}>{$t("Vault_Code_Again")}</button>
  </div>
</div>

<style lang="scss">
  .guard {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    align-items: center;
    text-align: center;
  }

  .code {
    font-family: monospace;
    font-size: 2.2rem;
    letter-spacing: 0.25rem;
    margin: 0.5rem 0 0;
    user-select: all;
  }

  .status {
    margin: 0.5rem 0;
  }

  .small {
    font-size: 0.8rem;
    opacity: 0.85;
    margin: 0;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.5rem;
  }
</style>

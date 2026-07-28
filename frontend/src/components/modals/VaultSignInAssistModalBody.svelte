<script lang="ts">
  /**
   * The assistant that sits beside Steam's own login screen (VAULT.md §4, extended).
   *
   * `Vault_Menu_GuardCode` already answers "get me the code" for an account that has a tile.
   * This screen answers the case that has no tile at all: an account that lives in the vault
   * but has never signed into Steam on this machine. There is no shortcut for that — Steam's
   * switcher is Steam's own `loginusers.vdf` — so the user has to type a username, a password
   * and a Guard code into Steam by hand. This panel is the three of them, in that order,
   * each one copyable.
   *
   * Why one panel rather than three actions: Steam asks for all three within a minute, and
   * every trip back to a menu is a trip away from the prompt the user is staring at.
   *
   * Ownership note: the caller starts the backend's sign-in assist before restarting Steam —
   * the anchor has to predate Steam sending the mail — and this component ends it on destroy.
   * That split is deliberate but easy to break: if this component stops being opened for
   * every begun assist, the anchor leaks until LoginWindow expires it.
   */
  import { onDestroy } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import {
    VAULT_FIELDS,
    endLoginAssist,
    guardCode,
    revealField,
    type VaultCode,
  } from "../../stores/vault";

  export let steamId64: string;
  export let accountName = "";
  export let hasPassword = false;

  /** Matches the reveal window in the login-details panel — long enough to type, short enough
   *  that a secret is not left on screen behind an app the user has walked away from. */
  const REVEAL_MS = 20_000;

  let password = "";
  let revealTimer: ReturnType<typeof setTimeout> | null = null;

  let code: VaultCode | null = null;
  let codeError = "";
  let codeLoading = false;
  let remaining = 0;
  let ticker: ReturnType<typeof setInterval> | null = null;

  function clearPassword(): void {
    password = "";
    if (revealTimer) {
      clearTimeout(revealTimer);
      revealTimer = null;
    }
  }

  function clearTicker(): void {
    if (ticker) {
      clearInterval(ticker);
      ticker = null;
    }
  }

  onDestroy(() => {
    clearPassword();
    clearTicker();
    // Closing the panel is the only signal the app gets that the sign-in is over — either it
    // worked or the user gave up. Both mean the anchor should stop widening and any inbox
    // connection still open behind it should be dropped.
    void endLoginAssist(steamId64);
  });

  async function copy(value: string): Promise<void> {
    if (!value) {
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      pushToast({ type: "success", message: $t("Toast_Copied"), duration: 2000 });
    } catch {
      // The value is on screen; a refused clipboard is not worth an error toast.
    }
  }

  async function revealPassword(): Promise<void> {
    clearPassword();
    try {
      password = await revealField(steamId64, VAULT_FIELDS.password);
      revealTimer = setTimeout(clearPassword, REVEAL_MS);
      await copy(password);
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_RevealFailed"), e),
        duration: 8000,
      });
    }
  }

  async function fetchCode(): Promise<void> {
    codeLoading = true;
    codeError = "";
    clearTicker();
    try {
      code = await guardCode(steamId64);
      remaining = code.remainingSeconds ?? 0;
      if (remaining > 0) {
        ticker = setInterval(() => {
          remaining = Math.max(0, remaining - 1);
        }, 1000);
      }
      // Copying straight away is the point: the user is at a prompt with a cursor in it.
      await copy(code.code);
    } catch (e) {
      codeError = formatToastWithError($t("Toast_Vault_CodeFailed"), e);
      code = null;
    } finally {
      codeLoading = false;
    }
  }
</script>

<div class="assist">
  <p class="intro">{$t("Vault_SignIn_Assist_Intro")}</p>

  <div class="row">
    <span class="row__label">{$t("Vault_SignIn_Assist_Login")}</span>
    <span class="row__value" class:row__value--empty={!accountName}>
      {accountName || $t("Vault_SignIn_Assist_NoLogin")}
    </span>
    <button type="button" class="ss-btn" disabled={!accountName} on:click={() => void copy(accountName)}>
      {$t("Vault_Action_Copy")}
    </button>
  </div>

  <div class="row">
    <span class="row__label">{$t("Vault_SignIn_Assist_Password")}</span>
    {#if !hasPassword}
      <span class="row__value row__value--empty">{$t("Vault_SignIn_Assist_NoPassword")}</span>
      <span></span>
    {:else if password}
      <span class="row__value row__value--secret">{password}</span>
      <button type="button" class="ss-btn" on:click={clearPassword}>{$t("Vault_Action_Hide")}</button>
    {:else}
      <span class="row__value row__value--masked">••••••••</span>
      <button type="button" class="ss-btn" on:click={() => void revealPassword()}>
        {$t("Vault_Action_Reveal")}
      </button>
    {/if}
  </div>

  <div class="row row--code">
    <span class="row__label">{$t("Vault_SignIn_Assist_Code")}</span>
    {#if codeLoading}
      <span class="row__value row__value--empty">{$t("Vault_Code_Fetching")}</span>
    {:else if code}
      <span class="row__value row__value--code">{code.code}</span>
    {:else}
      <span class="row__value row__value--empty">{$t("Vault_SignIn_Assist_CodeIdle")}</span>
    {/if}
    <button type="button" class="ss-btn" disabled={codeLoading} on:click={() => void fetchCode()}>
      {code ? $t("Vault_Code_Again") : $t("Vault_SignIn_Assist_GetCode")}
    </button>
  </div>

  {#if code}
    <p class="note">
      {code.method === "totp" ? $t("Vault_Code_FromAuthenticator") : $t("Vault_Code_FromInbox")}
      {#if remaining > 0}
        · {$t("Vault_Code_ExpiresIn", { seconds: String(remaining) })}
      {/if}
    </p>
  {:else if codeError}
    <p class="note note--error">{codeError}</p>
    <p class="note">{$t("Vault_Code_ManualFallback")}</p>
  {/if}
</div>

<style lang="scss">
  .assist {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    min-width: min(26rem, 80vw);
  }

  .intro {
    margin: 0 0 0.2rem;
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .row {
    display: grid;
    grid-template-columns: 5.5rem 1fr auto;
    align-items: center;
    gap: 0.6rem;
  }

  .row__label {
    font-size: var(--fs-meta);
    opacity: 0.8;
  }

  .row__value {
    font-family: monospace;
    overflow-wrap: anywhere;
    user-select: all;
  }

  .row__value--empty {
    font-family: inherit;
    opacity: 0.65;
    user-select: none;
  }

  .row__value--masked {
    letter-spacing: 0.15rem;
    opacity: 0.65;
    user-select: none;
  }

  .row__value--code {
    font-size: 1.6rem;
    letter-spacing: 0.2rem;
  }

  .note {
    margin: 0;
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .note--error {
    opacity: 1;
    color: var(--danger-text, inherit);
  }
</style>

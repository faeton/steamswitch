<script lang="ts">
  /**
   * The login debug panel (VAULT.md §6.3), Tier 2: what the stored refresh token says about
   * itself.
   *
   * Read-only, always. The one non-obvious thing on this screen is the audience: a
   * client-audience token signs in from any machine with no password and no Guard challenge,
   * and the ip_subject/ip_confirmer claims it carries are **not enforced by Steam**. That is
   * why the panel says so rather than just listing the claims.
   */
  import { onDestroy, onMount } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { VAULT_FIELDS, revealField, tokenDetails } from "../../stores/vault";
  import { clipboardClearEnabled, revealMs } from "../../stores/autoLock";
  import { CLIPBOARD_CLEAR_SECONDS, copySecret } from "../../lib/clipboardSecret";
  import type { TokenDetails } from "../../../bindings/steamswitch/internal/vault/models";

  export let steamId64: string;

  let details: TokenDetails | null = null;
  let rawToken = "";
  let revealTimer: ReturnType<typeof setTimeout> | null = null;

  onMount(async () => {
    try {
      details = await tokenDetails(steamId64);
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_LoadFailed"), e), duration: 8000 });
    }
  });

  function clearToken(): void {
    rawToken = "";
    if (revealTimer) {
      clearTimeout(revealTimer);
      revealTimer = null;
    }
  }

  onDestroy(clearToken);

  /**
   * Copies what is already on screen. Fetching it inside this click would not work: browsers only
   * allow clipboard writes during the click itself, not after waiting on the backend.
   */
  async function copyToken(): Promise<void> {
    const outcome = await copySecret(rawToken, {
      clearEnabled: clipboardClearEnabled,
      writeText: (v) => navigator.clipboard.writeText(v),
      readText: () => navigator.clipboard.readText(),
      setTimer: (fn, ms) => setTimeout(fn, ms),
    });
    if (outcome === "refused") {
      pushToast({ type: "info", message: $t("Toast_CopyRefused_SelectText"), duration: 5000 });
      return;
    }
    pushToast({
      type: "success",
      message:
        outcome === "copied-will-clear"
          ? $t("Toast_CopiedClearsIn", { seconds: CLIPBOARD_CLEAR_SECONDS })
          : $t("Toast_Copied"),
      duration: outcome === "copied-will-clear" ? 4000 : 2000,
    });
  }

  function when(iso: string | undefined): string {
    if (!iso) return "—";
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  async function revealToken(): Promise<void> {
    clearToken();
    try {
      rawToken = await revealField(steamId64, VAULT_FIELDS.refreshToken);
      revealTimer = setTimeout(clearToken, revealMs());
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_RevealFailed"), e), duration: 8000 });
    }
  }
</script>

<div class="details">
  {#if !details}
    <p class="small">{$t("Vault_Login_Loading")}</p>
  {:else if !details.present}
    <p class="small">{$t("Vault_Login_NoToken")}</p>
  {:else if details.unreadable}
    <p class="small">{$t("Vault_Login_TokenUnreadable")}</p>
  {:else}
    <header class="summary">
      <strong>{$t("Vault_Login_TokenStored")}</strong>
      {#if details.expired}
        <span class="flag">{$t("Vault_Login_Expired")}</span>
      {:else if details.daysUntilExpiry}
        <span class="small">{$t("Vault_Login_DaysLeft", { days: String(details.daysUntilExpiry) })}</span>
      {/if}
    </header>

    {#if details.isClientToken}
      <p class="callout">{$t("Vault_Login_ClientTokenWarning")}</p>
    {/if}

    {#if rawToken}
      <code class="token">{rawToken}</code>
      <div class="actions">
        <button class="ss-btn" type="button" on:click={copyToken}>{$t("Vault_Action_Copy")}</button>
        <button class="ss-btn" type="button" on:click={clearToken}>{$t("Vault_Action_Hide")}</button>
      </div>
    {:else}
      <div class="actions">
        <button class="ss-btn" type="button" on:click={revealToken}>{$t("Vault_Login_RevealToken")}</button>
      </div>
    {/if}

    <details class="technical">
      <summary>{$t("Vault_Login_Technical")}</summary>
      <dl>
        <dt>{$t("Vault_Login_Audience")}</dt>
        <dd>{details.audience}</dd>
        <dt>{$t("Vault_Login_Subject")}</dt>
        <dd>{details.subject || "—"}</dd>
        <dt>{$t("Vault_Login_Issuer")}</dt>
        <dd>{details.issuer || "—"}</dd>
        <dt>{$t("Vault_Login_IssuedAt")}</dt>
        <dd>{when(details.issuedAt)}</dd>
        <dt>{$t("Vault_Login_ExpiresAt")}</dt>
        <dd>{when(details.expiresAt)}</dd>
        <dt>{$t("Vault_Login_JTI")}</dt>
        <dd>{details.jti || "—"}</dd>
        <dt>{$t("Vault_Login_IPClaims")}</dt>
        <dd>{details.ipSubject || "—"} / {details.ipConfirmer || "—"}</dd>
        <dt>{$t("Vault_Login_GuardData")}</dt>
        <dd>{details.hasGuardData ? $t("Vault_Login_GuardDataStored") : "—"}</dd>
      </dl>
      <p class="small">{$t("Vault_Login_IPNotEnforced")}</p>
      <p class="small">{$t("Vault_Login_Tier3Note")}</p>
    </details>
  {/if}
</div>

<style lang="scss">
  .details {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    /* The frame stops at 75% of the window, with the title and Close button inside it. */
    max-height: calc(75vh - 180px);
    min-width: min(460px, 80vw);
    overflow-y: auto;
    text-align: left;
  }

  .summary {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .summary strong {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .flag {
    font-size: var(--fs-meta);
    font-weight: var(--fw-semibold);
    color: var(--fg-fail);
  }

  .small {
    margin: 0;
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .callout {
    margin: 0;
    padding: 10px var(--space-4);
    border: 1px solid var(--border-warn);
    border-radius: var(--radius-md);
    background: var(--bg-warn);
    color: var(--fg-warn);
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
  }

  .token {
    display: block;
    padding: var(--space-3);
    max-height: 6.5rem;
    overflow-y: auto;
    border: 1px solid var(--hairline-strong, var(--border-bar-bg));
    border-radius: var(--radius-md);
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-meta);
    word-break: break-all;
    /* The app turns text selection off by default, which made this impossible to copy by hand. */
    user-select: text;
    -webkit-user-select: text;
    color: var(--fg-primary);
  }

  .actions {
    display: flex;
    gap: var(--space-2);
  }

  .technical summary {
    cursor: pointer;
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-4);
    margin: var(--space-3) 0;
  }

  dt {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
  }

  dd {
    margin: 0;
    font-size: var(--fs-meta);
    color: var(--fg-primary);
    word-break: break-all;
  }

  .technical .small + .small {
    margin-top: var(--space-2);
  }
</style>

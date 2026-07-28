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
  import type { TokenDetails } from "../../../bindings/steamswitch/internal/vault/models";

  export let steamId64: string;

  let details: TokenDetails | null = null;
  let rawToken = "";
  let revealTimer: ReturnType<typeof setTimeout> | null = null;

  const REVEAL_MS = 20_000;

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

  async function revealToken(): Promise<void> {
    clearToken();
    try {
      rawToken = await revealField(steamId64, VAULT_FIELDS.refreshToken);
      revealTimer = setTimeout(clearToken, REVEAL_MS);
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
    <dl>
      <dt>{$t("Vault_Login_Audience")}</dt>
      <dd>{details.audience}</dd>
      <dt>{$t("Vault_Login_Subject")}</dt>
      <dd>{details.subject || "—"}</dd>
      <dt>{$t("Vault_Login_Issuer")}</dt>
      <dd>{details.issuer || "—"}</dd>
      <dt>{$t("Vault_Login_IssuedAt")}</dt>
      <dd>{details.issuedAt || "—"}</dd>
      <dt>{$t("Vault_Login_ExpiresAt")}</dt>
      <dd>
        {details.expiresAt || "—"}
        {#if details.expired}
          <span class="flag">{$t("Vault_Login_Expired")}</span>
        {:else if details.daysUntilExpiry}
          <span class="small">({$t("Vault_Login_DaysLeft", { days: String(details.daysUntilExpiry) })})</span>
        {/if}
      </dd>
      <dt>{$t("Vault_Login_JTI")}</dt>
      <dd>{details.jti || "—"}</dd>
      <dt>{$t("Vault_Login_IPClaims")}</dt>
      <dd>{details.ipSubject || "—"} / {details.ipConfirmer || "—"}</dd>
      <dt>{$t("Vault_Login_GuardData")}</dt>
      <dd>{details.hasGuardData ? $t("Vault_Login_GuardDataStored") : "—"}</dd>
    </dl>

    {#if details.isClientToken}
      <p class="callout">{$t("Vault_Login_ClientTokenWarning")}</p>
    {/if}
    <p class="small">{$t("Vault_Login_IPNotEnforced")}</p>

    <div class="actions">
      {#if rawToken}
        <code>{rawToken}</code>
        <button type="button" on:click={clearToken}>{$t("Vault_Action_Hide")}</button>
      {:else}
        <button type="button" on:click={revealToken}>{$t("Vault_Login_RevealToken")}</button>
      {/if}
    </div>
  {/if}

  <p class="small footnote">{$t("Vault_Login_Tier3Note")}</p>
</div>

<style lang="scss">
  .details {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-height: 60vh;
    overflow-y: auto;
  }

  dl {
    display: grid;
    grid-template-columns: minmax(7rem, auto) 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
  }

  dt {
    font-size: var(--fs-meta);
    opacity: 0.8;
  }

  dd {
    margin: 0;
    font-size: var(--fs-secondary);
    word-break: break-all;
  }

  .callout {
    margin: 0;
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--border-bar-bg);
    border-radius: 4px;
    font-size: var(--fs-secondary);
  }

  .flag {
    font-size: var(--fs-meta);
    text-transform: uppercase;
  }

  .small {
    font-size: var(--fs-meta);
    opacity: 0.85;
    margin: 0;
  }

  .footnote {
    border-top: 1px solid var(--border-bar-bg);
    padding-top: 0.4rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
  }

  code {
    font-family: monospace;
    font-size: var(--fs-meta);
    word-break: break-all;
    flex: 1 1 12rem;
  }
</style>

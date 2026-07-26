<script lang="ts">
  /**
   * Every vault entry at once (VAULT.md §5.4).
   *
   * This is the only place standalone entries — accounts stored and checked here but never
   * signed into on this machine — are visible, since by definition they have no tile.
   *
   * Summaries only. Every column below is a presence flag or a verdict; no secret reaches
   * this component, which is what makes a screenshot of it harmless.
   */
  import { t } from "../../stores/i18n";
  import { verdictLabelKey } from "../../lib/vault/health";
  import { vaultEntries, vaultHealth } from "../../stores/vault";

  function verdictOf(steamId64: string, fallback: string | undefined): string {
    return $vaultHealth[steamId64]?.verdict ?? fallback ?? "unknown";
  }

  function statusClass(v: string): string {
    return v === "ok" || v === "warn" || v === "fail" ? v : "unknown";
  }

  function labelOf(e: { label?: string; accountName?: string; steamId64: string }): string {
    return e.label?.trim() || e.accountName?.trim() || e.steamId64;
  }
</script>

<div class="overview">
  {#if $vaultEntries.length === 0}
    <p class="small">{$t("Vault_Overview_Empty")}</p>
  {:else}
    <ul>
      {#each $vaultEntries as e (e.steamId64)}
        {@const verdict = verdictOf(e.steamId64, e.health?.verdict)}
        <li>
          <span class="dot {statusClass(verdict)}" aria-hidden="true"></span>
          <span class="name">
            {labelOf(e)}
            {#if e.standalone}
              <span class="tag">{$t("Vault_Overview_Standalone")}</span>
            {/if}
          </span>
          <span class="verdict small">{$t(verdictLabelKey(verdict))}</span>
          <span class="flags small">
            {#if e.hasPassword}<span title={$t("Vault_Field_Password")}>🔑</span>{/if}
            {#if e.hasSharedSecret}<span title={$t("Vault_Field_SharedSecret")}>⏱</span>{/if}
            {#if e.emailSource !== "none"}<span title={$t("Vault_Field_EmailAddress")}>✉</span>{/if}
            {#if e.hasRefreshToken}<span title={$t("Vault_Login_GuardData")}>🎫</span>{/if}
          </span>
        </li>
      {/each}
    </ul>
    <p class="small footnote">{$t("Vault_Overview_Hint")}</p>
  {/if}
</div>

<style lang="scss">
  .overview {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-height: 60vh;
    overflow-y: auto;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }

  li {
    display: grid;
    grid-template-columns: auto 1fr auto auto;
    gap: 0.5rem;
    align-items: center;
  }

  .dot {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    background: var(--border-bar-bg);
  }

  .dot.ok {
    background: #3fa45b;
  }

  .dot.warn {
    background: #d19a20;
  }

  .dot.fail {
    background: #c0392b;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    font-size: 0.7rem;
    text-transform: uppercase;
    opacity: 0.75;
    margin-left: 0.3rem;
  }

  .flags {
    letter-spacing: 0.15rem;
  }

  .small {
    font-size: 0.8rem;
    opacity: 0.85;
    margin: 0;
  }

  .footnote {
    border-top: 1px solid var(--border-bar-bg);
    padding-top: 0.4rem;
  }
</style>

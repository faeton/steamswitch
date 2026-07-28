<script lang="ts">
  /**
   * The vault roster (REDESIGN_BRIEF.md A6 "Vault page").
   *
   * Default columns are identity, health, secret presence and tags — and nothing else.
   * Provenance (source / acquired-at / note) is deliberately *not* here: the brief puts it in
   * the editor until it proves it earns a column, and four columns is what stays scannable at
   * the 1000px minimum window.
   *
   * The "secrets" column shows which of the four kinds of material is stored, never the
   * material itself. These come from the Go side's redacted `Summary`, which carries
   * "hasPassword", never a password.
   */
  import { createEventDispatcher } from "svelte";
  import { t } from "../../stores/i18n";
  import HealthBadge from "./HealthBadge.svelte";
  import { hasActiveFilters, type HealthFilter, type VaultFilters } from "../../lib/vault/filter";
  import type { Summary } from "../../../bindings/steamswitch/internal/vault/models";

  export let entries: Summary[];
  export let total: number;
  export let filters: VaultFilters;
  /** SteamID64 currently open in the editor, "" for a blank new entry, null for none. */
  export let selected: string | null = null;

  const dispatch = createEventDispatcher<{
    open: string;
    check: string;
    export: string;
    delete: Summary;
    signIn: Summary;
  }>();

  const HEALTH_FILTERS: { id: HealthFilter; labelKey: string }[] = [
    { id: "any", labelKey: "Vault_Filter_HealthAny" },
    { id: "attention", labelKey: "Vault_Filter_HealthAttention" },
    { id: "ok", labelKey: "Vault_Filter_HealthOK" },
    { id: "unchecked", labelKey: "Vault_Filter_HealthUnchecked" },
  ];

  function labelOf(entry: Summary): string {
    return entry.label?.trim() || entry.accountName?.trim() || entry.steamId64;
  }

  function cycleHealth(): void {
    const i = HEALTH_FILTERS.findIndex((f) => f.id === filters.health);
    filters = { ...filters, health: HEALTH_FILTERS[(i + 1) % HEALTH_FILTERS.length].id };
  }

  function clearFilters(): void {
    filters = { query: "", health: "any", hasPassword: false, onSwitcher: false };
  }

  $: healthLabelKey = HEALTH_FILTERS.find((f) => f.id === filters.health)?.labelKey ?? HEALTH_FILTERS[0].labelKey;
</script>

<div class="table">
  <div class="table__filters">
    <input
      type="search"
      class="ss-field table__search"
      placeholder={$t("Vault_Filter_SearchPlaceholder")}
      aria-label={$t("Vault_Filter_SearchPlaceholder")}
      bind:value={filters.query}
    />
    <button
      type="button"
      class="ss-chip"
      aria-pressed={filters.health !== "any"}
      on:click={cycleHealth}>{$t(healthLabelKey)}</button
    >
    <button
      type="button"
      class="ss-chip"
      aria-pressed={filters.hasPassword}
      on:click={() => (filters = { ...filters, hasPassword: !filters.hasPassword })}
      >{$t("Vault_Filter_HasPassword")}</button
    >
    <button
      type="button"
      class="ss-chip"
      aria-pressed={filters.onSwitcher}
      on:click={() => (filters = { ...filters, onSwitcher: !filters.onSwitcher })}
      >{$t("Vault_Filter_OnSwitcher")}</button
    >
    <span class="table__count meta-mono">
      {entries.length === total
        ? $t("Vault_Filter_Count", { count: total })
        : $t("Vault_Filter_CountFiltered", { shown: entries.length, total })}
    </span>
  </div>

  <div class="table__scroll">
    <div class="table__head" role="row">
      <span role="columnheader">{$t("Vault_Col_Account")}</span>
      <span role="columnheader">{$t("Vault_Col_Health")}</span>
      <span role="columnheader">{$t("Vault_Col_Secrets")}</span>
      <span role="columnheader">{$t("Vault_Col_Actions")}</span>
    </div>

    {#if entries.length === 0}
      <div class="table__none">
        <p class="ss-help">{$t("Vault_Filter_NoMatches")}</p>
        {#if hasActiveFilters(filters)}
          <button type="button" class="ss-btn" on:click={clearFilters}>
            {$t("Vault_Filter_Clear")}
          </button>
        {/if}
      </div>
    {:else}
      <ul class="table__rows">
        {#each entries as entry (entry.steamId64)}
          <li>
            <div class="row" class:row--selected={selected === entry.steamId64}>
              <button
                type="button"
                class="ss-row-button ss-row-button--stacked row__identity"
                aria-label={$t("Vault_Action_Open", { name: labelOf(entry) })}
                on:click={() => dispatch("open", entry.steamId64)}
              >
                <span class="row__name">
                  {labelOf(entry)}
                  {#if entry.standalone}
                    <!-- Says out loud that this account has no home tile, so "why isn't it on
                         the switcher" never becomes a mystery. -->
                    <span class="row__vault-only">{$t("Vault_Badge_VaultOnly")}</span>
                  {/if}
                </span>
                <span class="row__login meta-mono">{entry.accountName || entry.steamId64}</span>
              </button>

              <div class="row__health">
                <HealthBadge report={entry.health} showAction />
              </div>

              <div class="row__secrets" aria-label={$t("Vault_Col_Secrets")}>
                <span class="chip" class:chip--on={entry.hasPassword} title={$t("Vault_Secret_Password")}
                  >{$t("Vault_Secret_Password_Short")}</span
                >
                <span class="chip" class:chip--on={entry.hasSharedSecret} title={$t("Vault_Secret_TOTP")}
                  >{$t("Vault_Secret_TOTP_Short")}</span
                >
                <span class="chip" class:chip--on={entry.hasRefreshToken} title={$t("Vault_Secret_Token")}
                  >{$t("Vault_Secret_Token_Short")}</span
                >
                <span class="chip" class:chip--on={entry.hasEmailAuth} title={$t("Vault_Secret_Email")}
                  >{$t("Vault_Secret_Email_Short")}</span
                >
              </div>

              <div class="row__actions">
                {#if entry.standalone}
                  <!--
                    The only route from a vault-only entry onto the switcher.

                    Steam's grid is Steam's own loginusers.vdf — this app cannot add a row to
                    it for an account Steam has never signed into here, and faking one would
                    put a tile on screen that fails when clicked. So the honest action is to
                    take the user to Steam's login screen; the account joins the grid once
                    Steam has actually signed into it.
                  -->
                  <button
                    type="button"
                    class="ss-btn row__btn"
                    on:click={() => dispatch("signIn", entry)}
                  >
                    {$t("Vault_Action_SignInHere")}
                  </button>
                {/if}
                <button type="button" class="ss-btn row__btn" on:click={() => dispatch("check", entry.steamId64)}>
                  {$t("Vault_Action_Check")}
                </button>
                <button type="button" class="ss-btn row__btn" on:click={() => dispatch("export", entry.steamId64)}>
                  {$t("Vault_Action_Export")}
                </button>
                <button
                  type="button"
                  class="ss-btn ss-btn--danger row__btn"
                  on:click={() => dispatch("delete", entry)}
                >
                  {$t("Vault_Action_Delete")}
                </button>
              </div>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<style>
  .table {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .table__filters {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .table__search {
    width: 260px;
    flex: 0 1 260px;
  }

  .table__count {
    color: var(--fg-muted);
    margin-left: auto;
  }

  .table__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
  }

  /* One grid template, declared once and shared by the header and every row, so a column can
     never drift out of alignment with its heading. */
  .table__head,
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1.6fr) minmax(0, 1.1fr) minmax(0, 0.9fr) auto;
    gap: var(--space-4);
    align-items: center;
  }

  .table__head {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 10px var(--space-4);
    background: var(--surface-chrome, var(--code));
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-meta);
    letter-spacing: var(--ls-caps);
    text-transform: uppercase;
    color: var(--fg-muted);
  }

  .table__rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .table__rows > li + li .row {
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .row {
    padding: var(--space-4);
  }

  .row--selected {
    background: var(--accent-fill-very-soft, var(--button-bg));
  }

  .row__identity {
    display: flex;
    gap: 2px;
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .row__identity:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
    border-radius: var(--radius-xs);
  }

  .row__name {
    /* Overrides the inherited `button span { line-height: 2.5em }` — see tokens.scss. */
    line-height: var(--lh-tight);
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row__vault-only {
    font-size: var(--fs-meta);
    font-weight: var(--fw-normal);
    color: var(--fg-muted);
  }

  .row__login {
    line-height: var(--lh-tight);
    color: var(--fg-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row__health,
  .row__secrets {
    min-width: 0;
  }

  .row__secrets {
    display: flex;
    gap: var(--space-1);
    flex-wrap: wrap;
  }

  /*
    Present *and* absent are both drawn, rather than only listing what is stored: "this
    account has no TOTP seed" is the answer to a question people actually ask, and an absent
    chip would be indistinguishable from a column that failed to render.
  */
  .chip {
    padding: 2px 5px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-xs);
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-meta);
    line-height: 1.3;
    color: var(--fg-disabled);
  }

  .chip--on {
    border-color: var(--accent-overlay-border, var(--accent));
    background: var(--accent-fill-very-soft, transparent);
    color: var(--accent-text-bright, var(--accent));
  }

  .row__actions {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
  }

  .row__btn {
    min-height: 28px;
    padding: 0 10px;
    font-size: var(--fs-meta);
  }

  .table__none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-7) var(--space-5);
    text-align: center;
  }

  /* Below the doc's 1000px minimum the four columns stop being scannable; the actions drop
     under the identity rather than squeezing every column to nothing. */
  /* +190px nav +420px editor: the table's real width is far less than the viewport's. */
  @media (max-width: 1300px) {
    .table__head {
      display: none;
    }
    .row {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--space-2);
    }
    .row__actions {
      justify-content: flex-start;
    }
  }
</style>

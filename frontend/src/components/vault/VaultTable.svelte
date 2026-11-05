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
  import Icon from "../Icon.svelte";
  import { banSignal, limitedSignal } from "../../lib/vault/health";
  import { canCheckEmail, emailCheckView, type EmailCheckView } from "../../lib/vault/emailCheck";
  import { openContextMenuAtRect } from "../../stores/contextMenu";
  import {
    hasActiveFilters,
    isOnThisMachine,
    type HealthFilter,
    type VaultFilters,
  } from "../../lib/vault/filter";
  import { switcherRoster } from "../../stores/switcherRoster";
  import type { Summary } from "../../../bindings/steamswitch/internal/vault/models";

  export let entries: Summary[];
  export let total: number;
  export let filters: VaultFilters;
  /** SteamID64 currently open in the editor, "" for a blank new entry, null for none. */
  export let selected: string | null = null;
  /** SteamID64 whose mailbox is being signed in to right now, or null. */
  export let checkingEmail: string | null = null;
  /** Accounts a Steam check is running for, so the row can say so. */
  export let checking: ReadonlySet<string> = new Set();

  const dispatch = createEventDispatcher<{
    open: string;
    health: string;
    recheck: Summary;
    loginDetails: string;
    checkEmail: string;
    export: string;
    delete: Summary;
    signIn: Summary;
    credentials: Summary;
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

  function limitedParams(entry: Summary): Record<string, string> {
    return (limitedSignal(entry.health)?.params ?? {}) as Record<string, string>;
  }

  /** Ban counts for the "Banned" flag's tooltip; the Go side always sets all three. */
  function banParams(entry: Summary): Record<string, string> {
    return (banSignal(entry.health)?.params ?? {}) as Record<string, string>;
  }

  function emailView(entry: Summary): EmailCheckView | null {
    return canCheckEmail(entry.emailSource) ? emailCheckView(entry.emailCheck) : null;
  }

  function cycleHealth(): void {
    const i = HEALTH_FILTERS.findIndex((f) => f.id === filters.health);
    filters = { ...filters, health: HEALTH_FILTERS[(i + 1) % HEALTH_FILTERS.length].id };
  }

  function clearFilters(): void {
    filters = { query: "", health: "any", hasPassword: false, onSwitcher: false };
  }

  /**
   * Check, Export and Delete live behind ⋯. Five bordered buttons on every row made the
   * actions the loudest thing in the list and pushed each entry to three lines; the two
   * that stay visible are the ones people come to the vault for.
   */
  function openRowMenu(e: MouseEvent, entry: Summary): void {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openContextMenuAtRect(
      { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height },
      [
        { label: $t("Vault_Menu_Health"), action: () => dispatch("health", entry.steamId64) },
        { label: $t("Vault_Action_Recheck"), action: () => dispatch("recheck", entry) },
        { label: $t("Vault_Menu_LoginDetails"), action: () => dispatch("loginDetails", entry.steamId64) },
        ...(canCheckEmail(entry.emailSource)
          ? [{ label: $t("Vault_Action_CheckEmail"), action: () => dispatch("checkEmail", entry.steamId64) }]
          : []),
        ...(entry.pending
          ? []
          : [{ label: $t("Vault_Action_Export"), action: () => dispatch("export", entry.steamId64) }]),
        { type: "separator" },
        { label: $t("Vault_Action_Delete"), danger: true, action: () => dispatch("delete", entry) },
      ],
    );
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
      <span role="columnheader"><span class="sr-only">{$t("Vault_Col_Actions")}</span></span>
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
                  {#if !isOnThisMachine(entry, $switcherRoster)}
                    <!-- Says out loud that this account has no home tile, so "why isn't it on
                         the switcher" never becomes a mystery. -->
                    <span class="row__vault-only">{$t("Vault_Badge_VaultOnly")}</span>
                  {/if}
                </span>
                <span class="row__login meta-mono">
                  {entry.accountName || entry.steamId64}{#if entry.emailAddress}
                    · {entry.emailAddress}{/if}
                </span>
              </button>

              <div class="row__health">
                <button
                  type="button"
                  class="row__health-open"
                  title={$t("Vault_Menu_Health")}
                  on:click={() => dispatch("health", entry.steamId64)}
                >
                  <HealthBadge report={entry.health} checking={checking.has(entry.steamId64)} />
                </button>
                {#if banSignal(entry.health)}
                  <span
                    class="flag"
                    data-tone="fail"
                    title={$t(banSignal(entry.health)?.detail ?? "Vault_Signal_Banned", banParams(entry))}>{$t("Badge_Banned")}</span
                  >
                {/if}
                {#if limitedSignal(entry.health)}
                  <span
                    class="flag"
                    data-tone="info"
                    title={$t(limitedSignal(entry.health)?.detail ?? "", limitedParams(entry))}
                    >{$t("Vault_Flag_Limited")}</span
                  >
                {/if}
                {#if checkingEmail === entry.steamId64}
                  <span class="flag" data-tone="busy">{$t("Vault_Verdict_Checking")}</span>
                {:else if emailView(entry)}
                  {@const view = emailView(entry)}
                  <span
                    class="flag"
                    data-tone={view?.tone}
                    title={view ? $t(view.detailKey, view.params) : ""}
                    >{view ? $t(view.flagKey, view.params) : ""}</span
                  >
                {/if}
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
                {#if !isOnThisMachine(entry, $switcherRoster)}
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
                <!--
                  Username, password and Guard code in one panel, for typing into Steam's own
                  login screen. On every row, not only standalone ones: an account can already
                  have a tile and still be the one Steam is sitting in front of asking for a
                  code, and the alternative is opening a browser and logging into an inbox.
                -->
                <button
                  type="button"
                  class="ss-btn row__btn"
                  on:click={() => dispatch("credentials", entry)}
                >
                  {$t("Vault_Action_Credentials")}
                </button>
                <button
                  type="button"
                  class="row__menu"
                  aria-haspopup="menu"
                  aria-label={$t("Aria_AccountMenu", { name: labelOf(entry) })}
                  title={$t("Aria_AccountMenu", { name: labelOf(entry) })}
                  on:click={(e) => openRowMenu(e, entry)}
                >
                  <Icon name="more" />
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
    /* The layout answers to the table's own width, not the window's: the editor panel can
       open beside it, and a viewport breakpoint had to guess at that (it guessed 1300px, so
       the default 1280px window always got the stacked fallback). */
    container-type: inline-size;
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
    font-variant-numeric: tabular-nums;
  }

  /*
    One grid for the whole table. The header, the list and every row are subgrids of it, so a
    column is as wide as its widest cell in *any* row — the way a real table behaves — while
    each row stays its own focusable, hoverable element. With a per-row grid, an `auto` column
    sized itself row by row and the vault-only row (one extra button) knocked every column
    out of line with its heading.

    The explicit template is repeated on the children before `subgrid`, so an engine without
    subgrid (WebKitGTK before 2.40) still gets aligned fixed columns rather than one stacked
    column.
  */
  .table__scroll {
    --cols: minmax(160px, 1.6fr) minmax(130px, 1fr) auto auto;
    display: grid;
    grid-template-columns: var(--cols);
    align-content: start;
    column-gap: var(--space-5);
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
  .table__rows,
  .table__rows > li,
  .row {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: minmax(160px, 1.6fr) minmax(130px, 1fr) 176px 260px;
    grid-template-columns: subgrid;
    align-items: center;
  }

  .table__none {
    grid-column: 1 / -1;
  }

  .table__head {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 8px var(--space-4);
    background: var(--surface-panel, var(--mainContentBackground));
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
    font-size: var(--fs-meta);
    font-weight: var(--fw-medium);
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
    min-height: 52px;
    padding: 6px var(--space-3) 6px var(--space-4);
    box-sizing: border-box;
  }

  .row:hover {
    background: var(--row-hover);
  }

  .row--selected,
  .row--selected:hover {
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
    line-height: var(--lh-tight);
    font-size: var(--fs-body);
    font-weight: var(--fw-medium);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .row__vault-only {
    margin-left: var(--space-2);
    line-height: var(--lh-tight);
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
    max-width: 100%;
  }

  .row__health {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    min-width: 0;
  }

  .row__health-open {
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
    text-align: left;
  }

  .row__health-open:hover :global(.health__label) {
    text-decoration: underline;
  }

  .row__secrets {
    min-width: 0;
    display: flex;
    gap: var(--space-1);
    flex-wrap: nowrap;
  }

  /* The same flag as the switcher row's "VAC ban" / "Limited". */
  .flag {
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 var(--space-2);
    border: 1px solid var(--border-fail);
    border-radius: var(--radius-xs);
    background: var(--bg-fail);
    color: var(--fg-fail);
    font-size: var(--fs-meta);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }

  .flag[data-tone="ok"] {
    border-color: var(--hairline-strong, var(--button-bg));
    background: transparent;
    color: var(--fg-ok);
  }

  .flag[data-tone="warn"] {
    border-color: var(--hairline-strong, var(--button-bg));
    background: transparent;
    color: var(--fg-warn);
  }

  .flag[data-tone="info"],
  .flag[data-tone="busy"] {
    border-color: var(--hairline-strong, var(--button-bg));
    background: transparent;
    color: var(--fg-muted);
  }

  /*
    Present *and* absent are both drawn, rather than only listing what is stored: "this
    account has no TOTP seed" is the answer to a question people actually ask, and an absent
    chip would be indistinguishable from a column that failed to render. Absent is dashed, so
    the difference survives a theme that flattens the accent.
  */
  .chip {
    padding: 1px 5px;
    border: 1px dashed var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-xs);
    font-size: var(--fs-meta);
    line-height: 1.35;
    color: var(--fg-disabled);
    white-space: nowrap;
  }

  .chip--on {
    border-style: solid;
    border-color: var(--accent-overlay-border, var(--accent));
    background: var(--accent-fill-very-soft, transparent);
    color: var(--accent-text-bright, var(--accent));
  }

  .row__actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    justify-content: flex-end;
  }

  .row__btn {
    min-height: 28px;
    padding: 0 10px;
    font-size: var(--fs-meta);
  }

  .row__menu {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    margin: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .row__menu:hover {
    background: var(--row-hover-strong);
    color: var(--fg-primary);
  }

  .row__menu:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 0;
  }

  .table__none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-7) var(--space-5);
    text-align: center;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  /* With the editor open, or a narrow window: secrets give way first (the editor shows them),
     then the row stacks with its actions under the identity. */
  @container (max-width: 760px) {
    .table__scroll {
      --cols: minmax(0, 1fr) auto auto;
      grid-template-columns: var(--cols);
    }
    .row__secrets,
    .table__head > :nth-child(3) {
      display: none;
    }
  }

  @container (max-width: 520px) {
    .table__head {
      display: none;
    }
    .row {
      grid-template-columns: minmax(0, 1fr) auto;
      row-gap: var(--space-2);
    }
    .row__actions {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }
  }

  @media (forced-colors: active) {
    .row:hover,
    .row--selected {
      outline: 1px solid CanvasText;
      outline-offset: -1px;
    }
  }
</style>

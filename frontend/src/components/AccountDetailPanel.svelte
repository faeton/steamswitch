<script lang="ts">
  /**
   * Account preview, as a panel you open rather than a card that ambushes you
   * (REDESIGN_BRIEF.md A6 "Account detail / preview", A14).
   *
   * What this replaces: hovering an avatar injected Steam's own miniprofile markup — a ~328px
   * card with an 80px avatar and a 96px frame, styled by `miniprofile.scss` — into a list
   * whose rows are 42px tall (brief Part B #4). It was a carried-over Steam blob, not a
   * preview, and on a dense Windows list it fired constantly while the pointer crossed rows.
   *
   * The brief's recommendation, confirmed here, is click→detail. The miniprofile is not
   * thrown away: it is *this* surface's content, at a size it was designed for.
   */
  import { createEventDispatcher } from "svelte";
  import { t, locale } from "../stores/i18n";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import AccountTagBubbles from "./AccountTagBubbles.svelte";
  import HealthBadge from "./vault/HealthBadge.svelte";
  import { formatLastLoginForLocale } from "../lib/formatLastLogin";
  import { sanitizeHtml } from "../lib/sanitizeHtml";
  import { offlineMode } from "../stores/offlineMode";
  import { vaultEntries, vaultHealth } from "../stores/vault";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { AccountRole } from "../lib/steam/accountRoles";

  export let account: SteamAccountRow;
  export let role: AccountRole = "plain";
  export let current = false;
  export let avatarEpoch = 0;
  export let showHealth = false;
  export let switchDisabled = false;

  const dispatch = createEventDispatcher<{
    close: void;
    switch: string;
    editVault: string;
    menu: { id: string; x: number; y: number };
  }>();

  $: id = account.steamId64;
  $: label = account.displayName?.trim() || account.personaName?.trim() || id;
  $: lastLogin = account.lastLogin ? formatLastLoginForLocale(account.lastLogin, $locale) : "";
  $: hasVaultEntry = $vaultEntries.some((e) => e.steamId64 === id);
  // Only rendered when Steam actually supplied it and offline mode is not blocking its
  // images; the markup is Steam's, so it is sanitised before it goes anywhere near the DOM.
  $: miniProfile =
    !$offlineMode && account.showMiniProfile && (account.miniProfileHtml ?? "").trim() !== ""
      ? sanitizeHtml(account.miniProfileHtml ?? "", "miniProfile")
      : "";

  function onMenu(e: MouseEvent): void {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    dispatch("menu", { id, x: r.left, y: r.bottom + 4 });
  }

</script>

<aside class="detail" aria-label={label}>
  <header class="detail__head">
    <div class="detail__identity">
      <SteamAccountAvatar {account} epoch={avatarEpoch} fallback="/img/BasicDefault.webp" />
      <div class="detail__names">
        <div class="detail__name" title={label}>{label}</div>
        {#if account.accountName}
          <div class="detail__login meta-mono">{account.accountName}</div>
        {/if}
      </div>
    </div>
    <button type="button" class="ss-btn ss-btn--quiet detail__close" aria-label={$t("Button_Close")} on:click={() => dispatch("close")}>✕</button>
  </header>

  <div class="detail__scroll">
    <dl class="facts">
      <dt>{$t("Steam_ShowSteamID")}</dt>
      <dd class="meta-mono">{id}</dd>
      {#if lastLogin}
        <dt>{$t("Steam_ShowLastLogin")}</dt>
        <dd>{lastLogin}</dd>
      {/if}
      {#if current}
        <dt>{$t("Detail_Field_State")}</dt>
        <dd>{$t("Detail_State_Current")}</dd>
      {/if}
      {#if role === "home"}
        <dt>{$t("Detail_Field_Role")}</dt>
        <dd>{$t("Badge_Home")}</dd>
      {:else if role === "shared"}
        <dt>{$t("Detail_Field_Role")}</dt>
        <dd>{$t("Badge_Shared")}</dd>
      {/if}
    </dl>

    {#if showHealth && hasVaultEntry}
      <section class="block">
        <h3 class="ss-eyebrow">{$t("Vault_Col_Health")}</h3>
        <HealthBadge report={$vaultHealth[id]} showAction />
      </section>
    {/if}

    {#if account.tags?.length}
      <section class="block">
        <h3 class="ss-eyebrow">{$t("Detail_Field_Tags")}</h3>
        <AccountTagBubbles tags={account.tags} maxVisible={12} />
      </section>
    {/if}

    {#if miniProfile}
      <section class="block">
        <h3 class="ss-eyebrow">{$t("Detail_Field_Profile")}</h3>
        <!-- Steam's own markup, at the size it was drawn for. Sanitised above; the scale
             wrapper keeps a 328px card inside a narrower panel without clipping it. -->
        <div class="miniprofile">
          <div class="miniprofile__inner">{@html miniProfile}</div>
        </div>
      </section>
    {/if}
  </div>

  <footer class="detail__foot">
    <button
      type="button"
      class="ss-btn ss-btn--primary"
      disabled={current || switchDisabled}
      on:click={() => dispatch("switch", id)}
    >
      {current ? $t("Detail_State_Current") : $t("Detail_Action_SwitchTo", { name: label })}
    </button>
    {#if hasVaultEntry}
      <button type="button" class="ss-btn" on:click={() => dispatch("editVault", id)}>
        {$t("Detail_Action_EditVault")}
      </button>
    {/if}
    <button type="button" class="ss-btn" on:click={onMenu}>{$t("Detail_Action_More")}</button>
  </footer>
</aside>

<style>
  .detail {
    --avatar-size: 52px;
    flex: 0 0 auto;
    width: 340px;
    max-width: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
    border: 1px solid var(--hairline-strong, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel-raised, var(--mainContentBackground));
    overflow: hidden;
  }

  .detail__head {
    flex: 0 0 auto;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-4) var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .detail__identity {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-width: 0;
  }

  .detail__names {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .detail__name {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail__login {
    color: var(--fg-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail__close {
    flex: 0 0 auto;
    min-height: 26px;
    padding: 0 var(--space-2);
  }

  .detail__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .facts {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--space-2) var(--space-4);
    font-size: var(--fs-secondary);
  }

  .facts dt {
    color: var(--fg-muted);
  }

  .facts dd {
    margin: 0;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .miniprofile {
    overflow: hidden;
    border-radius: var(--radius-md);
  }

  .miniprofile__inner {
    /* Steam's card is a fixed 328px; the panel is narrower, so it scales rather than clips. */
    width: 328px;
    transform: scale(0.88);
    transform-origin: top left;
  }

  .detail__foot {
    flex: 0 0 auto;
    display: flex;
    gap: var(--space-2);
    padding: var(--space-4) var(--space-5);
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .detail__foot :global(.ss-btn:first-child) {
    flex: 1 1 auto;
    min-width: 0;
  }
</style>

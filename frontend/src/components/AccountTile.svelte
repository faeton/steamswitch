<script lang="ts">
  /**
   * One account row in the compact list (REDESIGN.md §3).
   *
   * A single click switches — there is no select-then-act step, because at three or four
   * accounts selection buys nothing. Right-click (or the ⋯ button) opens the account menu.
   * At most two badges, and the Home accent ring is never the only way to tell Home apart:
   * the badge text says so too.
   */
  import { createEventDispatcher } from "svelte";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import AccountTagBubbles from "./AccountTagBubbles.svelte";
  import { t, locale } from "../stores/i18n";
  import { formatLastLoginForLocale } from "../lib/formatLastLogin";
  import { accountMetaLine, accountNotePreview } from "../lib/steam/accountMetaLine";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { AccountRole } from "../lib/steam/accountRoles";
  import { vaultHealth } from "../stores/vault";
  import { tileDot } from "../lib/vault/health";

  export let account: SteamAccountRow;
  export let role: AccountRole = "plain";
  export let kitTravels = false;
  /** Steam is currently set to log in as this account. */
  export let current = false;
  /** Disabled while a switch is running or a recovery is blocking. */
  export let disabled = false;
  /** 1-based position, rendered as the `1–4` keyboard hint. */
  export let index = 0;
  export let avatarEpoch = 0;
  export let boundary: HTMLElement | null = null;

  const dispatch = createEventDispatcher<{
    switch: string;
    menu: { id: string; x: number; y: number };
  }>();

  const PROFILE_PLACEHOLDER = "/img/BasicDefault.webp";

  $: id = account.steamId64;
  $: label = account.displayName?.trim() || account.personaName?.trim() || id;
  $: meta = accountMetaLine(account, (raw) => formatLastLoginForLocale(raw, $locale));
  $: notePreview = accountNotePreview(account);
  $: healthDot = tileDot($vaultHealth[id]);

  function activate(): void {
    if (disabled || current) {
      return;
    }
    dispatch("switch", id);
  }

  function openMenu(x: number, y: number): void {
    dispatch("menu", { id, x, y });
  }

  function onContextMenu(e: MouseEvent): void {
    e.preventDefault();
    openMenu(e.clientX, e.clientY);
  }

  function onMenuButton(e: MouseEvent): void {
    e.stopPropagation();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(r.left, r.bottom + 4);
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      activate();
      return;
    }
    // The keyboard equivalent of right-click, for users who never reach for a mouse.
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
      openMenu(r.left + 16, r.bottom - 8);
    }
  }
</script>

<div
  class="tile"
  class:tile--home={role === "home"}
  class:tile--current={current}
  class:tile--disabled={disabled}
  role="button"
  tabindex={disabled ? -1 : 0}
  aria-disabled={disabled}
  aria-current={current ? "true" : undefined}
  on:click={activate}
  on:keydown={onKeydown}
  on:contextmenu={onContextMenu}
>
  <div class="tile__avatar">
    <SteamAccountAvatar
      {account}
      epoch={avatarEpoch}
      fallback={PROFILE_PLACEHOLDER}
      {boundary}
    />
  </div>

  <div class="tile__body">
    <div class="tile__name" title={label}>{label}</div>
    {#if meta}
      <div class="tile__sub meta-mono" title={meta}>{meta}</div>
    {/if}
    {#if notePreview}
      <div class="tile__note" title={notePreview}>{notePreview}</div>
    {/if}
    <!-- Tags get their own row rather than joining the badge cluster: the two-badge cap in
         REDESIGN §3 is about the fixed role markers, and a user can define any number of
         tags. Capped at three plus a "+N" so one heavily tagged account cannot grow to
         several times the height of its neighbours. A tile with no tags renders nothing and
         stays the original height. -->
    {#if account.tags?.length}
      <div class="tile__tags"><AccountTagBubbles tags={account.tags} maxVisible={3} /></div>
    {/if}
  </div>

  <div class="tile__badges">
    {#if healthDot}
      <!--
        Only warn and fail get a dot. A green dot on every healthy account is noise that
        trains people to ignore the colour, and a dot for "not checked" would mark every
        account on a machine where the vault is never used.
      -->
      <span
        class="badge badge--health badge--health-{healthDot}"
        title={$t(healthDot === "fail" ? "Vault_Tile_Fail" : "Vault_Tile_Warn")}
        aria-label={$t(healthDot === "fail" ? "Vault_Tile_Fail" : "Vault_Tile_Warn")}
      ></span>
    {/if}
    {#if role === "home"}
      <span class="badge badge--home">{$t("Badge_Home")}</span>
    {:else if role === "shared"}
      <span class="badge badge--shared">{$t("Badge_Shared")}</span>
    {/if}
    {#if kitTravels}
      <span class="badge badge--kit" title={$t("Badge_KitTravels")} aria-label={$t("Badge_KitTravels")}
        >⧉</span
      >
    {/if}
  </div>

  {#if index > 0 && index <= 4}
    <kbd class="tile__hint meta-mono" aria-hidden="true">{index}</kbd>
  {/if}

  <button
    type="button"
    class="tile__menu"
    aria-label={$t("Aria_AccountMenu", { name: label })}
    on:click={onMenuButton}>⋯</button
  >
</div>

<style>
  .tile {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border: 1px solid var(--hairline, var(--button-bg));
    border-radius: 10px;
    background: var(--role-panel-bg, var(--mainContentBackground));
    cursor: pointer;
    min-width: 0;
  }
  :global(.animations-enabled) .tile {
    transition:
      background-color 140ms ease-out,
      border-color 140ms ease-out;
  }
  .tile:hover:not(.tile--disabled) {
    background: var(--button-bg-hover);
  }
  .tile:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }
  .tile--home {
    border-color: var(--accent);
    box-shadow: inset 0 0 0 1px var(--accent-overlay-border, transparent);
  }
  .tile--current {
    background: var(--accent-fill-very-soft, var(--button-bg));
    cursor: default;
  }
  .tile--disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: default;
  }

  .tile__avatar {
    flex: 0 0 auto;
    width: 36px;
    height: 36px;
    display: flex;
  }
  .tile__body {
    flex: 1 1 auto;
    min-width: 0;
  }
  .tile__name {
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tile__sub,
  .tile__note {
    color: var(--role-text-muted, var(--text-subtle-gray));
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tile__note {
    font-size: 11px;
    font-style: italic;
  }
  .tile__tags {
    margin-top: 3px;
  }

  .tile__badges {
    display: flex;
    gap: 4px;
    flex: 0 0 auto;
  }
  .badge {
    font-size: 9px;
    letter-spacing: 0.06em;
    padding: 2px 5px;
    border-radius: 5px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    color: var(--role-text-muted, var(--text-subtle-gray));
    line-height: 1.2;
  }
  .badge--home {
    border-color: var(--accent);
    color: var(--accent-text-bright, var(--accent));
  }
  .badge--kit {
    font-size: 11px;
    padding: 1px 4px;
  }

  /* A dot, not a word: the badge row already carries up to two text badges, and the health
     marker has to fit alongside them without pushing the name column narrower. Colour is
     never the only carrier — the title and aria-label say which state it is. */
  .badge--health {
    width: 8px;
    height: 8px;
    padding: 0;
    border-radius: 50%;
    align-self: center;
    border: none;
  }
  .badge--health-warn {
    background: var(--warning, #d19a20);
  }
  .badge--health-fail {
    background: var(--danger, #c0392b);
  }

  .tile__hint {
    flex: 0 0 auto;
    color: var(--role-text-muted, var(--text-subtle-gray));
    border: 1px solid var(--hairline, var(--button-bg));
    border-radius: 4px;
    padding: 0 4px;
    line-height: 15px;
  }

  .tile__menu {
    flex: 0 0 auto;
    background: none;
    border: none;
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 15px;
    line-height: 1;
    padding: 4px 6px;
    border-radius: 6px;
    cursor: pointer;
  }
  .tile__menu:hover {
    background: var(--button-bg);
    color: var(--white);
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.animations-enabled) .tile {
      transition: none;
    }
  }
</style>

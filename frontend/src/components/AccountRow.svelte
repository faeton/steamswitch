<script lang="ts">
  /**
   * One account in the switcher list (REDESIGN_BRIEF.md A6 "The account list & tile").
   *
   * A row, not a card: the switcher's job is "click the account you want", and a list of
   * 52px rows shows six or more accounts in the 860×600 minimum window where the old card
   * grid showed two. The account Steam is signed in as is not a row at all — it lives once,
   * in the current-account bar above the list (`switchTargets`).
   *
   * Two sibling buttons, never one inside the other. The tile this replaces was a
   * `role="button"` wrapping its own ⋯ button, so Enter on ⋯ bubbled into the tile and
   * switched accounts. Here the main button is a real <button> (Enter and Space are native)
   * and the menu button sits beside it.
   *
   * One click switches; Shift-click opens the detail panel; right-click, the ContextMenu key,
   * Shift+F10 or the ⋯ button open the account menu.
   */
  import { createEventDispatcher } from "svelte";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import AccountTagBubbles from "./AccountTagBubbles.svelte";
  import HealthBadge from "./vault/HealthBadge.svelte";
  import Icon from "./Icon.svelte";
  import { t, locale } from "../stores/i18n";
  import { formatLastLoginForLocale } from "../lib/formatLastLogin";
  import { accountMetaLine, accountNotePreview } from "../lib/steam/accountMetaLine";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { AccountRole } from "../lib/steam/accountRoles";
  import { vaultHealth } from "../stores/vault";
  import { healthState } from "../lib/vault/health";
  import { tileKeyIntent } from "../lib/steam/tileKeys";

  export let account: SteamAccountRow;
  export let role: AccountRole = "plain";
  export let kitTravels = false;
  /** Disabled while a switch is running or a recovery is blocking. */
  export let disabled = false;
  /** 1-based quick-switch digit in the *visible* order. 0 = none. */
  export let index = 0;
  export let avatarEpoch = 0;
  /** True while this account is the target of a running switch. */
  export let switching = false;
  /** Only show vault-derived health when the vault is actually in use. */
  export let showHealth = false;

  const dispatch = createEventDispatcher<{
    switch: string;
    detail: string;
    menu: { id: string; x: number; y: number };
  }>();

  const PROFILE_PLACEHOLDER = "/img/BasicDefault.webp";

  $: id = account.steamId64;
  $: label = account.displayName?.trim() || account.personaName?.trim() || id;
  $: meta = accountMetaLine(account, (raw) => formatLastLoginForLocale(raw, $locale));
  $: note = accountNotePreview(account);
  $: health = showHealth ? healthState($vaultHealth[id]) : null;
  // Only warn and fail earn row space. A green badge on every healthy account is noise that
  // trains people to ignore the colour, and "never checked" would mark every account on a
  // machine where the vault is never used.
  $: showHealthBadge = !!health && (health.tone === "warn" || health.tone === "fail");
  // Steam's public ban flags, independent of the vault. These used to be a coloured outline
  // on the avatar and nothing else — colour as the only carrier, which the health badge
  // next to it was built specifically to avoid.
  $: vac = !!(account.showVac && account.vac);
  $: limited = !!(account.showLimited && account.ltd);
  $: tone = vac || health?.tone === "fail" ? "fail" : showHealthBadge || limited ? "warn" : undefined;
  $: quickKey = index > 0 && index <= 9 ? index : 0;

  function activate(e: MouseEvent): void {
    if (disabled) return;
    // Shift opens detail instead of switching — the same modifier the digit shortcut uses.
    if (e.shiftKey) {
      dispatch("detail", id);
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
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(r.left, r.bottom + 4);
  }

  /** Enter and Space are the button's own; only the menu keys need handling. */
  function onKeydown(e: KeyboardEvent): void {
    if (tileKeyIntent(e) !== "menu") return;
    e.preventDefault();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(r.left + 48, r.bottom - 4);
  }
</script>

<div
  class="row"
  class:row--switching={switching}
  class:row--disabled={disabled && !switching}
  data-tone={tone}
>
  <button
    type="button"
    class="row__main"
    {disabled}
    aria-busy={switching}
    aria-keyshortcuts={quickKey ? String(quickKey) : undefined}
    on:click={activate}
    on:keydown={onKeydown}
    on:contextmenu={onContextMenu}
  >
    <span class="row__key">
      {#if quickKey}
        <kbd class="keycap" title={$t("Switch_Keycap_Tooltip", { number: quickKey, name: label })}>{quickKey}</kbd>
      {/if}
    </span>

    <SteamAccountAvatar {account} epoch={avatarEpoch} fallback={PROFILE_PLACEHOLDER} />

    <span class="row__text">
      <span class="row__name-line">
        <span class="row__name" title={label}>{label}</span>
        {#if account.tags?.length}
          <AccountTagBubbles tags={account.tags} maxVisible={2} />
        {/if}
      </span>
      <span class="row__meta">
        {#if switching}
          {$t("Switch_Tile_Switching")}
        {:else}
          <span class="row__meta-text" title={meta}>{meta}</span>
          {#if note}<span class="row__note" title={note}>{note}</span>{/if}
        {/if}
      </span>
    </span>

    <span class="row__status">
      {#if vac}
        <span class="flag" data-tone="fail">{$t("Badge_Vac")}</span>
      {/if}
      {#if limited}
        <span class="flag" data-tone="warn">{$t("Badge_Limited")}</span>
      {/if}
      {#if showHealthBadge}
        <HealthBadge report={$vaultHealth[id]} />
      {/if}
      {#if role === "home"}
        <span class="flag">{$t("Badge_Home")}</span>
      {:else if role === "shared"}
        <span class="flag">
          {#if kitTravels}
            <span class="flag__icon" title={$t("Badge_KitTravels")}><Icon name="kit" size={13} /></span>
            <span class="sr-only">{$t("Badge_KitTravels")}</span>
          {/if}
          {$t("Badge_Shared")}
        </span>
      {/if}
    </span>
  </button>

  <button
    type="button"
    class="row__menu"
    aria-label={$t("Aria_AccountMenu", { name: label })}
    title={$t("Aria_AccountMenu", { name: label })}
    on:click={onMenuButton}
  >
    <Icon name="more" />
  </button>
</div>

<style>
  .row {
    --avatar-size: 32px;
    --avatar-radius: var(--radius-sm);
    display: flex;
    align-items: stretch;
    min-width: 0;
    border-radius: var(--radius-md);
    line-height: var(--lh-tight);
  }

  .row:hover:not(.row--disabled) {
    background: var(--row-hover);
  }

  :global(.animations-enabled) .row {
    transition: background-color var(--dur-fast) ease-out;
  }

  .row--switching {
    background: var(--accent-fill-very-soft, var(--row-hover));
  }

  .row--disabled {
    opacity: var(--role-disabled-opacity, 0.55);
  }

  .row__main {
    flex: 1 1 auto;
    min-width: 0;
    display: grid;
    grid-template-columns: 22px var(--avatar-size) minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--space-4);
    height: 52px;
    padding: 0 var(--space-3) 0 var(--space-4);
    border: 0;
    border-radius: inherit;
    background: none;
    color: inherit;
    font: inherit;
    text-align: start;
    cursor: pointer;
  }

  /* `UI.scss` gives every span inside every button `line-height: 2.5em`; inside this button
     that pulled the name and meta lines apart and ballooned the tag bubbles. */
  .row__main :global(span) {
    line-height: var(--lh-tight);
  }

  .row__main:disabled {
    cursor: default;
  }

  .row__main:focus-visible,
  .row__menu:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: -2px;
  }

  .row__key {
    display: flex;
    justify-content: center;
  }

  .row__text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .row__name-line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }

  .row__name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-body);
    font-weight: var(--fw-medium);
    color: var(--fg-primary);
  }

  .row__meta {
    display: flex;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fs-meta);
    color: var(--fg-muted);
  }

  .row__meta-text,
  .row__note {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row__meta-text {
    flex: 0 1 auto;
  }

  .row__note {
    flex: 1 1 0;
    font-style: italic;
  }

  .row--switching .row__meta {
    color: var(--accent-text-bright, var(--accent));
  }

  .row__status {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-3);
    flex-wrap: nowrap;
  }

  /* One badge shape for every row-level fact: role, ban flags. Tone changes the colour, the
     words carry the meaning. Visually unlike the keycap (a key) and the health badge (a dot). */
  .flag {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: 20px;
    padding: 0 var(--space-2);
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-xs);
    font-size: var(--fs-meta);
    color: var(--fg-secondary);
    white-space: nowrap;
  }

  .flag[data-tone="fail"] {
    border-color: var(--border-fail);
    background: var(--bg-fail);
    color: var(--fg-fail);
    font-weight: var(--fw-semibold);
  }

  .flag[data-tone="warn"] {
    border-color: var(--border-warn);
    background: var(--bg-warn);
    color: var(--fg-warn);
  }

  .flag__icon {
    display: inline-flex;
    color: var(--accent-text-bright, var(--accent));
  }

  .row__menu {
    flex: 0 0 auto;
    display: grid;
    place-items: center;
    width: 36px;
    margin: 8px 6px 8px 0;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--fg-muted);
    cursor: pointer;
    opacity: 0;
  }

  /* Revealed on row hover or focus, and always for keyboard users who tab to it. On a list of
     a dozen rows, a ⋯ on every one is a column of noise. */
  .row:hover .row__menu,
  .row:focus-within .row__menu,
  .row__menu:focus-visible {
    opacity: 1;
  }

  .row__menu:hover {
    background: var(--row-hover-strong);
    color: var(--fg-primary);
  }

  .keycap {
    display: inline-grid;
    place-items: center;
    min-width: 20px;
    height: 20px;
    padding: 0 4px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-bottom-width: 2px;
    border-radius: var(--radius-xs);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: var(--fs-meta);
    line-height: 1;
    color: var(--fg-secondary);
  }

  /* Windows high contrast replaces every background with Canvas, which erases the hover and
     switching tints. Outlines survive, so the states move there. */
  @media (forced-colors: active) {
    .row:hover:not(.row--disabled) {
      outline: 1px solid CanvasText;
    }
    .row--switching {
      outline: 2px solid Highlight;
    }
    .row__menu {
      opacity: 1;
    }
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>

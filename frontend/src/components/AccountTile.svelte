<script lang="ts">
  /**
   * One account in the switcher grid (REDESIGN_BRIEF.md A6 "The account list & tile").
   *
   * A single click switches — there is no select-then-act step, because at three or four
   * accounts selection buys nothing. Right-click (or the ⋯ button) opens the account menu;
   * Shift-click opens the detail panel instead of switching.
   *
   * The keycap is the visible change from before. It used to be a bare `1` with
   * `aria-hidden`, no tooltip and no affordance — brief Part B #6, "unexplained". It now
   * reads as a key (a doubled bottom border), says what it does on hover, and is announced.
   * It is a *bordered key*; health is a *dot plus words*. The brief calls out that those two
   * must never be confusable, so they differ in shape, not just colour.
   */
  import { createEventDispatcher } from "svelte";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import AccountTagBubbles from "./AccountTagBubbles.svelte";
  import HealthBadge from "./vault/HealthBadge.svelte";
  import { t, locale } from "../stores/i18n";
  import { formatLastLoginForLocale } from "../lib/formatLastLogin";
  import { accountMetaLine, accountNotePreview } from "../lib/steam/accountMetaLine";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { AccountRole } from "../lib/steam/accountRoles";
  import { vaultHealth } from "../stores/vault";
  import { healthState } from "../lib/vault/health";

  export let account: SteamAccountRow;
  export let role: AccountRole = "plain";
  export let kitTravels = false;
  /** Steam is currently set to log in as this account. */
  export let current = false;
  /** Disabled while a switch is running or a recovery is blocking. */
  export let disabled = false;
  /** 1-based position in the *visible* order, rendered as the keyboard hint. 0 = none. */
  export let index = 0;
  export let avatarEpoch = 0;
  export let boundary: HTMLElement | null = null;
  /** True while this specific account is the target of a running switch. */
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
  $: notePreview = accountNotePreview(account);
  $: health = showHealth ? healthState($vaultHealth[id]) : null;
  // Only warn and fail earn tile space. A green badge on every healthy account is noise that
  // trains people to ignore the colour, and "never checked" would mark every account on a
  // machine where the vault is never used.
  $: showHealthLine = !!health && (health.tone === "warn" || health.tone === "fail");
  $: tone = showHealthLine ? health?.tone : undefined;

  function activate(e: MouseEvent | KeyboardEvent): void {
    if (disabled) return;
    // Shift opens detail instead of switching — the same modifier the keyboard shortcut uses,
    // so the two ways of reaching detail agree. The current account has nothing to switch to,
    // so clicking it goes to detail as well.
    if (e.shiftKey || current) {
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
    e.stopPropagation();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(r.left, r.bottom + 4);
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      activate(e);
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
  class:tile--switching={switching}
  class:tile--disabled={disabled}
  data-tone={tone}
  role="button"
  tabindex={disabled ? -1 : 0}
  aria-disabled={disabled}
  aria-current={current ? "true" : undefined}
  aria-busy={switching}
  aria-keyshortcuts={index > 0 && index <= 9 && !current ? String(index) : undefined}
  on:click={activate}
  on:keydown={onKeydown}
  on:contextmenu={onContextMenu}
>
  <SteamAccountAvatar {account} epoch={avatarEpoch} fallback={PROFILE_PLACEHOLDER} {boundary} />

  <div class="tile__body">
    <div class="tile__name" title={label}>{label}</div>
    {#if account.accountName}
      <div class="tile__login meta-mono" title={account.accountName}>{account.accountName}</div>
    {/if}

    {#if switching}
      <div class="tile__state">{$t("Switch_Tile_Switching")}</div>
    {:else if showHealthLine}
      <!-- Health replaces the last-used line rather than joining it: "used 3 weeks ago" is
           small talk next to "token expired", and stacking both makes tiles uneven. -->
      <HealthBadge report={$vaultHealth[id]} />
    {:else if meta}
      <div class="tile__meta" title={meta}>{meta}</div>
    {/if}

    {#if notePreview}
      <div class="tile__note" title={notePreview}>{notePreview}</div>
    {/if}
    {#if account.tags?.length}
      <div class="tile__tags"><AccountTagBubbles tags={account.tags} maxVisible={3} /></div>
    {/if}
  </div>

  <div class="tile__end">
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

    {#if index > 0 && index <= 9 && !current}
      <kbd class="keycap" title={$t("Switch_Keycap_Tooltip", { number: index, name: label })}>
        {index}
      </kbd>
    {/if}

    <button
      type="button"
      class="tile__menu"
      aria-label={$t("Aria_AccountMenu", { name: label })}
      on:click={onMenuButton}>⋯</button
    >
  </div>
</div>

<style>
  .tile {
    --avatar-size: var(--avatar-tile-size, 42px);
    display: flex;
    align-items: center;
    gap: 13px;
    padding: var(--space-4);
    border: 1px solid var(--hairline, var(--button-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
    cursor: pointer;
    min-width: 0;
  }

  :global(.animations-enabled) .tile {
    transition:
      background-color var(--dur-fast) ease-out,
      border-color var(--dur-fast) ease-out;
  }

  .tile:hover:not(.tile--disabled) {
    border-color: var(--accent);
    background: var(--button-bg);
  }

  .tile:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }

  /*
    A tile carrying a health warning keeps that colour on its border in the resting state and
    on hover, so scanning the grid surfaces the unhappy accounts without reading every line.
  */
  .tile[data-tone="warn"] {
    border-color: var(--border-warn);
  }
  .tile[data-tone="warn"]:hover:not(.tile--disabled) {
    border-color: var(--orange);
    background: var(--bg-warn);
  }
  .tile[data-tone="fail"] {
    border-color: var(--border-fail);
  }
  .tile[data-tone="fail"]:hover:not(.tile--disabled) {
    border-color: var(--red);
    background: var(--bg-fail);
  }

  .tile--home {
    box-shadow: inset 0 0 0 1px var(--accent-overlay-border, transparent);
  }

  .tile--current {
    background: var(--accent-fill-very-soft, var(--button-bg));
  }

  .tile--switching {
    border-color: var(--accent);
    background: var(--accent-fill-very-soft, var(--button-bg));
  }

  .tile--disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: default;
  }

  .tile__body {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .tile__name {
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile__login {
    color: var(--fg-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile__meta,
  .tile__note,
  .tile__state {
    font-size: var(--fs-meta);
    color: var(--fg-disabled);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile__state {
    color: var(--accent-text-bright, var(--accent));
  }

  .tile__note {
    font-style: italic;
  }

  .tile__tags {
    margin-top: 3px;
  }

  .tile__end {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .badge {
    font-size: var(--fs-meta);
    letter-spacing: 0.06em;
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--hairline-strong, var(--button-bg));
    color: var(--fg-muted);
    line-height: 1.3;
  }

  .badge--home {
    border-color: var(--accent);
    color: var(--accent-text-bright, var(--accent));
  }

  /*
    The keycap. Its whole job is to look pressable rather than informational: a doubled bottom
    border reads as key depth, and the monospace digit matches the physical key. The tooltip
    carries the actual instruction, because a lone digit can never explain itself.
  */
  .keycap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--keycap-size);
    height: var(--keycap-size);
    padding: 0 4px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-bottom-width: 2px;
    border-radius: var(--radius-xs);
    font-family: var(--font-mono, monospace);
    font-size: var(--keycap-fs);
    line-height: 1;
    color: var(--fg-muted);
  }

  .tile__menu {
    background: none;
    border: none;
    color: var(--fg-muted);
    font-size: 15px;
    line-height: 1;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-md);
    cursor: pointer;
  }

  .tile__menu:hover {
    background: var(--button-bg-hover);
    color: var(--fg-primary);
  }

  .tile__menu:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.animations-enabled) .tile {
      transition: none;
    }
  }
</style>

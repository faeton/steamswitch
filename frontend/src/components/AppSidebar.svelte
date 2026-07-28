<script lang="ts">
  /**
   * The persistent left nav (REDESIGN_BRIEF.md A6 "App chrome & global nav").
   *
   * The brief fixes the top-level destinations as a small closed set, so this is a static
   * list, not a registry: five rows, always the same five, always in the same order. A user
   * who learns where Settings sits should find it there on every screen.
   *
   * The vault card at the bottom is the one piece of live state the chrome carries. It is
   * here rather than on the Vault page because "am I unlocked, and for how much longer" is
   * something you need to know *while doing something else* — that is the whole reason the
   * question is worth persistent space (brief A10).
   */
  import { get } from "svelte/store";
  import { t } from "../stores/i18n";
  import { route, previousPage } from "../stores/nav";
  import { prefetchPage } from "../lib/pageLoaders";
  import { TOP_LEVEL_PAGES, DEFAULT_SETTINGS_CATEGORY } from "../stores/routeCodec";
  import type { Route, TopLevelPage } from "../stores/routeCodec";
  import { securityStatus, securityStatusLoaded, lockVault } from "../stores/security";
  import { vaultStatus } from "../stores/vault";
  import { autoLockRemainingLabel } from "../stores/autoLock";

  const LABEL_KEYS: Record<TopLevelPage, string> = {
    home: "Nav_Switcher",
    vault: "Nav_Vault",
    tools: "Nav_Tools",
    settings: "Nav_Settings",
    about: "Nav_About",
  };

  function routeFor(page: TopLevelPage): Route {
    return page === "settings"
      ? { page: "settings", category: DEFAULT_SETTINGS_CATEGORY }
      : ({ page } as Route);
  }

  /**
   * Which row reads as current.
   *
   * Sub-routes light up their parent — Dota configs and Advanced clearing are reached from
   * Tools, so leaving the whole nav unhighlighted there would tell the user they had left the
   * app's structure rather than gone one level into it.
   */
  function isActive(current: Route, page: TopLevelPage): boolean {
    if (current.page === page) return true;
    if (page === "tools") {
      return current.page === "dota-configs" || current.page === "steam-advanced-clearing";
    }
    if (page === "settings") return current.page === "preview-css";
    return false;
  }

  function go(page: TopLevelPage): void {
    const next = routeFor(page);
    if (get(route).page !== next.page) {
      previousPage.set(get(route));
    }
    route.set(next);
  }

  let locking = false;

  async function onLockNow(): Promise<void> {
    if (locking) return;
    locking = true;
    try {
      await lockVault();
    } finally {
      locking = false;
    }
  }

  // Three states, not two: a machine that never set an app password has no vault to report
  // on, and showing it "locked" would imply there is something behind the lock.
  $: vaultState = !$securityStatus.appPasswordSet
    ? "off"
    : $securityStatus.vaultLocked
      ? "locked"
      : "unlocked";
  $: showVaultCard = $securityStatusLoaded && vaultState !== "off";
  $: entryCount = $vaultStatus.entryCount ?? 0;
</script>

<nav class="sidebar" aria-label={$t("Nav_Primary")}>
  <ul class="sidebar__list">
    {#each TOP_LEVEL_PAGES as page (page)}
      {@const active = isActive($route, page)}
      <li>
        <button
          type="button"
          class="ss-row-button sidebar__item"
          class:sidebar__item--active={active}
          aria-current={active ? "page" : undefined}
          on:mouseenter={() => prefetchPage(routeFor(page))}
          on:focus={() => prefetchPage(routeFor(page))}
          on:click={() => go(page)}
        >
          <span class="sidebar__marker" aria-hidden="true"></span>
          <span class="sidebar__label">{$t(LABEL_KEYS[page])}</span>
        </button>
      </li>
    {/each}
  </ul>

  <div class="sidebar__spacer"></div>

  {#if showVaultCard}
    <div class="sidebar__vault" data-state={vaultState}>
      <div class="sidebar__vault-line">
        <span class="sidebar__vault-dot" aria-hidden="true"></span>
        <span>
          {vaultState === "unlocked"
            ? $t("Sidebar_Vault_Unlocked")
            : $t("Sidebar_Vault_Locked")}
        </span>
      </div>
      {#if vaultState === "unlocked"}
        {#if $autoLockRemainingLabel}
          <div class="sidebar__vault-meta meta-mono">
            {$t("Sidebar_Vault_AutoLocksIn", { time: $autoLockRemainingLabel })}
          </div>
        {:else if entryCount > 0}
          <div class="sidebar__vault-meta meta-mono">
            {$t("Sidebar_Vault_EntryCount", { count: entryCount })}
          </div>
        {/if}
        <button type="button" class="ss-btn sidebar__vault-btn" disabled={locking} on:click={onLockNow}>
          {$t("Sidebar_Vault_LockNow")}
        </button>
      {/if}
    </div>
  {/if}
</nav>

<style>
  .sidebar {
    flex: 0 0 auto;
    width: var(--nav-width);
    display: flex;
    flex-direction: column;
    padding: var(--space-4) 10px;
    background: var(--surface-nav, var(--mainContentBackground));
    border-right: 1px solid var(--hairline, var(--border-bar-bg));
    overflow: hidden;
  }

  .sidebar__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .sidebar__item {
    display: flex;
    gap: 10px;
    padding: var(--space-3) 10px;
    border: 0;
    border-radius: var(--radius-md);
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: var(--fs-body);
    line-height: var(--lh-tight);
    text-align: left;
    cursor: pointer;
  }

  :global(.animations-enabled) .sidebar__item {
    transition:
      background-color var(--dur-fast) ease-out,
      color var(--dur-fast) ease-out;
  }

  .sidebar__item:hover:not(.sidebar__item--active) {
    background: var(--button-bg);
    color: var(--fg-primary);
  }

  .sidebar__item:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: -2px;
  }

  /*
    The active row is carried by fill *and* weight *and* the marker's colour, never by the
    accent tint alone — a user on a heavily-tinted classic theme, or one who cannot separate
    the accent from the surface, still has two other cues.
  */
  .sidebar__item--active {
    background: var(--accent-fill-very-soft, var(--button-bg));
    color: var(--accent-text-bright, var(--fg-primary));
    font-weight: var(--fw-semibold);
  }

  .sidebar__marker {
    flex: 0 0 auto;
    width: 7px;
    height: 7px;
    border-radius: 2px;
    background: var(--hairline-strong, var(--button-bg));
  }

  .sidebar__item--active .sidebar__marker {
    background: var(--accent);
  }

  .sidebar__label {
    /* Overrides the inherited `button span { line-height: 2.5em }` — see tokens.scss. */
    line-height: inherit;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sidebar__spacer {
    flex: 1 1 auto;
    min-height: var(--space-4);
  }

  .sidebar__vault {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: 11px;
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: 7px;
    background: var(--surface-panel, var(--mainContentBackground));
  }

  .sidebar__vault-line {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .sidebar__vault-dot {
    flex: 0 0 auto;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--dot-neutral);
  }

  .sidebar__vault[data-state="unlocked"] .sidebar__vault-dot {
    background: var(--green);
  }

  .sidebar__vault[data-state="locked"] .sidebar__vault-dot {
    background: var(--orange);
  }

  .sidebar__vault-meta {
    color: var(--fg-muted);
  }

  .sidebar__vault-btn {
    width: 100%;
    min-height: 28px;
    font-size: var(--fs-secondary);
  }
</style>

<script lang="ts">
  /**
   * Settings, as navigable categories rather than one scroll (REDESIGN_BRIEF.md A11).
   *
   * What this replaces: a single flat column that stacked all six blocks — Appearance, Steam,
   * Game modules, Vault, System, Language — one after another, so finding "run as admin"
   * meant scrolling past every theme in the catalogue. That column, plus the inherited
   * upstream selectors it was styled with, was the "still looks like TcNo" tell in Part B #11.
   *
   * The six blocks are *reused, not rewritten*: each becomes the body of its category. The
   * category lives in the route, so a settings page is linkable and Back walks categories
   * instead of jumping out of Settings entirely.
   *
   * Trust settings get their own category rather than being buried among refresh intervals —
   * the brief asks for that specifically.
   */
  import { get } from "svelte/store";
  import { onMount } from "svelte";
  import { appBarTitle, navigateBackLikeButton, previousPage, route } from "../stores/nav";
  import { activeModal } from "../stores/modal";
  import { t } from "../stores/i18n";
  import { SETTINGS_CATEGORIES, type SettingsCategory } from "../stores/routeCodec";
  import { controllerSpatialNavigation } from "../lib/actions/controllerSpatialNavigation";
  import PageHeader from "../components/PageHeader.svelte";
  import "../styles/Settings.scss";

  const LABEL_KEYS: Record<SettingsCategory, string> = {
    appearance: "Settings_Cat_Appearance",
    steam: "Settings_Cat_Steam",
    "game-modules": "Settings_Cat_GameModules",
    vault: "Settings_Cat_Vault",
    system: "Settings_Cat_System",
    language: "Settings_Cat_Language",
  };

  const DESC_KEYS: Record<SettingsCategory, string> = {
    appearance: "Settings_Cat_Appearance_Desc",
    steam: "Settings_Cat_Steam_Desc",
    "game-modules": "Settings_Cat_GameModules_Desc",
    vault: "Settings_Cat_Vault_Desc",
    system: "Settings_Cat_System_Desc",
    language: "Settings_Cat_Language_Desc",
  };

  /**
   * Lazy per category, so opening Settings does not pull in the theme catalogue, every Steam
   * option and the statistics panel to show one page.
   */
  const LOADERS: Record<SettingsCategory, () => Promise<{ default: unknown }>> = {
    appearance: () => import("../components/ThemeSettings.svelte"),
    steam: () => import("../components/SteamSettings.svelte"),
    "game-modules": () => import("../components/GameModuleSettings.svelte"),
    vault: () => import("../components/VaultSettings.svelte"),
    system: () => import("../components/SystemSettings.svelte"),
    language: () => import("../components/LanguageSettings.svelte"),
  };

  $: category = $route.page === "settings" ? $route.category : "appearance";
  $: appBarTitle.set($t("Title_Settings"));

  onMount(() => {
    previousPage.set({ page: "home" });
  });

  function select(next: SettingsCategory): void {
    if (next === category) return;
    route.set({ page: "settings", category: next });
  }

  function onWindowKeyDown(e: KeyboardEvent): void {
    if (e.key !== "Escape" || get(activeModal)) return;
    e.preventDefault();
    navigateBackLikeButton();
  }
</script>

<div class="settings">
  <PageHeader title={$t("Title_Settings_Short")} description={$t(DESC_KEYS[category])} />

  <div class="settings__body">
    <nav class="rail" aria-label={$t("Settings_Categories")}>
      <div class="ss-eyebrow rail__eyebrow">{$t("Settings_Categories")}</div>
      <ul class="rail__list">
        {#each SETTINGS_CATEGORIES as cat (cat)}
          {@const active = cat === category}
          <li>
            <button
              type="button"
              class="ss-row-button rail__item"
              class:rail__item--active={active}
              aria-current={active ? "page" : undefined}
              on:mouseenter={() => void LOADERS[cat]()}
              on:focus={() => void LOADERS[cat]()}
              on:click={() => select(cat)}
            >
              {$t(LABEL_KEYS[cat])}
            </button>
          </li>
        {/each}
      </ul>
    </nav>

    <div class="settings__pane" use:controllerSpatialNavigation>
      <!-- Keyed on the category so switching tabs remounts rather than leaving the previous
           page's reactive state hanging around under a new heading. -->
      {#key category}
        {#await LOADERS[category]() then { default: Body }}
          <svelte:component this={Body} />
        {/await}
      {/key}
    </div>
  </div>
</div>
<svelte:window on:keydown={onWindowKeyDown} />

<style>
  .settings {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .settings__body {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    gap: var(--space-5);
    padding: 0 var(--space-7) var(--space-5);
  }

  .rail {
    flex: 0 0 auto;
    width: var(--settings-rail-width, 230px);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    overflow-y: auto;
  }

  .rail__eyebrow {
    padding: 0 11px var(--space-2);
  }

  .rail__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .rail__item {
    display: flex;
    padding: var(--space-3) 11px;
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: var(--fs-body);
    cursor: pointer;
  }

  .rail__item:hover:not(.rail__item--active) {
    background: var(--button-bg);
    color: var(--fg-primary);
  }

  .rail__item:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: -2px;
  }

  .rail__item--active {
    border-color: var(--accent-overlay-border, var(--hairline-strong));
    background: var(--accent-fill-very-soft, var(--button-bg));
    color: var(--accent-text-bright, var(--fg-primary));
    font-weight: var(--fw-semibold);
  }

  .settings__pane {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
    padding-right: var(--space-1);
    padding-bottom: var(--space-4);
  }

  /* Below the doc's minimum the rail becomes a horizontal strip: a 230px column plus a
     content pane does not fit, and hiding categories behind a menu would undo the whole
     point of giving Settings navigation. */
  /* +190px nav: at a 1050px window the content pane is already down to ~860px. */
  @media (max-width: 1050px) {
    .settings__body {
      flex-direction: column;
      gap: var(--space-4);
    }
    .rail {
      width: auto;
    }
    .rail__eyebrow {
      display: none;
    }
    .rail__list {
      flex-direction: row;
      flex-wrap: wrap;
    }
    .rail__item {
      width: auto;
    }
  }
</style>

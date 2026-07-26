<script lang="ts">
  /**
   * The Tools hub (REDESIGN.md §4).
   *
   * Everything inherited from upstream that is useful but not part of the daily switch loop
   * lives one click behind here, rather than cluttering the main window.
   *
   * Rows are grouped rather than flat: at 420px a list of seven equal entries reads as seven
   * peers, when in fact the snapshot library is a whole workspace and "open the log folder"
   * is a single call. Grouping also puts the destructive maintenance rows visually apart from
   * the read-only ones.
   *
   * Entries are either a `Route` (anything with a back stack, a form, or progress) or an
   * `action` (one call plus a toast). Forcing everything to be a route would give
   * "VAC status check" a page it does not need.
   */
  import { get } from "svelte/store";
  import { onMount } from "svelte";
  import { appBarTitle, navigateBackLikeButton, previousPage, route } from "../stores/nav";
  import { activeModal } from "../stores/modal";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import type { Route } from "../stores/routeCodec";
  import { prefetchPage } from "../lib/pageLoaders";
  import { controllerSpatialNavigation } from "../lib/actions/controllerSpatialNavigation";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
  import "../styles/Settings.scss";

  type ToolEntry = {
    titleKey: string;
    descKey: string;
    target?: Route;
    action?: () => Promise<void>;
  };

  type ToolGroup = { labelKey: string; entries: ToolEntry[] };

  /** Which entry is mid-flight, so a slow call cannot be fired twice. */
  let running = "";

  async function withFeedback(key: string, run: () => Promise<void>, okKey: string): Promise<void> {
    if (running) return;
    running = key;
    try {
      await run();
      pushToast({ type: "success", message: get(t)(okKey), duration: 5000 });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_ActionFailed"), e),
        duration: 8000,
      });
    } finally {
      running = "";
    }
  }

  const groups: ToolGroup[] = [
    {
      labelKey: "Tools_Group_Session",
      entries: [
        {
          titleKey: "Tools_DotaConfigs_Title",
          descKey: "Tools_DotaConfigs_Desc",
          target: { page: "dota-configs" },
        },
      ],
    },
    {
      labelKey: "Tools_Group_Maintenance",
      entries: [
        {
          titleKey: "Tools_AdvancedClearing_Title",
          descKey: "Tools_AdvancedClearing_Desc",
          target: { page: "steam-advanced-clearing" },
        },
        {
          titleKey: "Tools_RefreshImages_Title",
          descKey: "Tools_RefreshImages_Desc",
          action: () =>
            withFeedback(
              "images",
              () => SteamService.RefreshAllSteamImages(),
              "Tools_RefreshImages_Done",
            ),
        },
        {
          titleKey: "Tools_VacCheck_Title",
          descKey: "Tools_VacCheck_Desc",
          action: () =>
            withFeedback("vac", () => SteamService.RefreshVACStatus(), "Tools_VacCheck_Done"),
        },
      ],
    },
    {
      labelKey: "Tools_Group_Diagnostics",
      entries: [
        {
          titleKey: "Tools_OpenDataFolder_Title",
          descKey: "Tools_OpenDataFolder_Desc",
          action: () =>
            withFeedback(
              "datafolder",
              () => PlatformService.OpenUserDataFolder(),
              "Tools_OpenDataFolder_Done",
            ),
        },
      ],
    },
  ];

  $: appBarTitle.set($t("Nav_Tools"));
  onMount(() => {
    previousPage.set({ page: "home" });
  });

  function activate(entry: ToolEntry): void {
    if (entry.target) {
      route.set(entry.target);
      return;
    }
    void entry.action?.();
  }

  function onWindowKeyDown(e: KeyboardEvent): void {
    if (e.key !== "Escape" || get(activeModal)) {
      return;
    }
    e.preventDefault();
    navigateBackLikeButton();
  }
</script>

<div class="main-content main-spacing" use:controllerSpatialNavigation>
  <h1 class="SettingsHeader">{$t("Nav_Tools")}</h1>

  {#each groups as group (group.labelKey)}
    <h2 class="tools__group">{$t(group.labelKey)}</h2>
    <ul class="tools">
      {#each group.entries as entry (entry.titleKey)}
        <li>
          <button
            type="button"
            class="tools__entry"
            disabled={running !== ""}
            on:mouseenter={() => entry.target && prefetchPage(entry.target)}
            on:focus={() => entry.target && prefetchPage(entry.target)}
            on:click={() => activate(entry)}
          >
            <span class="tools__title">{$t(entry.titleKey)}</span>
            <span class="tools__desc">{$t(entry.descKey)}</span>
            <!-- Routes get a chevron, one-shot actions do not: the affordance should say
                 whether clicking navigates or just does something. -->
            {#if entry.target}<span class="tools__chevron" aria-hidden="true">›</span>{/if}
          </button>
        </li>
      {/each}
    </ul>
  {/each}
</div>
<svelte:window on:keydown={onWindowKeyDown} />

<style>
  .tools__group {
    margin: 14px 0 6px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }
  .tools__group:first-of-type {
    margin-top: 4px;
  }
  .tools {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tools__entry {
    display: grid;
    grid-template-columns: 1fr auto;
    grid-template-rows: auto auto;
    align-items: center;
    gap: 2px 8px;
    width: 100%;
    text-align: left;
    padding: 10px 12px;
    border: 1px solid var(--hairline, var(--button-bg));
    border-radius: 10px;
    background: var(--role-panel-bg, var(--mainContentBackground));
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .tools__entry:hover:not(:disabled) {
    background: var(--button-bg-hover);
  }
  .tools__entry:disabled {
    opacity: 0.55;
    cursor: progress;
  }
  .tools__entry:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }
  .tools__title {
    grid-column: 1;
    font-size: 13px;
  }
  .tools__desc {
    grid-column: 1;
    font-size: 11px;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }
  .tools__chevron {
    grid-column: 2;
    grid-row: 1 / span 2;
    font-size: 16px;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }
</style>

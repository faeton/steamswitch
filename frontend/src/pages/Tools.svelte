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
  import { activeModal, openAlert, openConfirm } from "../stores/modal";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import type { Route } from "../stores/routeCodec";
  import { prefetchPage } from "../lib/pageLoaders";
  import { controllerSpatialNavigation } from "../lib/actions/controllerSpatialNavigation";
  import PageHeader from "../components/PageHeader.svelte";
  import Icon from "../components/Icon.svelte";
  import HelpAboutModalBody from "../components/modals/HelpAboutModalBody.svelte";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
  import "../styles/Settings.scss";

  const STEAM = "Steam";

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

  /**
   * Backup writes a zip of Steam's `config/` and `userdata/` into the data folder; the report
   * names the archive, because a backup you cannot find is not a backup.
   */
  async function backup(everything: boolean): Promise<void> {
    if (running) return;
    running = everything ? "backup-all" : "backup";
    try {
      const res = await PlatformService.BackupPlatform(STEAM, everything);
      pushToast({
        type: "success",
        message: get(t)("Tools_Backup_Done", { path: res?.archivePath ?? "", files: res?.files ?? 0 }),
        duration: 9000,
      });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Tools_Backup_Failed"), e),
        duration: 8000,
      });
    } finally {
      running = "";
    }
  }

  /**
   * Restore overwrites the live Steam config from the newest archive, so it asks first and
   * refuses while Steam is running — Steam rewrites `config/` as it exits, which would undo
   * the restore without saying anything.
   */
  async function restoreLatest(): Promise<void> {
    if (running) return;
    try {
      if (await SteamService.IsSteamRunning()) {
        pushToast({ type: "error", message: get(t)("Tools_Restore_SteamRunning"), duration: 8000 });
        return;
      }
    } catch {
      // Cannot tell — fall through to the confirmation, which spells out the risk anyway.
    }
    const ok = await openConfirm({
      title: get(t)("Tools_Restore_Title"),
      body: get(t)("Tools_Restore_Confirm"),
      style: "yesno",
    });
    if (!ok) return;

    running = "restore";
    try {
      const res = await PlatformService.RestoreLatestPlatformBackup(STEAM);
      pushToast({
        type: "success",
        message: get(t)("Tools_Restore_Done", { path: res?.restoredFrom ?? "", files: res?.files ?? 0 }),
        duration: 9000,
      });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Tools_Restore_Failed"), e),
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
    // The vault group is gone from here: "check all", "view all" and "import" are now buttons
    // on the Vault page itself, next to the roster they act on (REDESIGN_BRIEF.md A6). Leaving
    // duplicates here is how the read-only dead-end survived in the first place.
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
      // Backup covers Steam's own config and userdata trees, not SteamSwitch's saved
      // accounts — those live in the data folder and are backed up by copying it.
      labelKey: "Tools_Group_Backup",
      entries: [
        {
          titleKey: "Tools_Backup_Title",
          descKey: "Tools_Backup_Desc",
          action: () => backup(false),
        },
        {
          titleKey: "Tools_BackupAll_Title",
          descKey: "Tools_BackupAll_Desc",
          action: () => backup(true),
        },
        {
          titleKey: "Tools_Restore_Title",
          descKey: "Tools_Restore_Desc",
          action: restoreLatest,
        },
        {
          titleKey: "Tools_OpenBackup_Title",
          descKey: "Tools_OpenBackup_Desc",
          action: () =>
            withFeedback(
              "backupfolder",
              () => PlatformService.OpenPlatformBackupFolder(STEAM),
              "Tools_OpenBackup_Done",
            ),
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
        {
          // The version string and the upstream attribution. Reachable from nowhere else
          // since the title bar lost its menu.
          titleKey: "Tools_About_Title",
          descKey: "Tools_About_Desc",
          action: async () => {
            await openAlert({
              title: get(t)("Tools_About_Title"),
              bodyComponent: HelpAboutModalBody,
            });
          },
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

<div class="tools-page" use:controllerSpatialNavigation>
  <PageHeader title={$t("Nav_Tools")} />

  <div class="tools-page__scroll">
    {#each groups as group (group.labelKey)}
      <section class="tools-group">
        <h2 class="ss-section-label">{$t(group.labelKey)}</h2>
        <!-- One panel per group, rows divided by hairlines (the Settings card), instead of an
             88px bordered card per tool: ten tools now fit on one screen. -->
        <ul class="tools">
          {#each group.entries as entry (entry.titleKey)}
            <li>
              <button
                type="button"
                class="ss-row-button tools__entry"
                disabled={running !== ""}
                on:mouseenter={() => entry.target && prefetchPage(entry.target)}
                on:focus={() => entry.target && prefetchPage(entry.target)}
                on:click={() => activate(entry)}
              >
                <span class="tools__text">
                  <span class="tools__title">{$t(entry.titleKey)}</span>
                  <span class="tools__desc">{$t(entry.descKey)}</span>
                </span>
                <!-- Routes get a chevron, one-shot actions do not: the affordance should say
                     whether clicking navigates or just does something. -->
                {#if entry.target}<span class="tools__chevron"><Icon name="chevron" /></span>{/if}
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
</div>
<svelte:window on:keydown={onWindowKeyDown} />

<style>
  .tools-page {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .tools-page__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
    padding: 0 var(--space-7) var(--space-6);
    max-width: 880px;
  }

  .tools-group {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .tools {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
    overflow: hidden;
  }

  .tools > li + li {
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .tools__entry {
    display: flex;
    gap: var(--space-4);
    min-height: 56px;
    padding: var(--space-3) var(--space-4) var(--space-3) var(--space-5);
    border: 0;
    border-radius: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .tools__entry:hover:not(:disabled) {
    background: var(--row-hover);
  }

  .tools__entry:disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: progress;
  }

  .tools__entry:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: -2px;
  }

  .tools__text {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  /* `UI.scss` gives every span inside a button `line-height: 2.5em`. */
  .tools__entry :global(span) {
    line-height: var(--lh-tight);
  }

  .tools__title {
    font-size: var(--fs-body);
    color: var(--fg-primary);
  }

  .tools__entry .tools__desc {
    font-size: var(--fs-secondary);
    line-height: var(--lh-body);
    color: var(--fg-muted);
  }

  .tools__chevron {
    flex: 0 0 auto;
    display: flex;
    color: var(--fg-muted);
  }
</style>

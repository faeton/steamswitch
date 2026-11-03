<script lang="ts">
  /**
   * Settings → Steam (REDESIGN.md §4).
   *
   * The redesign deleted the per-platform settings pages, and every Steam option went with
   * them: the install folder, "stay signed in", whether a switch launches Steam at all, the
   * closing method, the refresh schedule. None of those stopped being read by the backend —
   * they just became uneditable, frozen at whatever `SteamSettings.json` last said.
   *
   * So this is a restoration, not a new surface. Two things changed on the way back:
   *
   *  - Options nothing reads were dropped rather than carried over. `SteamWebApiKey`,
   *    `ForgetAccountEnabled` and `AlwaysSwapOnShortcut` had no consumer left anywhere in the
   *    Go tree; a control that writes a value no code reads is worse than a missing one,
   *    because it looks like it works. Those fields are deleted from the structs too.
   *  - The old page listed "days before profile images expire" twice, bound to the same
   *    field. Once here.
   *
   * Everything writes the whole `Settings` struct back, because that is the shape the service
   * takes. `sanitizeSettingsPayload` drops the two fields the Go side will reject when the
   * bindings hand them over as null.
   */
  import { onMount } from "svelte";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { openFolderPicker } from "../stores/modal";
  import SharedSettingCheckbox from "./settings/SharedSettingCheckbox.svelte";
  import ProcessMethodDropdown from "./settings/ProcessMethodDropdown.svelte";
  import SettingRow from "./settings/SettingRow.svelte";
  import SettingsCard from "./settings/SettingsCard.svelte";
  import {
    ARG_SILENT,
    closingValues,
    startingValues,
    closingLabel,
    startingLabel,
    hasLaunchArgFlag,
    overrideStates,
    sanitizeSettingsPayload,
    withLaunchArgFlag,
  } from "../lib/platformSettingsShared";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import type { Settings } from "../../bindings/steamswitch/internal/steam/models";

  /** Mirrors steam.SteamAutoRefreshMinMinutes; the backend clamps regardless of what is typed. */
  const AUTO_REFRESH_MIN_MINUTES = 15;

  let settings: Settings | null = null;
  /** True once a save has failed, so the next successful one can say it recovered. */
  let saveFailed = false;
  /**
   * Where the backend will actually look for Steam, and whether that came from the setting
   * below or from detection.
   *
   * This line used to print `Settings.FolderPath` directly, which only read sensibly because
   * a fresh install shipped a hardcoded `C:\Program Files (x86)\Steam\` in that field. That
   * default was the bug — it short-circuited detection, so anyone with Steam on another drive
   * (or on macOS) had the app confidently looking at a directory that does not exist. The
   * field now starts empty and the root is detected, so the one screen a user checks to see
   * where the app is looking has to be told the answer rather than inferring it.
   */
  let resolvedRoot: { path: string; configured: boolean; exists: boolean } | null = null;

  async function loadResolvedRoot(): Promise<void> {
    try {
      resolvedRoot = await SteamService.GetResolvedSteamRoot();
    } catch {
      // Not worth a toast: the picker below still works, and the settings themselves loaded.
      resolvedRoot = null;
    }
  }

  /** Clear the stored path so detection takes over again. */
  async function useDetectedFolder(): Promise<void> {
    if (!settings) return;
    settings.FolderPath = "";
    await save();
    await loadResolvedRoot();
  }

  async function load(): Promise<void> {
    void loadResolvedRoot();
    try {
      settings = (await SteamService.GetSteamSettings()) as Settings;
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_SettingsLoadFailed"), e),
        duration: 8000,
      });
    }
  }

  /**
   * The fields this component is allowed to write.
   *
   * `SteamSettings.json` holds more than Steam options: the Home account, the shared list and
   * per-module pause state live in the same struct, and the backend writes them from
   * elsewhere. `SetGameModulePaused` and `ListGameModules` — the latter of which auto-pauses
   * as a side effect of merely being called — both do their own load-modify-save.
   *
   * So saving the object this component loaded would silently revert them. The sequence is
   * not hypothetical, it is one page: Settings mounts and reads the struct, the Game modules
   * card below auto-pauses Dota after a game update and writes it, then ticking any checkbox
   * up here saves a snapshot from before the pause and undoes it.
   *
   * Listing the owned keys is what makes that impossible, and it doubles as the record of
   * which half of the struct belongs to this card.
   */
  const OWNED_KEYS = [
    "FolderPath",
    "AutoStart",
    "RunAsAdmin",
    "LaunchArguments",
    "ClosingMethod",
    "StartingMethod",
    "TrayAccNumber",
    "ShowShortNotes",
    "CollectInfo",
    "ShowSteamSwitcher",
    "Steam_RememberPassword",
    "Steam_OverrideState",
    "Steam_ShowAccUsername",
    "Steam_ShowSteamID",
    "Steam_ShowLastLogin",
    "Steam_ShowVAC",
    "Steam_ShowLimited",
    "Steam_ShowMiniProfile",
    "Steam_ShowAvatarFrame",
    "Steam_TrayAccountName",
    "Steam_AutoRefreshOnLaunch",
    "Steam_AutoRefreshIntervalMinutes",
    "Steam_ImageExpiryTime",
  ] as const satisfies readonly (keyof Settings)[];

  /**
   * Re-read, copy the owned fields over, write that back.
   *
   * Deliberately silent on success. A settings checkbox that toasts on every click is the
   * pattern REDESIGN.md §3 set out to end — the control moving *is* the confirmation. A
   * failure still has to be said out loud, because the control will have moved anyway and
   * would otherwise be lying about what is on disk.
   */
  async function save(): Promise<void> {
    if (!settings) return;
    const edited = settings;
    try {
      const fresh = (await SteamService.GetSteamSettings()) as Settings;
      for (const key of OWNED_KEYS) {
        (fresh as unknown as Record<string, unknown>)[key] = (
          edited as unknown as Record<string, unknown>
        )[key];
      }
      await SteamService.SaveSteamSettings(sanitizeSettingsPayload(fresh) as unknown as Settings);
      if (saveFailed) {
        saveFailed = false;
        await load();
      }
    } catch (e) {
      saveFailed = true;
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_SaveFailed"), e),
        duration: 8000,
      });
      // Re-read, so the controls show what is actually stored rather than what was clicked.
      await load();
    }
  }

  function toggle<K extends keyof Settings>(key: K): void {
    if (!settings) return;
    settings[key] = !settings[key] as Settings[K];
    void save();
  }

  async function pickSteamFolder(): Promise<void> {
    if (!settings) return;
    const picked = await openFolderPicker({
      title: $t("Settings_PickFolder", { platform: "Steam" }),
      initialPath: settings.FolderPath ?? "",
      dirsOnly: true,
    });
    if (!picked) return;
    settings.FolderPath = picked;
    await save();
    await loadResolvedRoot();
  }

  $: silentOn = hasLaunchArgFlag(settings?.LaunchArguments ?? "", ARG_SILENT);

  onMount(() => {
    void load();
  });
</script>

{#if !settings}
  <p class="subtext">{$t("Kit_Module_Loading")}</p>
{:else}
<div class="steam-page">
  <SettingsCard title={$t("Settings_Header_Steam")}>
    <SettingRow
      label={$t("Settings_SteamFolder_Label")}
      description={resolvedRoot?.path || settings.FolderPath || "—"}
    >
      <span slot="detail" class="steam-root__tags">
        {#if resolvedRoot && !resolvedRoot.configured}
          <span class="steam-root__tag">{$t("Settings_SteamFolder_Detected")}</span>
        {/if}
        {#if resolvedRoot && !resolvedRoot.exists}
          <!-- The state where switching will fail. Worth saying out loud here rather than
               letting the user discover it as an unexplained error on their first switch. -->
          <span class="steam-root__tag steam-root__tag--warn">
            {$t("Settings_SteamFolder_Missing")}
          </span>
        {/if}
      </span>
      {#if resolvedRoot?.configured}
        <button type="button" class="ss-btn" on:click={() => void useDetectedFolder()}>
          {$t("Settings_SteamFolder_UseDetected")}
        </button>
      {/if}
      <button type="button" class="ss-btn" on:click={() => void pickSteamFolder()}>
        {$t("Settings_PickFolder", { platform: "Steam" })}
      </button>
    </SettingRow>

  <SharedSettingCheckbox
    id="steam-remember-password"
    checked={settings.Steam_RememberPassword}
    label={$t("Settings_SteamRememberPassword")}
    tooltip={$t("Settings_SteamRememberPassword_Tooltip")}
    on:change={() => toggle("Steam_RememberPassword")}
  />
  <SharedSettingCheckbox
    id="steam-autostart"
    checked={settings.AutoStart}
    label={$t("Settings_AutoStart", { platform: "Steam" })}
    on:change={() => toggle("AutoStart")}
  />
  <SharedSettingCheckbox
    id="steam-run-admin"
    checked={settings.RunAsAdmin}
    label={$t("Settings_Admin", { platform: "Steam" })}
    on:change={() => toggle("RunAsAdmin")}
  />
  <!-- Launch flags only reach Steam when the switch is the thing starting it. -->
  <SharedSettingCheckbox
    id="steam-silent"
    checked={silentOn}
    disabled={!settings.AutoStart}
    label={$t("Steam_StartSilent")}
    on:change={() => {
      if (!settings) return;
      settings.LaunchArguments = withLaunchArgFlag(
        settings.LaunchArguments ?? "",
        ARG_SILENT,
        !silentOn,
      );
      void save();
    }}
  />
    <SettingRow
      label={$t("Settings_LaunchArgumentsForPlatform", { platform: "Steam" })}
      description={$t("Settings_LaunchArguments_Hint")}
      controlId="steam-launch-args"
    >
      <input
        id="steam-launch-args"
        class="ss-field ss-field--mono steam-field--wide"
        type="text"
        spellcheck="false"
        autocomplete="off"
        disabled={!settings.AutoStart}
        bind:value={settings.LaunchArguments}
        on:change={() => void save()}
      />
    </SettingRow>
    <SettingRow label={$t("Steam_OverrideDefaultState")} controlId="steam-override-state">
      <select
        id="steam-override-state"
        class="ss-field steam-field--select"
        value={settings.Steam_OverrideState ?? -1}
        on:change={(e) => {
          if (!settings) return;
          settings.Steam_OverrideState = Number(e.currentTarget.value);
          void save();
        }}
      >
        {#each overrideStates as o (o.v)}
          <option value={o.v}>{$t(o.key)}</option>
        {/each}
      </select>
    </SettingRow>

  </SettingsCard>

  <SettingsCard title={$t("Settings_Header_AccountDisplay")}>
  <SharedSettingCheckbox
    id="steam-show-user"
    checked={settings.Steam_ShowAccUsername}
    label={$t("Steam_ShowAccUsername")}
    on:change={() => toggle("Steam_ShowAccUsername")}
  />
  <SharedSettingCheckbox
    id="steam-show-sid"
    checked={settings.Steam_ShowSteamID}
    label={$t("Steam_ShowSteamID")}
    on:change={() => toggle("Steam_ShowSteamID")}
  />
  <SharedSettingCheckbox
    id="steam-show-ll"
    checked={settings.Steam_ShowLastLogin}
    label={$t("Steam_ShowLastLogin")}
    on:change={() => toggle("Steam_ShowLastLogin")}
  />
  <SharedSettingCheckbox
    id="steam-show-notes"
    checked={settings.ShowShortNotes}
    label={$t("Settings_ShowShortNotes")}
    on:change={() => toggle("ShowShortNotes")}
  />
  <SharedSettingCheckbox
    id="steam-show-vac"
    checked={settings.Steam_ShowVAC}
    label={$t("Steam_ShowVac")}
    on:change={() => toggle("Steam_ShowVAC")}
  />
  <SharedSettingCheckbox
    id="steam-show-ltd"
    checked={settings.Steam_ShowLimited}
    label={$t("Steam_ShowLimited")}
    on:change={() => toggle("Steam_ShowLimited")}
  />
  <SharedSettingCheckbox
    id="steam-show-miniprofile"
    checked={settings.Steam_ShowMiniProfile}
    label={$t("Steam_ShowMiniProfile")}
    tooltip={$t("Tooltip_SteamShowMiniProfile")}
    on:change={() => toggle("Steam_ShowMiniProfile")}
  />
  <SharedSettingCheckbox
    id="steam-show-avatar-frame"
    checked={settings.Steam_ShowAvatarFrame}
    label={$t("Steam_ShowAvatarFrame")}
    tooltip={$t("Tooltip_SteamShowAvatarFrame")}
    on:change={() => toggle("Steam_ShowAvatarFrame")}
  />

  </SettingsCard>

  <SettingsCard title={$t("Settings_Steam_RefreshHeading")}>
  <SharedSettingCheckbox
    id="steam-collect-info"
    checked={settings.CollectInfo}
    label={$t("Settings_SteamCollectInfo")}
    on:change={() => toggle("CollectInfo")}
  />
  <SharedSettingCheckbox
    id="steam-auto-refresh-launch"
    checked={settings.Steam_AutoRefreshOnLaunch}
    disabled={!settings.CollectInfo}
    label={$t("Settings_Steam_AutoRefreshOnLaunch")}
    on:change={() => toggle("Steam_AutoRefreshOnLaunch")}
  />
    <SettingRow
      label={$t("Settings_Steam_AutoRefreshInterval")}
      description={$t("Settings_Steam_AutoRefreshIntervalHint", { min: AUTO_REFRESH_MIN_MINUTES })}
      controlId="steam-auto-refresh-interval"
    >
      <input
        id="steam-auto-refresh-interval"
        class="ss-field steam-field--num"
        type="number"
        min="0"
        step="5"
        disabled={!settings.CollectInfo}
        bind:value={settings.Steam_AutoRefreshIntervalMinutes}
        on:change={() => void save()}
      />
    </SettingRow>
    <SettingRow label={$t("Settings_Steam_ImageExpiry")} controlId="steam-image-expiry">
      <input
        id="steam-image-expiry"
        class="ss-field steam-field--num"
        type="number"
        min="0"
        step="1"
        bind:value={settings.Steam_ImageExpiryTime}
        on:change={() => void save()}
      />
    </SettingRow>

  </SettingsCard>

  <SettingsCard title={$t("Settings_Header_TraySettings")}>
  <SharedSettingCheckbox
    id="steam-tray-name"
    checked={settings.Steam_TrayAccountName}
    label={$t("Steam_Tray_AccountName")}
    on:change={() => toggle("Steam_TrayAccountName")}
  />
    <SettingRow label={$t("Settings_TrayMax")} controlId="steam-tray-max">
      <input
        id="steam-tray-max"
        class="ss-field steam-field--num"
        type="number"
        min="0"
        max="365"
        bind:value={settings.TrayAccNumber}
        on:change={() => void save()}
      />
    </SettingRow>
  </SettingsCard>

  <SettingsCard title={$t("Settings_Header_ProcessManagement")}>
  {#if !settings.ClosingMethodForced}
    <ProcessMethodDropdown
      values={closingValues}
      current={settings.ClosingMethod}
      label={$t("Settings_Header_ClosingMethod", { platform: "Steam" })}
      labelFn={(v) => $t(closingLabel(v))}
      tooltip={$t("Tooltip_ClosingMethod")}
      on:select={(e) => {
        if (!settings) return;
        settings.ClosingMethod = e.detail.value;
        void save();
      }}
    />
  {/if}
  <ProcessMethodDropdown
    values={startingValues}
    current={settings.StartingMethod}
    label={$t("Settings_Header_StartingMethod", { platform: "Steam" })}
    labelFn={(v) => $t(startingLabel(v))}
    tooltip={$t("Tooltip_StartingMethod")}
    on:select={(e) => {
      if (!settings) return;
      settings.StartingMethod = e.detail.value;
      void save();
    }}
  />
  <!-- Last, and worded as a warning: Steam's own switcher writes loginusers.vdf from under
       us, which is the one thing this app cannot defend against. -->
  <SharedSettingCheckbox
    id="steam-show-switcher"
    checked={settings.ShowSteamSwitcher}
    label={$t("Settings_ShowSteamSwitcher")}
    on:change={() => toggle("ShowSteamSwitcher")}
  />
  </SettingsCard>
</div>
{/if}

<style lang="scss">
  .subtext {
    margin: 0 0 8px;
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 12px;
  }

  /* One child of the settings pane; the cards inside space themselves. */
  .steam-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
  }

  .steam-root__tags {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }

  .steam-root__tags:empty {
    display: none;
  }

  .steam-root__tag {
    padding: 2px 8px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-pill, 999px);
    font-size: var(--fs-meta, 12px);
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .steam-root__tag--warn {
    border-color: var(--border-warn);
    background: var(--bg-warn, transparent);
    color: var(--fg-warn);
  }

  .steam-field--wide {
    width: 220px;
  }

  .steam-field--num {
    width: 88px;
    font-variant-numeric: tabular-nums;
  }

  .steam-field--select {
    width: auto;
    min-width: 160px;
  }
</style>

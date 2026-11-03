<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { get, writable } from "svelte/store";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import SharedSettingCheckbox from "./settings/SharedSettingCheckbox.svelte";
  import SettingRow from "./settings/SettingRow.svelte";
  import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
  import { offlineMode, setUserOfflineMode } from "../stores/offlineMode";
  import { openConfirm, openPasswordSetupModal, openPrompt } from "../stores/modal";
  import { animationsEnabled, loadAnimationsEnabled, setAnimationsEnabled } from "../stores/animationSettings";
  import {
    deleteQuarantine,
    disableSavedAccountEncryption,
    enableSavedAccountEncryption,
    isWeakPassword,
    listQuarantines,
    loadSecurityStatus,
    repairInterruptedRestore,
    removeAppPassword,
    retryQuarantineImport,
    securityStatus,
    setAppPassword,
    type SecurityQuarantineInfo,
  } from "../stores/security";
  import {
    commandPaletteHotkey,
    formatCommandPaletteHotkeyEvent,
    loadCommandPaletteHotkey,
    normalizeCommandPaletteHotkey,
    setCommandPaletteHotkey,
  } from "../stores/commandPalette";
  import {
    applyControllerSupportEnabled,
    loadControllerSupportEnabled,
    setControllerSupportEnabled,
  } from "../stores/controllerSupport";
  import { formatAppVersion } from "../lib/checkForUpdates";
  import { createToggle } from "../lib/useToggleSetting";
  import { openMoveUserDataModal, openUserDataFolder, onCheckForUpdates, openStatsModal } from "../lib/settingsOperations";

  let isWindows = false;
  let currentVersion = "";
  let userDataPath = "";

  const userDataMoveLoading = writable(false);
  const updateCheckLoading = writable(false);
  const offlineLoading = writable(false);
  const securityLoading = writable(false);
  let quarantines: SecurityQuarantineInfo[] = [];
  let commandPaletteHotkeyCaptureActive = false;

  function stopCommandPaletteHotkeyCapture(): void {
    if (!commandPaletteHotkeyCaptureActive) return;
    commandPaletteHotkeyCaptureActive = false;
    window.removeEventListener("keydown", onCommandPaletteHotkeyCaptureKeydown, true);
  }

  function startCommandPaletteHotkeyCapture(): void {
    if (commandPaletteHotkeyCaptureActive) return;
    commandPaletteHotkeyCaptureActive = true;
    window.addEventListener("keydown", onCommandPaletteHotkeyCaptureKeydown, true);
  }

  function toggleCommandPaletteHotkeyCapture(): void {
    if (commandPaletteHotkeyCaptureActive) {
      stopCommandPaletteHotkeyCapture();
      return;
    }
    startCommandPaletteHotkeyCapture();
  }

  function onCommandPaletteHotkeyCaptureKeydown(e: KeyboardEvent): void {
    if (!commandPaletteHotkeyCaptureActive) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape") {
      stopCommandPaletteHotkeyCapture();
      return;
    }
    const next = formatCommandPaletteHotkeyEvent(e);
    if (!next) return;
    stopCommandPaletteHotkeyCapture();
    void setCommandPaletteHotkey(next);
  }

  const exitToTray = createToggle(
    () => PlatformService.GetExitToTray(),
    (v) => PlatformService.SetExitToTray(v),
    get(t)("Settings_ExitToTray"),
  );

  const minimizeOnSwitch = createToggle(
    () => PlatformService.GetMinimizeOnSwitch(),
    (v) => PlatformService.SetMinimizeOnSwitch(v),
    get(t)("Settings_MinimizeOnSwitch"),
  );

  const startTrayWithWindows = createToggle(
    () => PlatformService.GetStartTrayWithWindows(),
    (v) => PlatformService.SetStartTrayWithWindows(v),
    get(t)("Settings_Tray_StartWindows"),
  );

  const startProgramCentered = createToggle(
    () => PlatformService.GetStartProgramCentered(),
    (v) => PlatformService.SetStartProgramCentered(v),
    get(t)("Settings_StartCentered"),
  );

  const statsEnabled = createToggle(
    () => PlatformService.GetStatsEnabled(),
    (v) => PlatformService.SetStatsEnabled(v),
    get(t)("Settings_CollectStats"),
  );

  const crashReportAutoSubmit = createToggle(
    () => PlatformService.GetCrashReportAutoSubmit(),
    (v) => PlatformService.SetCrashReportAutoSubmit(v),
    get(t)("Settings_CrashReportAutoSubmit"),
    () => !get(offlineMode),
  );

  const protocol = createToggle(
    () => PlatformService.GetProtocolEnabled(),
    (v) => PlatformService.SetProtocolEnabled(v),
    get(t)("Settings_Protocol"),
  );
  protocol.toggle = async () => {
    if (get(protocol.loading)) return;
    const next = !get(protocol.value);
    protocol.loading.set(true);
    try {
      await PlatformService.SetProtocolEnabled(next);
      protocol.value.set(next);
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      protocol.loading.set(false);
    }
  };

  const animations = createToggle(
    async () => { await loadAnimationsEnabled(); return get(animationsEnabled); },
    (v) => setAnimationsEnabled(v),
    get(t)("Settings_AnimationsEnabled"),
  );
  animations.toggle = async () => {
    if (get(animations.loading)) return;
    const next = !get(animations.value);
    animations.value.set(next);
    animations.loading.set(true);
    try {
      await setAnimationsEnabled(next);
    } catch (e) {
      animations.value.set(get(animationsEnabled));
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      animations.loading.set(false);
    }
  };

  const controllerSupport = createToggle(
    () => loadControllerSupportEnabled(),
    (v) => setControllerSupportEnabled(v),
    get(t)("Settings_ControllerSupport"),
  );

  const desktopHomeShortcut = createToggle(
    () => PlatformService.GetDesktopHomeShortcutExists(),
    (v) => PlatformService.SetDesktopHomeShortcut(v),
    get(t)("Settings_DesktopShortcut"),
  );
  async function refreshDesktopShortcutState(): Promise<void> {
    try {
      desktopHomeShortcut.value.set(await PlatformService.GetDesktopHomeShortcutExists());
    } catch {
      desktopHomeShortcut.value.set(false);
    }
  }
  desktopHomeShortcut.toggle = async () => {
    if (get(desktopHomeShortcut.loading)) return;
    const next = !get(desktopHomeShortcut.value);
    desktopHomeShortcut.loading.set(true);
    try {
      await PlatformService.SetDesktopHomeShortcut(next);
      await refreshDesktopShortcutState();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      desktopHomeShortcut.loading.set(false);
    }
  };

  const discordRpc = createToggle(
    () => PlatformService.GetDiscordRpc(),
    (v) => PlatformService.SetDiscordRpc(v),
    get(t)("Settings_DiscordRpc"),
    () => !get(offlineMode),
  );
  discordRpc.toggle = async () => {
    if (get(discordRpc.loading) || get(offlineMode)) return;
    const next = !get(discordRpc.value);
    discordRpc.loading.set(true);
    try {
      await PlatformService.SetDiscordRpc(next);
      discordRpc.value.set(next);
      if (!next) discordRpcShare.value.set(false);
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      discordRpc.loading.set(false);
    }
  };

  const statsShare = createToggle(
    () => PlatformService.GetStatsShare(),
    (v) => PlatformService.SetStatsShare(v),
    get(t)("Settings_ShareStats"),
    () => get(statsEnabled.value),
  );

  const prereleaseUpdates = createToggle(
    () => PlatformService.GetPrereleaseUpdates(),
    (v) => PlatformService.SetPrereleaseUpdates(v),
    get(t)("Settings_PrereleaseUpdates"),
  );

  const discordRpcShare = createToggle(
    () => PlatformService.GetDiscordRpcShare(),
    (v) => PlatformService.SetDiscordRpcShare(v),
    get(t)("Settings_DiscordRpcShare"),
    () => !get(offlineMode) && get(discordRpc.value),
  );

  type SettingsSnapshot = Awaited<ReturnType<typeof PlatformService.ReadSettings>>;

  function applySettingsSnapshot(settings: SettingsSnapshot): void {
    offlineMode.set(settings.offlineMode);
    protocol.value.set(settings.protocolEnabled);
    exitToTray.value.set(settings.exitToTray);
    statsEnabled.value.set(settings.statsEnabled);
    statsShare.value.set(settings.statsShare);
    prereleaseUpdates.value.set(settings.prereleaseUpdates);
    crashReportAutoSubmit.value.set(settings.crashReportAutoSubmit);
    discordRpc.value.set(settings.discordRpc);
    discordRpcShare.value.set(settings.discordRpcShare);
    minimizeOnSwitch.value.set(settings.minimizeOnSwitch);
    startTrayWithWindows.value.set(settings.startTrayWithWindows);
    startProgramCentered.value.set(settings.startProgramCentered);
    animationsEnabled.set(settings.animationsEnabled);
    animations.value.set(settings.animationsEnabled);
    controllerSupport.value.set(applyControllerSupportEnabled(settings.controllerSupportEnabled));
    commandPaletteHotkey.set(normalizeCommandPaletteHotkey(settings.commandPaletteHotkey));
    currentVersion = settings.appVersion || "";
  }

  async function initSettingsIndividually(): Promise<void> {
    void protocol.init();
    void exitToTray.init();
    void statsEnabled.init();
    void statsShare.init();
    void prereleaseUpdates.init();
    void crashReportAutoSubmit.init();
    void discordRpc.init();
    void discordRpcShare.init();
    void minimizeOnSwitch.init();
    void startProgramCentered.init();
    void animations.init();
    void controllerSupport.init();
    void loadCommandPaletteHotkey();
    void PlatformService.GetAppVersion()
      .then((v) => { currentVersion = v || ""; })
      .catch(() => { currentVersion = ""; });
    if (isWindows) {
      void startTrayWithWindows.init();
    }
  }

  async function hydrateSettings(): Promise<void> {
    try {
      applySettingsSnapshot(await PlatformService.ReadSettings());
    } catch {
      await initSettingsIndividually();
    }
  }

  async function toggleOfflineMode(): Promise<void> {
    if (get(offlineLoading)) return;
    const next = !get(offlineMode);
    offlineLoading.set(true);
    try {
      await setUserOfflineMode(next);
      if (next) {
        discordRpc.value.set(false);
        discordRpcShare.value.set(false);
      }
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      offlineLoading.set(false);
    }
  }

  async function refreshSecurity(): Promise<void> {
    try {
      const status = await loadSecurityStatus();
      quarantines = status.quarantineCount > 0 ? await listQuarantines() : [];
    } catch {
      quarantines = [];
    }
  }

  async function promptSecurityPassword(title: string, body: string): Promise<string | null> {
    return openPrompt({
      title,
      body,
      inputType: "password",
      positiveLabel: $t("Ok"),
      negativeLabel: $t("Button_Cancel"),
    });
  }

  async function onSetAppPassword(): Promise<void> {
    if (get(securityLoading)) return;
    const result = await openPasswordSetupModal({
      title: $t("Security_SetAppPassword"),
      positiveLabel: $t("Security_SetAppPassword"),
      negativeLabel: $t("Button_Cancel"),
    });
    if (!result) return;
    securityLoading.set(true);
    try {
      await setAppPassword(result.password);
      await refreshSecurity();
      pushToast({ type: "success", message: $t("Security_AppPasswordSet"), duration: 4000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_SaveFailed"), e), duration: 8000 });
    } finally {
      securityLoading.set(false);
    }
  }

  async function onRemoveAppPassword(): Promise<void> {
    if (get(securityLoading)) return;
    const password = await promptSecurityPassword(
      $t("Security_RemoveAppPassword"),
      $t($securityStatus.savedAccountDataEncrypted ? "Security_RemovePasswordEncryptedBody" : "Security_CurrentPasswordBody"),
    );
    if (password === null) return;
    securityLoading.set(true);
    try {
      await removeAppPassword(password);
      await refreshSecurity();
      pushToast({ type: "success", message: $t("Security_AppPasswordRemoved"), duration: 5000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Security_PasswordActionFailed"), e), duration: 8000 });
    } finally {
      securityLoading.set(false);
    }
  }

  async function onToggleSavedDataEncryption(next: boolean): Promise<void> {
    if (get(securityLoading)) return;
    const password = await promptSecurityPassword(
      next ? $t("Security_EnableEncryption") : $t("Security_DisableEncryption"),
      next ? $t("Security_EnableEncryptionBody") : $t("Security_DisableEncryptionBody"),
    );
    if (password === null) {
      await refreshSecurity();
      return;
    }
    if (next && isWeakPassword(password)) {
      const ok = await openConfirm({
        title: $t("Security_WeakPasswordTitle"),
        body: $t("Security_WeakPasswordBody"),
        positiveLabel: $t("Security_WeakPasswordContinue"),
        negativeLabel: $t("Button_Cancel"),
        style: "okcancel",
      });
      if (!ok) {
        await refreshSecurity();
        return;
      }
    }
    securityLoading.set(true);
    try {
      if (next) {
        await enableSavedAccountEncryption(password);
      } else {
        await disableSavedAccountEncryption(password);
      }
      await refreshSecurity();
      pushToast({ type: "success", message: next ? $t("Security_EncryptionEnabled") : $t("Security_EncryptionDisabled"), duration: 5000 });
    } catch (e) {
      await refreshSecurity();
      pushToast({ type: "error", message: formatToastWithError($t("Security_PasswordActionFailed"), e), duration: 8000 });
    } finally {
      securityLoading.set(false);
    }
  }

  function onSavedDataEncryptionClick(e: MouseEvent): void {
    e.preventDefault();
    if (get(securityLoading) || $securityStatus.operationBusy) return;
    void onToggleSavedDataEncryption(!$securityStatus.savedAccountDataEncrypted);
  }

  async function onRetryQuarantine(id: string): Promise<void> {
    const password = await promptSecurityPassword($t("Security_QuarantineRetry"), $t("Security_QuarantineRetryBody"));
    if (password === null) return;
    securityLoading.set(true);
    try {
      await retryQuarantineImport(id, password);
      await refreshSecurity();
      pushToast({ type: "success", message: $t("Security_QuarantineRetryDone"), duration: 5000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Security_PasswordActionFailed"), e), duration: 8000 });
    } finally {
      securityLoading.set(false);
    }
  }

  async function onDeleteQuarantine(id: string): Promise<void> {
    const ok = await openConfirm({
      title: $t("Security_QuarantineDelete"),
      destructive: true,
      body: $t("Security_QuarantineDeleteBody"),
      positiveLabel: $t("Security_QuarantineDelete"),
      negativeLabel: $t("Button_Cancel"),
      style: "okcancel",
    });
    if (!ok) return;
    securityLoading.set(true);
    try {
      await deleteQuarantine(id);
      await refreshSecurity();
    } finally {
      securityLoading.set(false);
    }
  }

  async function onRepairInterruptedRestore(): Promise<void> {
    const ok = await openConfirm({
      title: $t("Security_InterruptedRestore_Title"),
      body: $t("Security_InterruptedRestore_Body"),
      positiveLabel: $t("Security_InterruptedRestore_Repair"),
      negativeLabel: $t("Security_InterruptedRestore_Later"),
      style: "yesno",
    });
    if (!ok) return;
    securityLoading.set(true);
    try {
      await repairInterruptedRestore();
      await refreshSecurity();
      pushToast({ type: "success", message: $t("Security_InterruptedRestore_Repaired"), duration: 5000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Security_InterruptedRestore_RepairFailed"), e), duration: 8000 });
    } finally {
      securityLoading.set(false);
    }
  }

  onMount(() => {
    isWindows = /windows/i.test(navigator.userAgent) || /win32/i.test(navigator.userAgent);
    void hydrateSettings();
    void PlatformService.GetUserDataLocation()
      .then((v) => { userDataPath = v || ""; })
      .catch(() => { userDataPath = ""; });
    void refreshSecurity();
    if (isWindows) {
      void desktopHomeShortcut.init();
    }
  });

  onDestroy(() => {
    stopCommandPaletteHotkeyCapture();
  });
</script>

<h2 class="SettingsHeader">{$t("Settings_Header_System")}</h2>

<div class="sys">
  <SettingRow label={$t("Settings_Header_DataLocation_Short")} description={userDataPath || "…"}>
    <button
      type="button"
      class="ss-btn"
      disabled={$userDataMoveLoading}
      on:click={() => void openMoveUserDataModal(userDataMoveLoading, userDataPath)}
    >{$t("Settings_SetDataLocation_Short")}</button>
    <button type="button" class="ss-btn" on:click={() => void openUserDataFolder()}
      >{$t("Settings_OpenUserDataFolder_Short")}</button
    >
  </SettingRow>

  <SettingRow label={$t("Settings_Header_Security")}>
    {#if $securityStatus.appPasswordSet}
      <button
        type="button"
        class="ss-btn"
        disabled={$securityLoading}
        on:click={() => void onRemoveAppPassword()}
      >
        {$t("Security_RemoveAppPassword")}
      </button>
    {:else}
      <button
        type="button"
        class="ss-btn"
        disabled={$securityLoading}
        on:click={() => void onSetAppPassword()}
      >
        {$t("Security_SetAppPassword")}
      </button>
    {/if}
  </SettingRow>

  {#if $securityStatus.appPasswordSet}
    <SharedSettingCheckbox
      id="security-encrypt-cache"
      label={$t("Security_EncryptSavedAccountData")}
      checked={$securityStatus.savedAccountDataEncrypted}
      disabled={$securityLoading || $securityStatus.operationBusy}
      on:click={onSavedDataEncryptionClick}
    />
  {/if}

  {#if $securityStatus.interruptedRestorePending}
    <SettingRow label={$t("Security_InterruptedRestorePending")} danger>
      <button type="button" class="ss-btn" disabled={$securityLoading} on:click={() => void onRepairInterruptedRestore()}>
        {$t("Security_InterruptedRestore_Repair")}
      </button>
    </SettingRow>
  {/if}

  {#if quarantines.length > 0}
    <SettingRow label={$t("Security_QuarantineStatus", { count: quarantines.length })} danger>
      <span slot="detail" class="sys__quarantines">
        {#each quarantines as q}
          <span class="sys__quarantine">
            <span class="sys__quarantine-names">{q.accounts.join(", ")}</span>
            <button type="button" class="ss-btn sys__small" disabled={$securityLoading} on:click={() => void onRetryQuarantine(q.id)}>
              {$t("Security_QuarantineRetry")}
            </button>
            <button type="button" class="ss-btn ss-btn--danger sys__small" disabled={$securityLoading} on:click={() => void onDeleteQuarantine(q.id)}>
              {$t("Security_QuarantineDelete")}
            </button>
          </span>
        {/each}
      </span>
    </SettingRow>
  {/if}

  <SharedSettingCheckbox
    id="gs-offline"
    label={$t("Settings_OfflineMode")}
    checked={$offlineMode}
    disabled={$offlineLoading}
    on:change={() => void toggleOfflineMode()}
  />
  <SharedSettingCheckbox
    id="gs-min-switch"
    label={$t("Settings_MinimizeOnSwitch")}
    checked={$minimizeOnSwitch.value}
    disabled={$minimizeOnSwitch.loading}
    on:change={() => void minimizeOnSwitch.toggle()}
  />

  {#if isWindows}
    <SharedSettingCheckbox
      id="gs-start-tray-win"
      label={$t("Settings_Tray_StartWindows")}
      checked={$startTrayWithWindows.value}
      disabled={$startTrayWithWindows.loading}
      on:change={() => void startTrayWithWindows.toggle()}
    />
    <SharedSettingCheckbox
      id="gs-exit-tray"
      label={$t("Settings_ExitToTray")}
      checked={$exitToTray.value}
      disabled={$exitToTray.loading}
      on:change={() => void exitToTray.toggle()}
    />
    <SharedSettingCheckbox
      id="gs-protocol"
      label={$t("Settings_Protocol")}
      checked={$protocol.value}
      disabled={$protocol.loading}
      on:change={() => void protocol.toggle()}
    />
  {/if}

  <SharedSettingCheckbox
    id="gs-start-centered"
    label={$t("Settings_StartCentered")}
    checked={$startProgramCentered.value}
    disabled={$startProgramCentered.loading}
    on:change={() => void startProgramCentered.toggle()}
  />
  <SharedSettingCheckbox
    id="settings-animations"
    label={$t("Settings_AnimationsEnabled")}
    checked={$animations.value}
    disabled={$animations.loading}
    on:change={() => void animations.toggle()}
  />
  <SharedSettingCheckbox
    id="settings-controller-support"
    label={$t("Settings_ControllerSupport")}
    checked={$controllerSupport.value}
    disabled={$controllerSupport.loading}
    on:change={() => void controllerSupport.toggle()}
  />

  {#if isWindows}
    <SharedSettingCheckbox
      id="gs-desktop-home"
      label={$t("Settings_DesktopShortcut")}
      checked={$desktopHomeShortcut.value}
      disabled={$desktopHomeShortcut.loading}
      on:change={() => void desktopHomeShortcut.toggle()}
    />
  {/if}

  <SettingRow label={$t("Settings_CommandPaletteHotkey")}>
    <button
      type="button"
      class="ss-btn sys__hotkey"
      class:sys__hotkey--capturing={commandPaletteHotkeyCaptureActive}
      aria-pressed={commandPaletteHotkeyCaptureActive}
      on:click={toggleCommandPaletteHotkeyCapture}
    >
      {commandPaletteHotkeyCaptureActive ? $t("Settings_CommandPaletteHotkey_Prompt") : $commandPaletteHotkey}
    </button>
  </SettingRow>

  <p class="sys__version">{formatAppVersion(currentVersion || "0.0.0")}</p>
</div>

<h2 class="SettingsHeader">{$t("Settings_Header_LocalStats")}</h2>

<div class="sys">
  <SharedSettingCheckbox
    id="gs-stats-enabled"
    label={$t("Settings_CollectStats")}
    checked={$statsEnabled.value}
    disabled={$statsEnabled.loading}
    on:change={() => void statsEnabled.toggle()}
  />
  <div class="sys__after-row">
    <button type="button" class="ss-btn" on:click={() => void openStatsModal()}>
      {$t("Settings_ViewStats")}
    </button>
  </div>
  <SharedSettingCheckbox
    id="gs-discord-rpc"
    label={$t("Settings_DiscordRpc")}
    checked={$discordRpc.value}
    disabled={$discordRpc.loading || $offlineMode}
    on:change={() => void discordRpc.toggle()}
  />
  <SharedSettingCheckbox
    id="gs-discord-rpc-share"
    label={$t("Settings_DiscordRpcShare")}
    checked={$discordRpcShare.value}
    disabled={$discordRpcShare.loading || $offlineMode || !$discordRpc.value}
    on:change={() => void discordRpcShare.toggle()}
  />
</div>

<style lang="scss">
  /* Rows separate themselves with a hairline (SharedSettingCheckbox); SettingRow brings its
     own padding, so it is pulled flush to the same left edge here. */
  .sys {
    display: flex;
    flex-direction: column;
    margin-bottom: var(--space-6);
  }

  .sys :global(.row) {
    padding-left: 0;
    padding-right: 0;
    border-bottom: 1px solid var(--hairline);
  }

  .sys__quarantines {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }

  .sys__quarantine {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .sys__quarantine-names {
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .sys__small {
    min-height: 26px;
    padding: 0 10px;
    font-size: var(--fs-meta);
  }

  .sys__after-row {
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--hairline);
  }

  .sys__hotkey {
    min-width: 7rem;
    font-family: var(--font-mono, ui-monospace, monospace);
  }

  .sys__hotkey--capturing {
    border-color: var(--accent);
    color: var(--accent-text-bright, var(--accent));
  }

  .sys__version {
    margin: var(--space-4) 0 0;
    font-size: var(--fs-meta);
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }
</style>

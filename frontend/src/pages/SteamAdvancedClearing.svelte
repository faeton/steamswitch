<script lang="ts">
  import { onMount } from "svelte";
  import { tick } from "svelte";
  import { get } from "svelte/store";
  import { route, previousPage, appBarTitle } from "../stores/nav";
  import { t } from "../stores/i18n";
  import { sanitizeHtml } from "../lib/sanitizeHtml";
  import { pushToast } from "../stores/toast";
  import { activeModal } from "../stores/modal";
  import PageHeader from "../components/PageHeader.svelte";
  import SettingsCard from "../components/settings/SettingsCard.svelte";
  import Icon from "../components/Icon.svelte";
  import { RunAdvancedClearingAction, RunRefreshPreset } from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import "../styles/Settings.scss";


  type Row = { actions: string[] };

  /** Button rows (left-to-right); IDs match Go `RunAdvancedClearingAction`. */
  const generalRows: Row[] = [
    { actions: ["clear_logs", "clear_dumps"] },
    { actions: ["clear_htmlcache", "clear_ui_logs"] },
    { actions: ["clear_appcache", "clear_httpcache"] },
    { actions: ["clear_depotcache"] },
  ];

  const loginRows: Row[] = [
    { actions: ["delete_loginusers_vdf", "clear_ssfn"] },
    { actions: ["delete_config_vdf", "reg_autologinuser"] },
    { actions: ["reg_lastgamenameused", "reg_pseudouuid"] },
    { actions: ["reg_rememberpassword"] },
  ];

  const labelText: Record<string, string> = {
    clear_logs: "..\\Steam\\logs",
    clear_dumps: "..\\Steam\\dumps",
    clear_htmlcache: "%Local%\\Steam\\htmlcache",
    clear_ui_logs: "..\\Steam\\*.log",
    clear_appcache: "..\\Steam\\appcache",
    clear_httpcache: "..\\Steam\\appcache\\httpcache",
    clear_depotcache: "..\\Steam\\depotcache",
    delete_loginusers_vdf: "..\\Steam\\config\\loginusers.vdf",
    clear_ssfn: "..\\Steam\\ssfn*",
    delete_config_vdf: "..\\Steam\\config\\config.vdf",
    reg_autologinuser: "HKCU\\..\\AutoLoginUser",
    reg_lastgamenameused: "HKCU\\..\\LastGameNameUsed",
    reg_pseudouuid: "HKCU\\..\\PseudoUUID",
    reg_rememberpassword: "HKCU\\..\\RememberPassword",
  };

  function isRegistryAction(id: string): boolean {
    return id.startsWith("reg_");
  }

  let acceptedRisk = false;
  let registrySupported = false;
  let busy = false;
  let logLines: string[] = [];
  let logEl: HTMLDivElement | null = null;

  $: appBarTitle.set($t("Title_Steam_Cleaning"));

  const i18nLogPrefix = "i18n:";
  const i18nLogSep = "\u001f";

  function isWindowsClient(): boolean {
    if (typeof navigator === "undefined") return false;
    const uaData = (navigator as { userAgentData?: { platform?: string } }).userAgentData;
    const platform = (uaData?.platform || navigator.platform || navigator.userAgent || "").toLowerCase();
    return platform.includes("win");
  }

  onMount(() => {
    previousPage.set({ page: "tools" });
    // UI visibility is client-platform based; backend still enforces OS support.
    registrySupported = isWindowsClient();
  });

  function showAction(_id: string): boolean {
    return true;
  }

  async function scrollLogToBottom(): Promise<void> {
    await tick();
    if (logEl) {
      logEl.scrollTop = logEl.scrollHeight;
    }
  }

  function translateLogLine(line: string): string {
    if (!line.startsWith(i18nLogPrefix)) {
      return line;
    }
    const parts = line.slice(i18nLogPrefix.length).split(i18nLogSep);
    const key = parts.shift() ?? "";
    const vars: Record<string, string | number> = {};
    for (let i = 0; i < parts.length; i += 2) {
      const name = parts[i];
      if (!name) continue;
      vars[name] = parts[i + 1] ?? "";
    }
    return get(t)(key, vars);
  }

  async function runAction(id: string): Promise<void> {
    if (!acceptedRisk || busy) return;
    busy = true;
    try {
      const res = await RunAdvancedClearingAction(id);
      const lines = res?.lines?.length
        ? res.lines.map(translateLogLine)
        : [$t("SteamAdvanced_NoOutput")];
      logLines = [...logLines, ...lines, ""];
      await scrollLogToBottom();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      logLines = [...logLines, $t("SteamAdvanced_LogError", { message: msg }), ""];
      await scrollLogToBottom();
      pushToast({ type: "error", title: "", message: msg, duration: 10000 });
    } finally {
      busy = false;
    }
  }

  /** One-click bundles; IDs match Go `RunRefreshPreset`. */
  const refreshPresets = ["caches", "deep"] as const;

  async function runPreset(id: string): Promise<void> {
    if (!acceptedRisk || busy) return;
    busy = true;
    try {
      const res = await RunRefreshPreset(id);
      const lines = res?.lines?.length
        ? res.lines.map(translateLogLine)
        : [$t("SteamAdvanced_NoOutput")];
      logLines = [...logLines, ...lines, ""];
      await scrollLogToBottom();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      logLines = [...logLines, $t("SteamAdvanced_LogError", { message: msg }), ""];
      await scrollLogToBottom();
      pushToast({ type: "error", title: "", message: msg, duration: 10000 });
    } finally {
      busy = false;
    }
  }

  function clearLog(): void {
    logLines = [];
  }

  function onClose(): void {
    route.set({ page: "tools" });
  }

  function onWindowKeyDown(e: KeyboardEvent): void {
    if (e.key !== "Escape") return;
    if (get(activeModal)) return;
    onClose();
  }
</script>

<div class="adv">
  <PageHeader title={$t("Tools_AdvancedClearing_Title")} />

  <div class="adv__scroll">
    <div class="adv__main">
      <!-- The risk statement and the acknowledgement that unlocks every button below. -->
      <section class="adv__warning" aria-labelledby="adv-warning-title">
        <h2 id="adv-warning-title" class="adv__warning-title">
          <Icon name="warn" size={16} />{$t("Cleaning_ImportantInfoHeader")}
        </h2>
        <div class="adv__warning-body">
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          {@html sanitizeHtml($t("Cleaning_ImportantInfo"), "inline")}
        </div>
        <label class="adv__ack">
          <input id="steam-adv-understand" type="checkbox" class="ss-checkbox" bind:checked={acceptedRisk} />
          <span>{$t("Cleaning_Understand")}</span>
        </label>
        <div>
          <button class="ss-btn" type="button" disabled={!acceptedRisk || busy} on:click={() => void runAction("close_steam")}>
            {$t("Cleaning_Button_KillProcess", { platform: "Steam" })}
          </button>
        </div>
      </section>

      <SettingsCard title={$t("Steam_Refresh_Heading")} description={$t("Steam_Refresh_Intro")}>
        {#each refreshPresets as preset}
          <div class="adv__row">
            <span class="adv__row-text">
              <span class="adv__row-label">{$t(`Steam_Refresh_Preset_${preset}`)}</span>
              <span class="adv__row-desc">{$t(`Steam_Refresh_Preset_${preset}_Hint`)}</span>
            </span>
            <button class="ss-btn adv__btn" type="button" disabled={!acceptedRisk || busy} on:click={() => void runPreset(preset)}>
              {$t("Cleaning_Button_Run")}
            </button>
          </div>
        {/each}
      </SettingsCard>

      <!-- One row per target, the path itself as the label: these are Steam's own files and
           registry values, and the path is the precise answer to "what will this delete". -->
      <SettingsCard title={$t("Cleaning_Header_General")}>
        {#each generalRows as row}
          {#each row.actions as id}
            {#if showAction(id)}
              <div class="adv__row">
                <span class="adv__path">{labelText[id]}</span>
                <button
                  class="ss-btn adv__btn"
                  type="button"
                  disabled={!acceptedRisk || busy || (isRegistryAction(id) && !registrySupported)}
                  on:click={() => void runAction(id)}
                >
                  {$t("Cleaning_Button_Delete")}
                </button>
              </div>
            {/if}
          {/each}
        {/each}
      </SettingsCard>

      <SettingsCard title={$t("Cleaning_Header_LoginHistory")}>
        {#each loginRows as row}
          {#each row.actions as id}
            {#if showAction(id)}
              <div class="adv__row">
                <span class="adv__path">{labelText[id]}</span>
                <button
                  class="ss-btn adv__btn"
                  type="button"
                  disabled={!acceptedRisk || busy || (isRegistryAction(id) && !registrySupported)}
                  on:click={() => void runAction(id)}
                >
                  {$t("Cleaning_Button_Delete")}
                </button>
              </div>
            {/if}
          {/each}
        {/each}
      </SettingsCard>
    </div>

    <aside class="adv__log-wrap">
      <div class="adv__log-head">
        <h2 class="ss-section-label">{$t("SteamAdvanced_LogHeading")}</h2>
        <button type="button" class="ss-btn ss-btn--quiet adv__btn" on:click={clearLog}>{$t("Button_ClearLog")}</button>
      </div>
      <div class="adv__log" bind:this={logEl} role="log" aria-live="polite">
        {#if logLines.length === 0}
          <p class="adv__log-empty">{$t("SteamAdvanced_LogPlaceholder")}</p>
        {:else}
          {#each logLines as line, i (i)}
            <div class="adv__log-line">{line || "\u00a0"}</div>
          {/each}
        {/if}
      </div>
    </aside>
  </div>
</div>

<svelte:window on:keydown={onWindowKeyDown} />

<style lang="scss">
  .adv {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    /* The two-column layout answers to this page's width, not the window's. */
    container-type: inline-size;
  }

  .adv__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(260px, 0.75fr);
    align-items: start;
    gap: var(--space-6);
    padding: 0 var(--space-7) var(--space-6);
  }

  .adv__main {
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
    min-width: 0;
  }

  .adv__warning {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-5);
    border: 1px solid var(--border-warn);
    border-radius: var(--radius-lg);
    background: var(--bg-warn, var(--surface-panel));
  }

  .adv__warning-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-warn);
  }

  .adv__warning-body {
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-secondary);
  }

  .adv__ack {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    font-size: var(--fs-body);
    color: var(--fg-primary);
    cursor: pointer;
  }

  .adv__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    min-height: 48px;
    padding: var(--space-2) var(--space-3) var(--space-2) var(--space-5);
  }

  .adv__row-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .adv__row-label {
    font-size: var(--fs-body);
    color: var(--fg-primary);
  }

  .adv__row-desc {
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .adv__path {
    min-width: 0;
    overflow-wrap: anywhere;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: var(--fs-secondary);
    color: var(--fg-primary);
  }

  .adv__btn {
    flex: 0 0 auto;
    min-height: 28px;
    padding: 0 10px;
    font-size: var(--fs-meta);
  }

  /* Stays in view while the actions scroll, so a click's result is always visible. */
  .adv__log-wrap {
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-width: 0;
  }

  .adv__log-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .adv__log {
    min-height: 12rem;
    max-height: min(60vh, 32rem);
    overflow-y: auto;
    padding: var(--space-3) var(--space-4);
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-lg);
    background: var(--surface-field, var(--role-field-bg));
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: var(--fs-meta);
    line-height: var(--lh-body);
    color: var(--fg-secondary);
  }

  .adv__log-empty {
    margin: 0;
    color: var(--fg-muted);
    font-family: inherit;
  }

  .adv__log-line {
    white-space: pre-wrap;
    word-break: break-word;
  }

  @container (max-width: 760px) {
    .adv__scroll {
      grid-template-columns: minmax(0, 1fr);
    }
    .adv__log-wrap {
      position: static;
    }
  }
</style>

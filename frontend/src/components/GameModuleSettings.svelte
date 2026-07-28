<script lang="ts">
  /**
   * Settings → Game modules (REDESIGN.md §5).
   *
   * One card per Session Kit game module. The card's job is to answer "will this carry my
   * settings on the next switch, and if not, why" without the user having to guess.
   *
   * The three states are deliberately distinct rather than one "enabled" toggle:
   *  - not installed — nothing to do, and not a fault;
   *  - paused by you — you turned it off, so there is nothing to diagnose;
   *  - paused because the game changed — the layout no longer matches the one last verified,
   *    which is the only case where a self-test is the right offer.
   *
   * Listing runs the auto-pause check as a side effect on the backend, so opening this page is
   * how a game patch gets noticed.
   */
  import { onMount } from "svelte";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { tooltip } from "../lib/actions/tooltip";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";

  type ModuleStatus = {
    id: string;
    displayName: string;
    installed: boolean;
    ready: boolean;
    paused: boolean;
    pausedByFingerprintChange: boolean;
    active: boolean;
    reason?: string;
    fingerprint?: string;
    knownGoodFingerprint?: string;
    lastSelfTestAt?: string;
    parts?: string[];
  };

  let modules: ModuleStatus[] = [];
  let loading = true;
  let busyID = "";
  /** Per-module self-test outcome, cleared on the next action so a stale pass cannot mislead. */
  let results: Record<string, { passed: boolean; reason?: string }> = {};

  async function load(): Promise<void> {
    loading = true;
    try {
      modules = ((await SteamService.ListGameModules()) ?? []) as ModuleStatus[];
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Kit_ModulesFailed"), e), duration: 8000 });
    } finally {
      loading = false;
    }
  }

  async function togglePaused(m: ModuleStatus): Promise<void> {
    if (busyID) return;
    busyID = m.id;
    delete results[m.id];
    results = results;
    try {
      await SteamService.SetGameModulePaused(m.id, !m.paused);
      await load();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Kit_ModulePauseFailed"), e), duration: 8000 });
    } finally {
      busyID = "";
    }
  }

  async function selfTest(m: ModuleStatus): Promise<void> {
    if (busyID) return;
    busyID = m.id;
    try {
      const res = await SteamService.RunGameModuleSelfTest(m.id);
      results = { ...results, [m.id]: { passed: !!res?.passed, reason: res?.reason } };
      // A pass can lift an automatic pause and records a new known-good fingerprint, so the
      // card has to be re-read rather than patched locally.
      await load();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Kit_Module_SelfTestFailed"), e), duration: 8000 });
    } finally {
      busyID = "";
    }
  }

  /** Translates a reason key, falling back to the raw key so an unmapped one is still legible. */
  function reasonText(key: string | undefined): string {
    if (!key) return "";
    const translated = $t(key);
    return translated === key ? key : translated;
  }

  function stateLabel(m: ModuleStatus): string {
    if (!m.installed) return $t("Kit_Module_NotInstalled");
    if (m.active) return $t("Kit_Module_Active");
    return $t("Kit_Module_Inactive");
  }

  function stateClass(m: ModuleStatus): string {
    if (!m.installed) return "is-absent";
    return m.active ? "is-active" : "is-inactive";
  }

  onMount(() => {
    void load();
  });
</script>

<h2 class="SettingsHeader">{$t("Settings_Header_GameModules")}</h2>
<p class="hint">{$t("Settings_GameModules_Hint")}</p>

{#if loading}
  <p class="hint">{$t("Kit_Module_Loading")}</p>
{:else if modules.length === 0}
  <p class="hint">{$t("Kit_Module_None")}</p>
{:else}
  <ul class="modules">
    {#each modules as m (m.id)}
      <li class="module">
        <div class="module__head">
          <span class="module__name">{m.displayName}</span>
          <span class="module__state {stateClass(m)}">{stateLabel(m)}</span>
        </div>

        {#if m.parts?.length}
          <p class="module__parts">{m.parts.join(" · ")}</p>
        {/if}

        {#if !m.active && m.reason}
          <p class="module__reason">{reasonText(m.reason)}</p>
        {/if}

        {#if m.pausedByFingerprintChange}
          <!-- The one case where the user is being told something they did not do. Say what
               changed, not just that something did. -->
          <p class="module__reason">{$t("Kit_Module_ChangedExplain")}</p>
        {/if}

        {#if results[m.id]}
          <p class={results[m.id].passed ? "module__ok" : "module__reason"}>
            {results[m.id].passed
              ? $t("Kit_Module_SelfTestPassed")
              : `${$t("Kit_Module_SelfTestFailed")} ${reasonText(results[m.id].reason)}`}
          </p>
        {/if}

        <div class="module__actions">
          {#if m.installed}
            <button
              type="button"
              class="btnicontext"
              disabled={busyID === m.id}
              on:click={() => void togglePaused(m)}
            >
              {m.paused ? $t("Kit_Module_Resume") : $t("Kit_Module_Pause")}
            </button>
            <button
              type="button"
              class="btnicontext"
              disabled={busyID === m.id}
              use:tooltip={$t("Kit_Module_SelfTest_Tooltip")}
              on:click={() => void selfTest(m)}
            >
              {$t("Kit_Module_SelfTest")}
            </button>
          {/if}
        </div>

        {#if m.fingerprint}
          <!-- Both values, because "the layout changed" is otherwise unfalsifiable from the
               user's side and unreportable in a bug report. -->
          <dl class="module__detail">
            <dt>{$t("Kit_Module_Layout")}</dt>
            <dd>{m.fingerprint}</dd>
            {#if m.knownGoodFingerprint && m.knownGoodFingerprint !== m.fingerprint}
              <dt>{$t("Kit_Module_LayoutVerified")}</dt>
              <dd>{m.knownGoodFingerprint}</dd>
            {/if}
            {#if m.lastSelfTestAt}
              <dt>{$t("Kit_Module_LastChecked")}</dt>
              <dd>{m.lastSelfTestAt}</dd>
            {/if}
          </dl>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style lang="scss">
  .hint {
    margin: 0 0 8px;
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 12px;
  }

  .modules {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .module {
    border: 1px solid var(--role-border, rgb(255 255 255 / 12%));
    border-radius: 6px;
    padding: 8px 10px;
  }

  .module__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }

  .module__name {
    font-weight: 600;
  }

  .module__state {
    font-size: var(--fs-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .module__state.is-active {
    color: var(--status-ok-fg, #4caf50);
  }

  .module__state.is-inactive {
    color: var(--status-warn-fg, #d0a020);
  }

  .module__state.is-absent {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .module__parts,
  .module__reason,
  .module__ok {
    margin: 4px 0 0;
    font-size: 12px;
  }

  .module__parts {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .module__reason {
    color: var(--status-warn-fg, #d0a020);
  }

  .module__ok {
    color: var(--status-ok-fg, #4caf50);
  }

  .module__actions {
    margin-top: 6px;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .module__detail {
    margin: 6px 0 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 8px;
    font-family: var(--mono-font, ui-monospace, "Cascadia Mono", Consolas, monospace);
    font-size: var(--fs-secondary);
    word-break: break-all;
  }

  .module__detail dt {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .module__detail dd {
    margin: 0;
  }
</style>

<script lang="ts">
  /**
   * Copy a game's config between two accounts, in either direction. The one copy flow in the app:
   * the switcher opens it in a dialog, and Tools > Game configs wraps it with the saved-config
   * library. Every game goes through one path in the backend (gameconfig.go).
   *
   * What a copy will replace is stated in the form rather than in a confirm dialog, because only
   * one modal can be open and the switcher's form already is one. There is no default destination:
   * this overwrites an account, so which one is always a choice.
   *
   * What a game's parts hold differs per game and is not assumed: the sizes shown come from disk,
   * and with `detailed` anything else in the game's folder is named as not copied.
   */
  import { createEventDispatcher, onMount } from "svelte";
  import SettingRow from "./settings/SettingRow.svelte";
  import Icon from "./Icon.svelte";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import {
    GetSteamAccounts,
    ListConfigGames,
    GetGameConfigInfo,
    CopyGameConfigBetween,
    UndoGameConfigCopy,
  } from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import {
    canCopyConfig,
    cloudRiskFor,
    configAccountLabel,
    configErrorMessage,
    configPartsSummary,
    configProfile,
    formatBytes,
    partHintKey,
    PART_GLOBAL,
    type ConfigAccount,
    type GameInfo,
  } from "../lib/steam/gameConfig";

  type Game = { appId: string; name: string; accounts: number };
  type WriteResult = {
    copiedParts?: string[] | null;
    removedParts?: string[] | null;
    skippedParts?: string[] | null;
    revertSnapshotId?: string;
    cloudFollowUp?: boolean;
  };

  /** The account the form was opened on. It starts as the source, and is not replaced. */
  export let initialSourceId = "";
  /** The Tools page's extras: part hints, the machine-wide folder, and what is not copied. */
  export let detailed = false;

  // Bound by the Tools page, whose library applies to this selection.
  export let accounts: ConfigAccount[] = [];
  export let appId = "";
  export let info: GameInfo | null = null;
  export let sourceId = initialSourceId;
  export let destId = "";
  export let selectedParts: string[] = [];
  export let busy = false;

  const dispatch = createEventDispatcher<{ refreshed: string }>();

  let games: Game[] = [];
  let selected: Record<string, boolean> = { local: true, remote: true };
  let loaded = false;
  let error = "";
  let revert: { id: string; destId: string; label: string; parts: string[] } | null = null;

  $: profile = configProfile(appId);
  $: gameName = info?.name ?? "";
  $: shownParts = (info?.parts ?? []).filter((p) => detailed || p.id !== PART_GLOBAL);
  $: selectedParts = shownParts.map((p) => p.id).filter((id) => selected[id]);
  // The machine-wide folder is the same for both accounts, so it is offered for snapshots only.
  $: copyParts = selectedParts.filter((p) => p !== PART_GLOBAL);
  $: sourceHolds = info?.accounts.find((a) => a.steamId64 === sourceId)?.parts ?? [];
  $: destHolds = info?.accounts.find((a) => a.steamId64 === destId)?.parts ?? [];
  $: canCopy = canCopyConfig({ busy, info, sourceId, destId, copyParts, sourceParts: sourceHolds.map((p) => p.id) });
  $: cloudRisk = cloudRiskFor(info, copyParts, accounts, destId);
  // Undo writes what the copy wrote, whatever is selected now, and passes this as accepted. So it
  // has to be on show whenever Undo is.
  $: undoRisk = !!revert && cloudRiskFor(info, revert.parts, accounts, revert.destId);
  $: otherContents = [
    ...new Set(
      (info?.accounts ?? [])
        .filter((a) => a.steamId64 === sourceId || a.steamId64 === destId)
        .flatMap((a) => a.other ?? []),
    ),
  ];
  // A stale destination equal to the new source would be hidden by its own list.
  $: if (sourceId && destId === sourceId) destId = "";

  const labelFor = (id: string): string => configAccountLabel(accounts, id);

  function optionLabel(a: ConfigAccount): string {
    const name = labelFor(a.steamId64);
    return a.currentSession ? `${name} (${$t("GameCfg_Label_SignedIn")})` : name;
  }

  function holdsSummary(parts: { id: string; files: number; sizeBytes: number }[]): string {
    if (parts.length === 0) return $t("GameCfg_Label_NoConfig");
    return parts
      .map((p) => $t("GameCfg_Contents_Part", { part: $t(`GameCfg_Part_${p.id}`), files: p.files, size: formatBytes(p.sizeBytes) }))
      .join(", ");
  }

  /** Takes the game as an argument so the markup calling it updates when the answer arrives. */
  function hasData(id: string, game: GameInfo | null): boolean {
    return (game?.accounts.find((a) => a.steamId64 === id)?.parts.length ?? 0) > 0;
  }

  /** Only the newest request may fill the form: a slow answer about the previous game must not. */
  let refreshSeq = 0;

  export async function refreshGame(): Promise<void> {
    const seq = ++refreshSeq;
    const forApp = appId;
    if (!forApp) {
      info = null;
    } else {
      const next = (await GetGameConfigInfo(forApp, accounts.map((a) => a.steamId64))) as GameInfo;
      if (seq !== refreshSeq) return;
      info = next;
    }
    dispatch("refreshed", forApp);
  }

  /** Tell the user what a write did, and offer to undo it. Also used by the library's Apply. */
  export function report(res: WriteResult, message: string, dest: string): void {
    const parts = [...(res.copiedParts ?? []), ...(res.removedParts ?? [])];
    revert = res.revertSnapshotId ? { id: res.revertSnapshotId, destId: dest, label: labelFor(dest), parts } : null;
    pushToast({ type: "success", message, duration: 6000 });
    if (res.removedParts?.length) {
      pushToast({ type: "info", message: $t("GameCfg_Toast_Removed", { parts: configPartsSummary($t, res.removedParts), dest: labelFor(dest) }), duration: 10000 });
    }
    const skipped = (res.skippedParts ?? []).filter((p) => p !== PART_GLOBAL);
    if (skipped.length > 0) {
      // The destination keeps what it had for these, which a bare "copied" would hide.
      pushToast({ type: "info", message: $t("GameCfg_Toast_Skipped", { parts: configPartsSummary($t, skipped) }), duration: 10000 });
    }
    if (res.cloudFollowUp) {
      // Steam Cloud can still show a conflict on next launch. Say so rather than implying the
      // copy is durable.
      pushToast({ type: "info", message: $t("GameCfg_Toast_CloudFollowUp"), duration: 12000 });
    }
  }

  async function onGameChange(): Promise<void> {
    revert = null;
    error = "";
    selected = { local: true, remote: true };
    // The old game's details must not stay on show, or be acted on, while the new ones load.
    info = null;
    try {
      await refreshGame();
      pickDefaultSource();
    } catch (e) {
      error = configErrorMessage($t, e);
    }
  }

  function pickDefaultSource(): void {
    if (initialSourceId) return;
    if (!sourceId || !hasData(sourceId, info)) {
      sourceId = accounts.find((a) => hasData(a.steamId64, info))?.steamId64 ?? sourceId;
    }
  }

  function reverseDirection(): void {
    [sourceId, destId] = [destId, sourceId];
  }

  async function doCopy(): Promise<void> {
    if (!canCopy) return;
    busy = true;
    error = "";
    const dest = destId;
    try {
      const res = await CopyGameConfigBetween(appId, sourceId, dest, copyParts, cloudRisk);
      report(res, $t("GameCfg_Toast_Copied", { dest: labelFor(dest), parts: configPartsSummary($t, res.copiedParts ?? []) }), dest);
    } catch (e) {
      error = configErrorMessage($t, e);
    } finally {
      await refreshGame().catch(() => {});
      busy = false;
    }
  }

  async function doUndo(): Promise<void> {
    if (!revert || busy) return;
    busy = true;
    error = "";
    try {
      await UndoGameConfigCopy(appId, revert.id, undoRisk);
      pushToast({ type: "success", message: $t("GameCfg_Toast_Reverted"), duration: 5000 });
      revert = null;
    } catch (e) {
      error = configErrorMessage($t, e);
    } finally {
      await refreshGame().catch(() => {});
      busy = false;
    }
  }

  /** The game or Steam may have been closed while the window was in the background. */
  function onWindowFocus(): void {
    if (busy || !loaded || !appId) return;
    void refreshGame().catch(() => {});
  }

  onMount(async () => {
    try {
      accounts = ((await GetSteamAccounts()) ?? []) as ConfigAccount[];
      games = ((await ListConfigGames(accounts.map((a) => a.steamId64))) ?? []) as Game[];
      appId = games[0]?.appId ?? "";
      await refreshGame();
      pickDefaultSource();
    } catch (e) {
      error = configErrorMessage($t, e);
    } finally {
      loaded = true;
    }
  });
</script>

<svelte:window on:focus={onWindowFocus} />

<div class="cfg-form" class:cfg-form--stacked={!detailed}>
{#if loaded && games.length === 0 && !error}
  <p class="cfg-empty">{$t("GameCfg_NoGames")}</p>
{:else}
  <SettingRow label={$t("GameCfg_Label_Game")} controlId="cfg-game">
    <select id="cfg-game" class="ss-field cfg-select" bind:value={appId} on:change={onGameChange} disabled={busy}>
      {#each games as g (g.appId)}
        <option value={g.appId}>{g.name}</option>
      {/each}
    </select>
  </SettingRow>

  <SettingRow label={$t("GameCfg_Label_Source")} controlId="cfg-source">
    <select id="cfg-source" class="ss-field cfg-select" bind:value={sourceId} disabled={busy}>
      <option value="">{$t("GameCfg_Label_PickAccount")}</option>
      {#each accounts as a (a.steamId64)}
        <option value={a.steamId64}>
          {optionLabel(a)}{!info || hasData(a.steamId64, info) ? "" : ` — ${$t("GameCfg_Label_NoConfig")}`}
        </option>
      {/each}
    </select>
  </SettingRow>

  <div class="cfg-swap">
    <button type="button" class="ss-btn" disabled={busy || !sourceId || !destId} on:click={reverseDirection}>
      {$t("GameCfg_Button_Reverse")}
    </button>
  </div>

  <SettingRow label={$t("GameCfg_Label_Destination")} controlId="cfg-dest">
    <select id="cfg-dest" class="ss-field cfg-select" bind:value={destId} disabled={busy}>
      <option value="">{$t("GameCfg_Label_PickAccount")}</option>
      {#each accounts.filter((a) => a.steamId64 !== sourceId) as a (a.steamId64)}
        <option value={a.steamId64}>{optionLabel(a)}</option>
      {/each}
    </select>
  </SettingRow>

  {#if info && sourceId && destId}
    <p class="cfg-contents">
      <span class="cfg-contents__title">{$t("GameCfg_Contents_Title")}</span>
      <span>{labelFor(sourceId)}: {holdsSummary(sourceHolds)}</span>
      <span>{labelFor(destId)}: {holdsSummary(destHolds)}</span>
    </p>
  {/if}

  {#if info}
    <fieldset class="parts">
      <legend class="parts__legend">{$t("GameCfg_Label_Parts")}</legend>
      {#each shownParts as part (part.id)}
        <label class="part">
          <input type="checkbox" class="ss-checkbox" bind:checked={selected[part.id]} disabled={busy} />
          <span class="part__text">
            <span class="part__name">{$t(`GameCfg_Part_${part.id}`)}</span>
            {#if detailed}
              <span class="part__hint">{$t(partHintKey(profile, part.id), { app: appId })}</span>
            {/if}
          </span>
        </label>
      {/each}
    </fieldset>

    {#if detailed}
      <p class="cfg-contents">
        <span class="cfg-contents__title">{$t("GameCfg_NotCopied_Title")}</span>
        <span>{$t(`GameCfg_NotCopied_${profile}`)}</span>
        {#if otherContents.length > 0}
          <span>{$t("GameCfg_NotCopied_Other", { names: otherContents.join(", ") })}</span>
        {/if}
      </p>
    {/if}
  {/if}

  {#if info?.gameRunning || cloudRisk || undoRisk || selected[PART_GLOBAL] || canCopy || error}
    <div class="cfg-warnings">
      {#if error}
        <p class="cfg-error" role="alert"><Icon name="warn" size={14} />{error}</p>
      {/if}
      {#if info?.gameRunning}
        <p class="cfg-error" role="alert"><Icon name="warn" size={14} />{$t("GameCfg_Warning_GameRunning", { game: gameName })}</p>
      {/if}
      {#if cloudRisk}
        <p class="cfg-warning"><Icon name="warn" size={14} />{$t("GameCfg_Warning_SteamRunning", { dest: labelFor(destId) })}</p>
      {/if}
      {#if revert && undoRisk && !(cloudRisk && destId === revert.destId)}
        <p class="cfg-warning"><Icon name="warn" size={14} />{$t("GameCfg_Warning_SteamRunning", { dest: revert.label })}</p>
      {/if}
      {#if selected[PART_GLOBAL]}
        <p class="cfg-warning"><Icon name="warn" size={14} />{$t("GameCfg_Warning_GlobalCopyExcluded")}</p>
      {/if}
      {#if canCopy}
        <p class="cfg-note">{$t("GameCfg_Quick_Replaces", { dest: labelFor(destId), source: labelFor(sourceId) })}</p>
      {/if}
    </div>
  {/if}

  <div class="cfg-actions">
    <button type="button" class="ss-btn ss-btn--primary" disabled={!canCopy} on:click={doCopy}>
      {$t("GameCfg_Button_Copy")}
    </button>
    <slot name="actions" />
    {#if revert}
      <button type="button" class="ss-btn" disabled={busy} on:click={doUndo}>
        {$t("GameCfg_Button_Undo", { dest: revert.label })}
      </button>
    {/if}
  </div>
{/if}
</div>

<style lang="scss">
  /* On the Tools page the rows belong to the surrounding card. */
  .cfg-form {
    display: contents;
  }

  .cfg-form:not(.cfg-form--stacked) > :global(* + *) {
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  /* In a dialog: too narrow for a label beside a control, and the dialog brings its own padding. */
  /* Short enough not to need a scrollbar once a destination adds its lines: a scrollbar arriving
     would take its width from the form. Copy from and Copy to share a row for that reason. */
  .cfg-form--stacked {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-4);
    width: 480px;
    max-width: 100%;

    /* min-width: without it a long account name pushes past its column and widens the dialog. */
    > :global(*) {
      grid-column: 1 / -1;
      min-width: 0;
    }

    /* The form's order is game, source, reverse, destination. */
    > :global(.row:nth-child(2)) {
      grid-column: 1;
      grid-row: 2;
    }

    > :global(.row:nth-child(4)) {
      grid-column: 2;
      grid-row: 2;
    }

    :global(.row) {
      flex-direction: column;
      align-items: stretch;
      gap: var(--space-2);
      padding: 0;
    }

    :global(.row__control) {
      width: 100%;
    }

    .cfg-select {
      width: 100%;
    }

    .cfg-swap {
      justify-content: flex-start;
      padding: 0;
    }

    .cfg-contents,
    .parts,
    .cfg-warnings,
    .cfg-actions {
      padding: 0;
      border-top: 0;
    }

    .parts {
      flex-direction: row;
      flex-wrap: wrap;
      gap: var(--space-2) var(--space-5);
    }

    .part {
      align-items: center;
    }
  }

  .cfg-select {
    width: 280px;
    max-width: 100%;
  }

  .cfg-swap {
    display: flex;
    justify-content: flex-end;
    padding: var(--space-2) var(--space-5);
  }

  .cfg-contents {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: var(--space-3) var(--space-5);
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .cfg-contents__title {
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .parts {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin: 0;
    padding: var(--space-4) var(--space-5);
    /* A fieldset draws its own box; keep only the card's row divider above it. */
    border: 0;
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .parts__legend {
    float: left;
    width: 100%;
    margin-bottom: var(--space-2);
    padding: 0;
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .part {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    cursor: pointer;
  }

  .part :global(.ss-checkbox) {
    margin-top: 1px;
  }

  .part__text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .part__name {
    font-size: var(--fs-body);
    color: var(--fg-primary);
  }

  .part__hint {
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
    overflow-wrap: anywhere;
  }

  .cfg-warnings {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-5);
  }

  .cfg-warning,
  .cfg-error,
  .cfg-note {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
  }

  .cfg-warning {
    color: var(--fg-warn);
  }

  .cfg-error {
    color: var(--fg-fail);
  }

  .cfg-note {
    color: var(--fg-secondary);
  }

  .cfg-warning :global(svg),
  .cfg-error :global(svg) {
    margin-top: 3px;
  }

  .cfg-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-5);
  }

  .cfg-empty {
    margin: 0;
    padding: var(--space-5);
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
  }
</style>

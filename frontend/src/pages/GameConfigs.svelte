<script lang="ts">
  /**
   * Copy a game's per-account config between any two accounts, in either direction, and keep a
   * library of saved configs. Every game goes through one path in the backend (gameconfig.go);
   * this page only differs by what it says about each game.
   *
   * What a game's parts hold differs per game and is not assumed: the sizes shown come from disk,
   * and anything else in the game's folder is named as not copied.
   */
  import { onMount } from "svelte";
  import { previousPage, appBarTitle } from "../stores/nav";
  import PageHeader from "../components/PageHeader.svelte";
  import SettingsCard from "../components/settings/SettingsCard.svelte";
  import SettingRow from "../components/settings/SettingRow.svelte";
  import Icon from "../components/Icon.svelte";
  import { openContextMenuAtRect } from "../stores/contextMenu";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { openConfirm, openPrompt } from "../stores/modal";
  import {
    GetSteamAccounts,
    ListConfigGames,
    GetGameConfigInfo,
    CopyGameConfigBetween,
    SnapshotGameConfig,
    ListGameConfigSnapshots,
    ApplyGameConfigSnapshot,
    UndoGameConfigCopy,
    RenameGameConfigSnapshot,
    DeleteGameConfigSnapshot,
    OpenGameConfigSnapshotFolder,
  } from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import {
    canCopyConfig,
    configProfile,
    formatBytes,
    partHintKey,
    selectionHasCloudPart,
    type GameInfo,
  } from "../lib/steam/gameConfig";
  import "../styles/Settings.scss";

  type Account = { steamId64: string; accountName: string; displayName: string; currentSession?: boolean };
  type Game = { appId: string; name: string; accounts: number };
  type Snapshot = {
    id: string;
    label: string;
    note?: string;
    sourceSteamId64?: string;
    sourceAccountName?: string;
    parts: string[];
    emptyParts?: string[];
    createdAt: string;
    auto: boolean;
    sizeBytes: number;
  };

  const PART_GLOBAL = "globalcfg";

  let accounts: Account[] = [];
  let games: Game[] = [];
  let appId = "";
  let info: GameInfo | null = null;
  let snapshots: Snapshot[] = [];

  let sourceId = "";
  let destId = "";
  let selected: Record<string, boolean> = { local: true, remote: true };

  let busy = false;
  let loaded = false;
  let loadError = "";

  $: appBarTitle.set($t("Title_GameConfigs"));
  $: profile = configProfile(appId);
  $: gameName = info?.name ?? "";
  $: selectedParts = (info?.parts ?? []).map((p) => p.id).filter((id) => selected[id]);
  // The machine-wide folder is the same for both accounts, so it is offered for snapshots only.
  $: copyParts = selectedParts.filter((p) => p !== PART_GLOBAL);
  $: sourceHolds = info?.accounts.find((a) => a.steamId64 === sourceId)?.parts ?? [];
  $: destHolds = info?.accounts.find((a) => a.steamId64 === destId)?.parts ?? [];
  $: canCopy = canCopyConfig({ busy, info, sourceId, destId, copyParts, sourceParts: sourceHolds.map((p) => p.id) });
  $: cloudWarning = !!info?.steamRunning && selectionHasCloudPart(info, selectedParts);
  $: otherContents = [
    ...new Set(
      (info?.accounts ?? [])
        .filter((a) => a.steamId64 === sourceId || a.steamId64 === destId)
        .flatMap((a) => a.other ?? []),
    ),
  ];
  // A stale destination equal to the new source would be hidden by its own list.
  $: if (sourceId && destId === sourceId) destId = "";

  /**
   * Ask before writing cloud-synced files under a running Steam.
   *
   * The backend refuses this by default and takes the answer as an argument, so the choice has to
   * be made here. Returns false when there is no risk to accept, true when the user accepted it,
   * and null when they backed out, which the caller must treat as cancel, not as "no".
   */
  async function cloudRiskAccepted(usesCloud: boolean): Promise<boolean | null> {
    if (!usesCloud || !info?.steamRunning) return false;
    const ok = await openConfirm({
      title: $t("GameCfg_Confirm_CloudRiskTitle"),
      body: $t("GameCfg_Confirm_CloudRiskBody"),
    });
    return ok ? true : null;
  }

  function accountLabel(a: Account): string {
    const name = a.displayName?.trim() || a.accountName?.trim() || a.steamId64;
    return a.currentSession ? `${name} (${$t("GameCfg_Label_SignedIn")})` : name;
  }

  function labelFor(id: string): string {
    const a = accounts.find((x) => x.steamId64 === id);
    return a ? a.displayName?.trim() || a.accountName?.trim() || a.steamId64 : id;
  }

  function formatDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  function partsSummary(list: string[]): string {
    return list.map((p) => $t(`GameCfg_Part_${p}`)).join(", ");
  }

  function holdsSummary(parts: { id: string; files: number; sizeBytes: number }[]): string {
    if (parts.length === 0) return $t("GameCfg_Label_NoConfig");
    return parts
      .map((p) => $t("GameCfg_Contents_Part", { part: $t(`GameCfg_Part_${p.id}`), files: p.files, size: formatBytes(p.sizeBytes) }))
      .join(", ");
  }

  function hasData(id: string): boolean {
    return (info?.accounts.find((a) => a.steamId64 === id)?.parts.length ?? 0) > 0;
  }

  /** Only the newest request may fill the page: a slow answer about the previous game must not. */
  let refreshSeq = 0;

  async function refreshGame(): Promise<void> {
    const seq = ++refreshSeq;
    if (!appId) {
      info = null;
      snapshots = [];
      return;
    }
    const nextInfo = (await GetGameConfigInfo(appId, accounts.map((a) => a.steamId64))) as GameInfo;
    const nextSnapshots = ((await ListGameConfigSnapshots(appId)) ?? []) as Snapshot[];
    if (seq !== refreshSeq) return;
    info = nextInfo;
    snapshots = nextSnapshots;
  }

  function pickDefaultAccounts(): void {
    if (!sourceId || !hasData(sourceId)) {
      sourceId = accounts.find((a) => hasData(a.steamId64))?.steamId64 ?? sourceId;
    }
    if (!destId) {
      destId = accounts.find((a) => a.steamId64 !== sourceId)?.steamId64 ?? "";
    }
  }

  async function refreshAll(): Promise<void> {
    try {
      accounts = ((await GetSteamAccounts()) ?? []) as Account[];
      games = ((await ListConfigGames(accounts.map((a) => a.steamId64))) ?? []) as Game[];
      if (!games.some((g) => g.appId === appId)) appId = games[0]?.appId ?? "";
      await refreshGame();
      pickDefaultAccounts();
      loadError = "";
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  async function onGameChange(): Promise<void> {
    lastRevertId = "";
    selected = { local: true, remote: true };
    // The old game's details must not stay on show, or be acted on, while the new ones load.
    info = null;
    snapshots = [];
    try {
      await refreshGame();
      pickDefaultAccounts();
      loadError = "";
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    }
  }

  /** The game or Steam may have been closed while the window was in the background. */
  function onWindowFocus(): void {
    if (busy || !loaded || !appId) return;
    void refreshGame().catch(() => {});
  }

  function reverseDirection(): void {
    [sourceId, destId] = [destId, sourceId];
  }

  onMount(() => {
    previousPage.set({ page: "tools" });
    void refreshAll();
  });

  /** Latest revert point, offered as a one-click undo after a write. */
  let lastRevertId = "";
  let lastRevertLabel = "";
  let lastWroteCloud = false;

  function reportResult(
    res: { copiedParts?: string[]; removedParts?: string[]; skippedParts?: string[]; revertSnapshotId?: string; cloudFollowUp?: boolean },
    message: string,
    dest: string,
  ): void {
    lastRevertId = res.revertSnapshotId ?? "";
    lastRevertLabel = dest;
    lastWroteCloud = !!res.cloudFollowUp;
    pushToast({ type: "success", message, duration: 6000 });
    if (res.removedParts?.length) {
      pushToast({ type: "info", message: $t("GameCfg_Toast_Removed", { parts: partsSummary(res.removedParts), dest }), duration: 10000 });
    }
    const skipped = (res.skippedParts ?? []).filter((p) => p !== PART_GLOBAL);
    if (skipped.length > 0) {
      // The destination keeps what it had for these, which a bare "copied" would hide.
      pushToast({ type: "info", message: $t("GameCfg_Toast_Skipped", { parts: partsSummary(skipped) }), duration: 10000 });
    }
    if (res.cloudFollowUp) {
      // Closing Steam prevents the easy failure, but Steam Cloud can still show a conflict on
      // next launch. Say so rather than implying the copy is durable.
      pushToast({ type: "info", message: $t("GameCfg_Toast_CloudFollowUp"), duration: 12000 });
    }
  }

  async function undoLastWrite(): Promise<void> {
    if (!lastRevertId || busy) return;
    // Undoing a write under a running Steam carries the same Cloud hazard as making one. The
    // user accepted the risk for a copy they wanted, which is not accepting it for putting back.
    const risk = await cloudRiskAccepted(lastWroteCloud);
    if (risk === null) return;
    busy = true;
    try {
      await UndoGameConfigCopy(appId, lastRevertId, risk);
      pushToast({ type: "success", message: $t("GameCfg_Toast_Reverted"), duration: 5000 });
      lastRevertId = "";
      await refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  function toastError(e: unknown): void {
    const raw = e instanceof Error ? e.message : String(e);
    // Backend errors carry an i18n key on the first line.
    const key = raw.split("\n")[0]?.trim() ?? raw;
    const translated = $t(key);
    pushToast({ type: "error", message: translated === key ? raw : translated, duration: 8000 });
  }

  async function doCopy(): Promise<void> {
    if (!canCopy || !info) return;
    const ok = await openConfirm({
      title: $t("GameCfg_Confirm_CopyTitle", { game: gameName }),
      body: $t("GameCfg_Confirm_CopyBody", {
        source: labelFor(sourceId),
        dest: labelFor(destId),
        parts: partsSummary(copyParts),
      }),
    });
    if (!ok) return;
    const risk = await cloudRiskAccepted(selectionHasCloudPart(info, copyParts));
    if (risk === null) return;
    busy = true;
    try {
      const res = await CopyGameConfigBetween(appId, sourceId, destId, copyParts, risk);
      reportResult(
        res,
        $t("GameCfg_Toast_Copied", { dest: labelFor(destId), parts: partsSummary(res.copiedParts ?? []) }),
        labelFor(destId),
      );
      await refreshGame();
    } catch (e) {
      toastError(e);
      await refreshGame().catch(() => {});
    } finally {
      busy = false;
    }
  }

  async function doSnapshot(): Promise<void> {
    if (!sourceId || busy || selectedParts.length === 0) return;
    const label = await openPrompt({ title: $t("GameCfg_Prompt_SnapshotLabel"), initialValue: labelFor(sourceId) });
    if (label === null) return;
    if (!label.trim()) {
      pushToast({ type: "error", message: $t("Toast_Dota_InvalidLabel"), duration: 5000 });
      return;
    }
    const note = (await openPrompt({ title: $t("GameCfg_Prompt_SnapshotNote"), initialValue: "" })) ?? "";
    busy = true;
    try {
      await SnapshotGameConfig(appId, sourceId, label, note, selectedParts);
      pushToast({ type: "success", message: $t("GameCfg_Toast_Snapshotted", { label }), duration: 5000 });
      await refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function applySnapshot(snap: Snapshot): Promise<void> {
    if (!destId) {
      pushToast({ type: "error", message: $t("GameCfg_Error_PickDestination"), duration: 5000 });
      return;
    }
    if (!info || selectedParts.length === 0) return;
    // What actually happens is the intersection. A revert point also removes what its own account
    // did not have, and the dialog has to say so.
    const written = selectedParts.filter((p) => snap.parts.includes(p));
    const removed =
      snap.auto && snap.sourceSteamId64 === destId
        ? selectedParts.filter((p) => !snap.parts.includes(p) && (snap.emptyParts ?? []).includes(p))
        : [];
    const ok = await openConfirm({
      title: $t("GameCfg_Confirm_ApplyTitle"),
      body:
        $t("GameCfg_Confirm_ApplyBody", {
          label: snap.label,
          dest: labelFor(destId),
          parts: partsSummary([...written, ...removed]),
        }) +
        (removed.length > 0
          ? "\n\n" + $t("GameCfg_Confirm_ApplyRemoves", { parts: partsSummary(removed), dest: labelFor(destId) })
          : "") +
        (selectedParts.includes(PART_GLOBAL) && snap.parts.includes(PART_GLOBAL)
          ? "\n\n" + $t("GameCfg_Confirm_GlobalWarning")
          : ""),
    });
    if (!ok) return;
    // A snapshot with no cloud part carries no cloud risk however the checkboxes are set.
    const risk = await cloudRiskAccepted(selectionHasCloudPart(info, [...written, ...removed]));
    if (risk === null) return;
    busy = true;
    try {
      const res = await ApplyGameConfigSnapshot(appId, snap.id, destId, selectedParts, risk);
      reportResult(
        res,
        $t("GameCfg_Toast_Applied", {
          label: snap.label,
          dest: labelFor(destId),
          parts: partsSummary(res.copiedParts ?? []),
        }),
        labelFor(destId),
      );
      await refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function renameSnapshot(snap: Snapshot): Promise<void> {
    const label = await openPrompt({ title: $t("GameCfg_Prompt_SnapshotLabel"), initialValue: snap.label });
    if (label === null) return;
    const note = (await openPrompt({ title: $t("GameCfg_Prompt_SnapshotNote"), initialValue: snap.note ?? "" })) ?? "";
    busy = true;
    try {
      await RenameGameConfigSnapshot(appId, snap.id, label, note);
      await refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function deleteSnapshot(snap: Snapshot): Promise<void> {
    const ok = await openConfirm({
      title: $t("GameCfg_Confirm_DeleteTitle"),
      destructive: true,
      body: $t("GameCfg_Confirm_DeleteBody", { label: snap.label }),
    });
    if (!ok) return;
    busy = true;
    try {
      await DeleteGameConfigSnapshot(appId, snap.id);
      await refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function openSnapshot(snap: Snapshot): Promise<void> {
    try {
      await OpenGameConfigSnapshotFolder(appId, snap.id);
    } catch (e) {
      toastError(e);
    }
  }

  /** Rename, open and delete sit behind ⋯; Apply, the reason the library exists, stays out. */
  function openSnapMenu(e: MouseEvent, snap: (typeof snapshots)[number]): void {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openContextMenuAtRect(
      { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height },
      [
        { label: $t("GameCfg_Button_Rename"), action: () => void renameSnapshot(snap) },
        { label: $t("Button_OpenBackup"), action: () => void openSnapshot(snap) },
        { type: "separator" },
        { label: $t("GameCfg_Button_Delete"), danger: true, action: () => void deleteSnapshot(snap) },
      ],
    );
  }
</script>

<svelte:window on:focus={onWindowFocus} />

<div class="cfg-page">
  <PageHeader title={$t("GameCfg_Heading")} description={$t("GameCfg_Intro")} />

  <div class="cfg-page__scroll">
    {#if loadError}
      <p class="cfg-error" role="alert"><Icon name="warn" size={14} />{loadError}</p>
    {/if}

    {#if loaded && games.length === 0 && !loadError}
      <p class="cfg-empty">{$t("GameCfg_NoGames")}</p>
    {:else}
      <SettingsCard title={$t("GameCfg_Section_Copy")} description={$t("GameCfg_Note_AutoRevert")}>
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
                {accountLabel(a)}{hasData(a.steamId64) ? "" : ` — ${$t("GameCfg_Label_NoConfig")}`}
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
              <option value={a.steamId64}>{accountLabel(a)}</option>
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
            {#each info.parts as part (part.id)}
              <label class="part">
                <input type="checkbox" class="ss-checkbox" bind:checked={selected[part.id]} disabled={busy} />
                <span class="part__text">
                  <span class="part__name">{$t(`GameCfg_Part_${part.id}`)}</span>
                  <span class="part__hint">{$t(partHintKey(profile, part.id), { app: appId })}</span>
                </span>
              </label>
            {/each}
          </fieldset>

          <p class="cfg-contents">
            <span class="cfg-contents__title">{$t("GameCfg_NotCopied_Title")}</span>
            <span>{$t(`GameCfg_NotCopied_${profile}`)}</span>
            {#if otherContents.length > 0}
              <span>{$t("GameCfg_NotCopied_Other", { names: otherContents.join(", ") })}</span>
            {/if}
          </p>
        {/if}

        {#if info?.gameRunning || cloudWarning || selected[PART_GLOBAL]}
          <div class="cfg-warnings">
            {#if info?.gameRunning}
              <p class="cfg-error" role="alert"><Icon name="warn" size={14} />{$t("GameCfg_Warning_GameRunning", { game: gameName })}</p>
            {/if}
            {#if cloudWarning}
              <p class="cfg-warning"><Icon name="warn" size={14} />{$t("GameCfg_Warning_SteamRunning")}</p>
            {/if}
            {#if selected[PART_GLOBAL]}
              <p class="cfg-warning"><Icon name="warn" size={14} />{$t("GameCfg_Warning_GlobalCopyExcluded")}</p>
            {/if}
          </div>
        {/if}

        <div class="cfg-actions">
          <button type="button" class="ss-btn ss-btn--primary" disabled={!canCopy} on:click={doCopy}>
            {$t("GameCfg_Button_Copy")}
          </button>
          <button type="button" class="ss-btn" disabled={busy || !sourceId || !appId || selectedParts.length === 0} on:click={doSnapshot}>
            {$t("GameCfg_Button_Snapshot")}
          </button>
          {#if lastRevertId}
            <button type="button" class="ss-btn" disabled={busy} on:click={undoLastWrite}>
              {$t("GameCfg_Button_Undo", { dest: lastRevertLabel })}
            </button>
          {/if}
        </div>
      </SettingsCard>

      <SettingsCard title={$t("GameCfg_Section_Library")}>
        {#if snapshots.length === 0}
          <p class="cfg-empty">{$t("GameCfg_Library_Empty")}</p>
        {:else}
          {#each snapshots as snap (snap.id)}
            <div class="snap">
              <div class="snap__text">
                <span class="snap__name-line">
                  <span class="snap__name">{snap.label}</span>
                  {#if snap.auto}<span class="snap__badge">{$t("GameCfg_Badge_RevertPoint")}</span>{/if}
                </span>
                {#if snap.note}<span class="snap__note">{snap.note}</span>{/if}
                <span class="snap__meta">
                  {formatDate(snap.createdAt)} · {snap.parts.length > 0 ? partsSummary(snap.parts) : $t("GameCfg_Snapshot_HadNothing")} · {formatBytes(snap.sizeBytes)}
                  {#if snap.sourceAccountName} · {snap.sourceAccountName}{/if}
                </span>
              </div>
              <div class="snap__actions">
                <button type="button" class="ss-btn snap__btn" disabled={busy || selectedParts.length === 0} on:click={() => applySnapshot(snap)}>
                  {$t("GameCfg_Button_Apply")}
                </button>
                <button
                  type="button"
                  class="snap__menu"
                  aria-haspopup="menu"
                  aria-label={$t("Aria_AccountMenu", { name: snap.label })}
                  title={$t("Aria_AccountMenu", { name: snap.label })}
                  disabled={busy}
                  on:click={(e) => openSnapMenu(e, snap)}
                >
                  <Icon name="more" />
                </button>
              </div>
            </div>
          {/each}
        {/if}
      </SettingsCard>
    {/if}
  </div>
</div>

<style lang="scss">
  .cfg-page {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .cfg-page__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
    padding: 0 var(--space-7) var(--space-6);
    max-width: 880px;
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
  .cfg-error {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-warn);
  }

  .cfg-warning :global(svg),
  .cfg-error :global(svg) {
    margin-top: 3px;
  }

  .cfg-error {
    color: var(--fg-fail);
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

  .snap {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-height: 56px;
    padding: var(--space-3) var(--space-3) var(--space-3) var(--space-5);
  }

  .snap:hover {
    background: var(--row-hover);
  }

  .snap__text {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .snap__name-line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }

  .snap__name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-body);
    font-weight: var(--fw-medium);
    color: var(--fg-primary);
  }

  .snap__badge {
    flex: 0 0 auto;
    padding: 0 var(--space-2);
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-xs);
    font-size: var(--fs-meta);
    color: var(--fg-secondary);
  }

  .snap__note {
    font-size: var(--fs-secondary);
    font-style: italic;
    color: var(--fg-secondary);
  }

  .snap__meta {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }

  .snap__actions {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .snap__btn {
    min-height: 28px;
    padding: 0 10px;
    font-size: var(--fs-meta);
  }

  .snap__menu {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    margin: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .snap__menu:hover:not(:disabled) {
    background: var(--row-hover-strong);
    color: var(--fg-primary);
  }

  .snap__menu:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 0;
  }
</style>

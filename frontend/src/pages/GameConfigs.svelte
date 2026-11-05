<script lang="ts">
  /**
   * The library of saved configs: save one from an account, apply one to an account. Copying between
   * accounts is on the switcher; this page borrows that form's game, account and part pickers for
   * the saving half. Applying picks its account on the row, so nothing has to be set up first.
   */
  import { onMount } from "svelte";
  import { previousPage, appBarTitle } from "../stores/nav";
  import PageHeader from "../components/PageHeader.svelte";
  import SettingsCard from "../components/settings/SettingsCard.svelte";
  import Icon from "../components/Icon.svelte";
  import GameConfigCopyForm from "../components/GameConfigCopyForm.svelte";
  import { openContextMenuAtRect } from "../stores/contextMenu";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { openConfirm, openPrompt } from "../stores/modal";
  import {
    SnapshotGameConfig,
    ListGameConfigSnapshots,
    ApplyGameConfigSnapshot,
    RenameGameConfigSnapshot,
    DeleteGameConfigSnapshot,
    OpenGameConfigSnapshotFolder,
  } from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import {
    cloudRiskFor,
    configAccountLabel,
    configErrorMessage,
    configPartsSummary,
    formatBytes,
    PART_GLOBAL,
    type ConfigAccount,
    type GameInfo,
  } from "../lib/steam/gameConfig";
  import "../styles/Settings.scss";

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

  let form: GameConfigCopyForm;
  let accounts: ConfigAccount[] = [];
  let appId = "";
  let info: GameInfo | null = null;
  let sourceId = "";
  let selectedParts: string[] = [];
  let busy = false;
  let snapshots: Snapshot[] = [];

  $: appBarTitle.set($t("Title_GameConfigs"));

  // Snapshot IDs belong to one game. The old game's rows must not stay on show, or be acted on,
  // while the new list loads or if it fails to.
  let listedApp = "";
  $: if (appId !== listedApp) {
    listedApp = appId;
    listSeq++;
    snapshots = [];
  }

  const labelFor = (id: string): string => configAccountLabel(accounts, id);
  const partsSummary = (list: string[]): string => configPartsSummary($t, list);

  function formatDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  function toastError(e: unknown): void {
    pushToast({ type: "error", message: configErrorMessage($t, e), duration: 8000 });
  }

  /**
   * Ask before writing cloud-synced files to the account signed in to a running Steam.
   *
   * The backend refuses this by default and takes the answer as an argument, so the choice has to
   * be made here. Returns false when there is no risk to accept, true when the user accepted it,
   * and null when they backed out, which the caller must treat as cancel, not as "no".
   */
  async function cloudRiskAccepted(parts: string[], dest: string): Promise<boolean | null> {
    if (!cloudRiskFor(info, parts, accounts, dest)) return false;
    const ok = await openConfirm({
      title: $t("GameCfg_Confirm_CloudRiskTitle"),
      body: $t("GameCfg_Confirm_CloudRiskBody"),
    });
    return ok ? true : null;
  }

  /** Only the newest list may fill the page: a slow answer about the previous game must not. */
  let listSeq = 0;

  async function refreshSnapshots(forApp: string): Promise<void> {
    const seq = ++listSeq;
    const next = forApp ? (((await ListGameConfigSnapshots(forApp)) ?? []) as Snapshot[]) : [];
    if (seq === listSeq) snapshots = next;
  }

  onMount(() => {
    previousPage.set({ page: "tools" });
  });

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
      await form.refreshGame();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  /**
   * What applying writes. A revert point also removes what its own account did not have, and leaves
   * the machine-wide part alone: copies never change it, so there is nothing of it to revert.
   */
  function partsToApply(snap: Snapshot, destId: string): { written: string[]; removed: string[] } {
    if (!snap.auto) return { written: snap.parts, removed: [] };
    const account = snap.parts.filter((p) => p !== PART_GLOBAL);
    const empty = snap.sourceSteamId64 === destId ? (snap.emptyParts ?? []).filter((p) => p !== PART_GLOBAL) : [];
    return { written: account, removed: empty };
  }

  async function applySnapshot(snap: Snapshot, destId: string): Promise<void> {
    if (!info) return;
    const { written, removed } = partsToApply(snap, destId);
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
        (written.includes(PART_GLOBAL)
          ? "\n\n" + $t("GameCfg_Confirm_GlobalWarning")
          : ""),
    });
    if (!ok) return;
    const risk = await cloudRiskAccepted([...written, ...removed], destId);
    if (risk === null) return;
    busy = true;
    try {
      const res = await ApplyGameConfigSnapshot(appId, snap.id, destId, [...written, ...removed], risk);
      form.report(
        res,
        $t("GameCfg_Toast_Applied", {
          label: snap.label,
          dest: labelFor(destId),
          parts: partsSummary(res.copiedParts ?? []),
        }),
        destId,
      );
      await form.refreshGame();
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
      await form.refreshGame();
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
      await form.refreshGame();
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

  function openApplyMenu(e: MouseEvent, snap: Snapshot): void {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openContextMenuAtRect(
      { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height },
      accounts.map((a) => ({ label: labelFor(a.steamId64), action: () => void applySnapshot(snap, a.steamId64) })),
    );
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

<div class="cfg-page">
  <PageHeader title={$t("GameCfg_Heading")} description={$t("GameCfg_Intro")} />

  <div class="cfg-page__scroll">
    <SettingsCard title={$t("GameCfg_Section_Save")}>
      <GameConfigCopyForm
        bind:this={form}
        detailed
        copyActions={false}
        bind:accounts
        bind:appId
        bind:info
        bind:sourceId
        bind:selectedParts
        bind:busy
        on:refreshed={(e) => void refreshSnapshots(e.detail).catch(toastError)}
      >
        <button
          slot="actions"
          type="button"
          class="ss-btn"
          disabled={busy || !sourceId || !appId || selectedParts.length === 0}
          on:click={doSnapshot}
        >
          {$t("GameCfg_Button_Snapshot")}
        </button>
      </GameConfigCopyForm>
    </SettingsCard>

    <SettingsCard title={$t("GameCfg_Section_Library")} description={$t("GameCfg_Note_AutoRevert")}>
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
              <button type="button" class="ss-btn snap__btn" aria-haspopup="menu" disabled={busy || accounts.length === 0} on:click={(e) => openApplyMenu(e, snap)}>
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

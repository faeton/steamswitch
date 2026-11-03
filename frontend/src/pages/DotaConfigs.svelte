<script lang="ts">
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
    GetDotaAccountStatuses,
    CopyDotaConfigBetween,
    SnapshotDotaConfig,
    ListDotaSnapshots,
    ApplyDotaSnapshot,
    RenameDotaSnapshot,
    DeleteDotaSnapshot,
    OpenDotaSnapshotFolder,
    IsSteamRunning,
  } from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import "../styles/Settings.scss";

  type Account = { steamId64: string; accountName: string; displayName: string };
  type Snapshot = {
    id: string;
    label: string;
    note?: string;
    sourceSteamId64?: string;
    sourceAccountName?: string;
    parts: string[];
    createdAt: string;
    auto: boolean;
    sizeBytes: number;
  };

  const PART_LOCAL = "local";
  const PART_REMOTE = "remote";
  const PART_GLOBAL = "globalcfg";

  let accounts: Account[] = [];
  let presentByAccount: Record<string, string[]> = {};
  let snapshots: Snapshot[] = [];

  let sourceId = "";
  let destId = "";
  let parts: Record<string, boolean> = {
    [PART_LOCAL]: true,
    [PART_REMOTE]: true,
    [PART_GLOBAL]: false,
  };

  let steamRunning = false;
  let busy = false;
  let loadError = "";

  $: appBarTitle.set($t("Title_DotaConfigs"));
  $: selectedParts = Object.keys(parts).filter((k) => parts[k]);
  // The global cfg folder is machine-wide, so it can never differ between two
  // accounts. It is offered for snapshots only.
  $: copyParts = selectedParts.filter((p) => p !== PART_GLOBAL);
  $: canCopy =
    !busy && !!sourceId && !!destId && sourceId !== destId && copyParts.length > 0;
  $: cloudWarning = parts[PART_REMOTE] && steamRunning;

  /**
   * Ask before writing cloud-synced files under a running Steam.
   *
   * The backend refuses this by default and takes the answer as an argument, so the choice
   * has to be made here. It is a question rather than a refusal because remote/ holds the
   * keybinds, hero grids and item builds — refusing outright declined the main reason anyone
   * opens this page, and "close Steam first" is a worse answer than the truth.
   *
   * Returns false when there is no risk to accept, true when the user accepted it, and null
   * when they backed out — which the caller must treat as cancel, not as "no".
   */
  async function cloudRiskAccepted(usesRemote: boolean): Promise<boolean | null> {
    if (!usesRemote || !steamRunning) return false;
    const ok = await openConfirm({
      title: $t("Dota_Confirm_CloudRiskTitle"),
      body: $t("Dota_Confirm_CloudRiskBody"),
    });
    return ok ? true : null;
  }

  function accountLabel(a: Account): string {
    return a.displayName?.trim() || a.accountName?.trim() || a.steamId64;
  }

  function labelFor(id: string): string {
    const a = accounts.find((x) => x.steamId64 === id);
    return a ? accountLabel(a) : id;
  }

  function formatBytes(n: number): string {
    if (!n) return "0 KB";
    const kb = n / 1024;
    if (kb < 1024) return `${Math.max(1, Math.round(kb))} KB`;
    return `${(kb / 1024).toFixed(1)} MB`;
  }

  function formatDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  function partsSummary(list: string[]): string {
    return list.map((p) => $t(`Dota_Part_${p}`)).join(", ");
  }

  async function refreshAll(): Promise<void> {
    try {
      accounts = (await GetSteamAccounts()) ?? [];
      const ids = accounts.map((a) => a.steamId64);
      const statuses = (await GetDotaAccountStatuses(ids)) ?? [];
      presentByAccount = Object.fromEntries(
        statuses.map((s: { steamId64: string; presentParts: string[] }) => [
          s.steamId64,
          s.presentParts ?? [],
        ]),
      );
      snapshots = (await ListDotaSnapshots()) ?? [];
      steamRunning = await IsSteamRunning();
      if (!sourceId) sourceId = accounts.find((a) => hasDotaData(a.steamId64))?.steamId64 ?? "";
      loadError = "";
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    }
  }

  function hasDotaData(id: string): boolean {
    const p = presentByAccount[id] ?? [];
    return p.includes(PART_LOCAL) || p.includes(PART_REMOTE);
  }

  onMount(() => {
    previousPage.set({ page: "tools" });
    void refreshAll();
  });

  /** Latest revert point, offered as a one-click undo after a write. */
  let lastRevertId = "";
  let lastRevertLabel = "";

  function reportResult(
    res: { copiedParts?: string[]; revertSnapshotId?: string; cloudFollowUp?: boolean },
    message: string,
    dest: string,
  ): void {
    lastRevertId = res.revertSnapshotId ?? "";
    lastRevertLabel = dest;
    pushToast({ type: "success", message, duration: 6000 });
    if (res.cloudFollowUp) {
      // Closing Steam prevents the easy failure, but Steam Cloud can still show a
      // conflict on next launch. Say so rather than implying the copy is durable.
      pushToast({ type: "info", message: $t("Dota_Toast_CloudFollowUp"), duration: 12000 });
    }
  }

  async function undoLastWrite(): Promise<void> {
    if (!lastRevertId || busy) return;
    // Undoing a write under a running Steam carries the same Cloud hazard as making one, and
    // asking again is not redundant: the user accepted the risk for a copy they wanted, which
    // is not the same as accepting it for putting things back.
    const risk = await cloudRiskAccepted(selectedParts.includes(PART_REMOTE));
    if (risk === null) return;
    busy = true;
    try {
      await ApplyDotaSnapshot(lastRevertId, destId, selectedParts, risk);
      pushToast({ type: "success", message: $t("Dota_Toast_Reverted"), duration: 5000 });
      lastRevertId = "";
      await refreshAll();
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
    pushToast({
      type: "error",
      message: translated === key ? raw : translated,
      duration: 8000,
    });
  }

  async function doCopy(): Promise<void> {
    if (!canCopy) return;
    const ok = await openConfirm({
      title: $t("Dota_Confirm_CopyTitle"),
      body: $t("Dota_Confirm_CopyBody", {
        source: labelFor(sourceId),
        dest: labelFor(destId),
        parts: partsSummary(copyParts),
      }),
    });
    if (!ok) return;
    const risk = await cloudRiskAccepted(copyParts.includes(PART_REMOTE));
    if (risk === null) return;
    busy = true;
    try {
      const res = await CopyDotaConfigBetween(sourceId, destId, copyParts, risk);
      reportResult(
        res,
        $t("Dota_Toast_Copied", {
          dest: labelFor(destId),
          parts: partsSummary(res.copiedParts ?? []),
        }),
        labelFor(destId),
      );
      await refreshAll();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function doSnapshot(): Promise<void> {
    if (!sourceId || busy) return;
    const label = await openPrompt({
      title: $t("Dota_Prompt_SnapshotLabel"),
      initialValue: labelFor(sourceId),
    });
    if (label === null) return;
    if (!label.trim()) {
      pushToast({ type: "error", message: $t("Toast_Dota_InvalidLabel"), duration: 5000 });
      return;
    }
    const note = (await openPrompt({ title: $t("Dota_Prompt_SnapshotNote"), initialValue: "" })) ?? "";
    busy = true;
    try {
      await SnapshotDotaConfig(sourceId, label, note, selectedParts);
      pushToast({ type: "success", message: $t("Dota_Toast_Snapshotted", { label }), duration: 5000 });
      await refreshAll();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function applySnapshot(snap: Snapshot): Promise<void> {
    if (!destId) {
      pushToast({ type: "error", message: $t("Dota_Error_PickDestination"), duration: 5000 });
      return;
    }
    const ok = await openConfirm({
      title: $t("Dota_Confirm_ApplyTitle"),
      body:
        $t("Dota_Confirm_ApplyBody", {
          label: snap.label,
          dest: labelFor(destId),
          parts: partsSummary(snap.parts.filter((x) => selectedParts.includes(x))),
        }) +
        (selectedParts.includes(PART_GLOBAL) && snap.parts.includes(PART_GLOBAL)
          ? "\n\n" + $t("Dota_Confirm_GlobalWarning")
          : ""),
    });
    if (!ok) return;
    // What actually gets written is the intersection: a snapshot with no remote/ carries no
    // cloud risk however the checkboxes are set.
    const risk = await cloudRiskAccepted(
      selectedParts.includes(PART_REMOTE) && snap.parts.includes(PART_REMOTE),
    );
    if (risk === null) return;
    busy = true;
    try {
      const res = await ApplyDotaSnapshot(snap.id, destId, selectedParts, risk);
      reportResult(
        res,
        $t("Dota_Toast_Applied", {
          label: snap.label,
          dest: labelFor(destId),
          parts: partsSummary(res.copiedParts ?? []),
        }),
        labelFor(destId),
      );
      await refreshAll();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function renameSnapshot(snap: Snapshot): Promise<void> {
    const label = await openPrompt({
      title: $t("Dota_Prompt_SnapshotLabel"),
      initialValue: snap.label,
    });
    if (label === null) return;
    const note =
      (await openPrompt({ title: $t("Dota_Prompt_SnapshotNote"), initialValue: snap.note ?? "" })) ?? "";
    busy = true;
    try {
      await RenameDotaSnapshot(snap.id, label, note);
      await refreshAll();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function deleteSnapshot(snap: Snapshot): Promise<void> {
    const ok = await openConfirm({
      title: $t("Dota_Confirm_DeleteTitle"),
      destructive: true,
      body: $t("Dota_Confirm_DeleteBody", { label: snap.label }),
    });
    if (!ok) return;
    busy = true;
    try {
      await DeleteDotaSnapshot(snap.id);
      await refreshAll();
    } catch (e) {
      toastError(e);
    } finally {
      busy = false;
    }
  }

  async function openSnapshot(snap: Snapshot): Promise<void> {
    try {
      await OpenDotaSnapshotFolder(snap.id);
    } catch (e) {
      toastError(e);
    }
  }

  /** Rename, open and delete sit behind ⋯; Apply — the reason the library exists — stays out. */
  function openSnapMenu(e: MouseEvent, snap: (typeof snapshots)[number]): void {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openContextMenuAtRect(
      { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height },
      [
        { label: $t("Dota_Button_Rename"), action: () => void renameSnapshot(snap) },
        { label: $t("Button_OpenBackup"), action: () => void openSnapshot(snap) },
        { type: "separator" },
        { label: $t("Dota_Button_Delete"), danger: true, action: () => void deleteSnapshot(snap) },
      ],
    );
  }
</script>

<div class="dota-page">
  <PageHeader title={$t("Dota_Heading")} description={$t("Dota_Intro")} />

  <div class="dota-page__scroll">
    {#if loadError}
      <p class="dota-error" role="alert"><Icon name="warn" size={14} />{loadError}</p>
    {/if}

    <SettingsCard title={$t("Dota_Section_Copy")} description={$t("Dota_Note_AutoRevert")}>
      <SettingRow label={$t("Dota_Label_Source")} controlId="dota-source">
        <select id="dota-source" class="ss-field dota-select" bind:value={sourceId} disabled={busy}>
          <option value="">{$t("Dota_Label_PickAccount")}</option>
          {#each accounts as a (a.steamId64)}
            <option value={a.steamId64}>
              {accountLabel(a)}{hasDotaData(a.steamId64) ? "" : ` — ${$t("Dota_Label_NoConfig")}`}
            </option>
          {/each}
        </select>
      </SettingRow>

      <SettingRow label={$t("Dota_Label_Destination")} controlId="dota-dest">
        <select id="dota-dest" class="ss-field dota-select" bind:value={destId} disabled={busy}>
          <option value="">{$t("Dota_Label_PickAccount")}</option>
          {#each accounts.filter((a) => a.steamId64 !== sourceId) as a (a.steamId64)}
            <option value={a.steamId64}>{accountLabel(a)}</option>
          {/each}
        </select>
      </SettingRow>

      <fieldset class="parts">
        <legend class="parts__legend">{$t("Dota_Label_Parts")}</legend>
        <label class="part">
          <input type="checkbox" class="ss-checkbox" bind:checked={parts[PART_LOCAL]} disabled={busy} />
          <span class="part__text">
            <span class="part__name">{$t("Dota_Part_local")}</span>
            <span class="part__hint">{$t("Dota_Part_local_Hint")}</span>
          </span>
        </label>
        <label class="part">
          <input type="checkbox" class="ss-checkbox" bind:checked={parts[PART_REMOTE]} disabled={busy} />
          <span class="part__text">
            <span class="part__name">{$t("Dota_Part_remote")}</span>
            <span class="part__hint">{$t("Dota_Part_remote_Hint")}</span>
          </span>
        </label>
        <label class="part">
          <input type="checkbox" class="ss-checkbox" bind:checked={parts[PART_GLOBAL]} disabled={busy} />
          <span class="part__text">
            <span class="part__name">{$t("Dota_Part_globalcfg")}</span>
            <span class="part__hint">{$t("Dota_Part_globalcfg_Hint")}</span>
          </span>
        </label>
      </fieldset>

      {#if cloudWarning || parts[PART_GLOBAL]}
        <div class="dota-warnings">
          {#if cloudWarning}
            <p class="dota-warning"><Icon name="warn" size={14} />{$t("Dota_Warning_SteamRunning")}</p>
          {/if}
          {#if parts[PART_GLOBAL]}
            <p class="dota-warning"><Icon name="warn" size={14} />{$t("Dota_Warning_GlobalCopyExcluded")}</p>
          {/if}
        </div>
      {/if}

      <div class="dota-actions">
        <button type="button" class="ss-btn ss-btn--primary" disabled={!canCopy} on:click={doCopy}>
          {$t("Dota_Button_Copy")}
        </button>
        <button type="button" class="ss-btn" disabled={busy || !sourceId} on:click={doSnapshot}>
          {$t("Dota_Button_Snapshot")}
        </button>
        {#if lastRevertId}
          <button type="button" class="ss-btn" disabled={busy} on:click={undoLastWrite}>
            {$t("Dota_Button_Undo", { dest: lastRevertLabel })}
          </button>
        {/if}
      </div>
    </SettingsCard>

    <SettingsCard title={$t("Dota_Section_Library")}>
      {#if snapshots.length === 0}
        <p class="dota-empty">{$t("Dota_Library_Empty")}</p>
      {:else}
        {#each snapshots as snap (snap.id)}
          <div class="snap">
            <div class="snap__text">
              <span class="snap__name-line">
                <span class="snap__name">{snap.label}</span>
                {#if snap.auto}<span class="snap__badge">{$t("Dota_Badge_RevertPoint")}</span>{/if}
              </span>
              {#if snap.note}<span class="snap__note">{snap.note}</span>{/if}
              <span class="snap__meta">
                {formatDate(snap.createdAt)} · {partsSummary(snap.parts)} · {formatBytes(snap.sizeBytes)}
                {#if snap.sourceAccountName} · {snap.sourceAccountName}{/if}
              </span>
            </div>
            <div class="snap__actions">
              <button type="button" class="ss-btn snap__btn" disabled={busy} on:click={() => applySnapshot(snap)}>
                {$t("Dota_Button_Apply")}
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
  .dota-page {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .dota-page__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-7);
    padding: 0 var(--space-7) var(--space-6);
    max-width: 880px;
  }

  .dota-select {
    width: 280px;
    max-width: 100%;
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

  .dota-warnings {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-5);
  }

  .dota-warning,
  .dota-error {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-warn);
  }

  .dota-warning :global(svg),
  .dota-error :global(svg) {
    margin-top: 3px;
  }

  .dota-error {
    color: var(--fg-fail);
  }

  .dota-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-5);
  }

  .dota-empty {
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

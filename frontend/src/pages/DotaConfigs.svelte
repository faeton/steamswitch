<script lang="ts">
  import { onMount } from "svelte";
  import { route, previousPage, appBarTitle } from "../stores/nav";
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
    busy = true;
    try {
      await ApplyDotaSnapshot(lastRevertId, destId, selectedParts);
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
    busy = true;
    try {
      const res = await CopyDotaConfigBetween(sourceId, destId, copyParts);
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
    busy = true;
    try {
      const res = await ApplyDotaSnapshot(snap.id, destId, selectedParts);
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
</script>

<div class="settings-page dota-page">
  <h1>{$t("Dota_Heading")}</h1>
  <p class="subtle">{$t("Dota_Intro")}</p>

  {#if loadError}
    <p class="error">{loadError}</p>
  {/if}

  <section>
    <h2>{$t("Dota_Section_Copy")}</h2>

    <div class="row">
      <label for="dota-source">{$t("Dota_Label_Source")}</label>
      <select id="dota-source" bind:value={sourceId} disabled={busy}>
        <option value="">{$t("Dota_Label_PickAccount")}</option>
        {#each accounts as a (a.steamId64)}
          <option value={a.steamId64}>
            {accountLabel(a)}{hasDotaData(a.steamId64) ? "" : ` — ${$t("Dota_Label_NoConfig")}`}
          </option>
        {/each}
      </select>
    </div>

    <div class="row">
      <label for="dota-dest">{$t("Dota_Label_Destination")}</label>
      <select id="dota-dest" bind:value={destId} disabled={busy}>
        <option value="">{$t("Dota_Label_PickAccount")}</option>
        {#each accounts.filter((a) => a.steamId64 !== sourceId) as a (a.steamId64)}
          <option value={a.steamId64}>{accountLabel(a)}</option>
        {/each}
      </select>
    </div>

    <fieldset class="parts">
      <legend>{$t("Dota_Label_Parts")}</legend>
      <label>
        <input type="checkbox" bind:checked={parts[PART_LOCAL]} disabled={busy} />
        {$t("Dota_Part_local")} <span class="hint">{$t("Dota_Part_local_Hint")}</span>
      </label>
      <label>
        <input type="checkbox" bind:checked={parts[PART_REMOTE]} disabled={busy} />
        {$t("Dota_Part_remote")} <span class="hint">{$t("Dota_Part_remote_Hint")}</span>
      </label>
      <label>
        <input type="checkbox" bind:checked={parts[PART_GLOBAL]} disabled={busy} />
        {$t("Dota_Part_globalcfg")} <span class="hint">{$t("Dota_Part_globalcfg_Hint")}</span>
      </label>
    </fieldset>

    {#if cloudWarning}
      <p class="warning">{$t("Dota_Warning_SteamRunning")}</p>
    {/if}
    {#if parts[PART_GLOBAL]}
      <p class="warning">{$t("Dota_Warning_GlobalCopyExcluded")}</p>
    {/if}

    <div class="actions">
      <button class="p-btn p-prim-col" disabled={!canCopy} on:click={doCopy}>
        {$t("Dota_Button_Copy")}
      </button>
      <button class="p-btn" disabled={busy || !sourceId} on:click={doSnapshot}>
        {$t("Dota_Button_Snapshot")}
      </button>
      {#if lastRevertId}
        <button class="p-btn" disabled={busy} on:click={undoLastWrite}>
          {$t("Dota_Button_Undo", { dest: lastRevertLabel })}
        </button>
      {/if}
    </div>
    <p class="subtle small">{$t("Dota_Note_AutoRevert")}</p>
  </section>

  <section>
    <h2>{$t("Dota_Section_Library")}</h2>
    {#if snapshots.length === 0}
      <p class="subtle">{$t("Dota_Library_Empty")}</p>
    {:else}
      <ul class="snapshots">
        {#each snapshots as snap (snap.id)}
          <li class:auto={snap.auto}>
            <div class="meta">
              <strong>{snap.label}</strong>
              {#if snap.auto}<span class="badge">{$t("Dota_Badge_RevertPoint")}</span>{/if}
              {#if snap.note}<span class="note">{snap.note}</span>{/if}
              <span class="subtle small">
                {formatDate(snap.createdAt)} · {partsSummary(snap.parts)} · {formatBytes(snap.sizeBytes)}
                {#if snap.sourceAccountName} · {snap.sourceAccountName}{/if}
              </span>
            </div>
            <div class="snap-actions">
              <button class="p-btn" disabled={busy} on:click={() => applySnapshot(snap)}>
                {$t("Dota_Button_Apply")}
              </button>
              <button class="p-btn" disabled={busy} on:click={() => renameSnapshot(snap)}>
                {$t("Dota_Button_Rename")}
              </button>
              <button class="p-btn" disabled={busy} on:click={() => openSnapshot(snap)}>
                {$t("Button_OpenBackup")}
              </button>
              <button class="p-btn danger" disabled={busy} on:click={() => deleteSnapshot(snap)}>
                {$t("Dota_Button_Delete")}
              </button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <button class="p-btn back" on:click={() => route.set({ page: "tools" })}>
    {$t("Button_Back")}
  </button>
</div>

<style lang="scss">
  .dota-page {
    padding: 1rem 1.25rem 2rem;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin: 0.5rem 0;
    label {
      min-width: 9rem;
    }
    select {
      flex: 1 1 auto;
      max-width: 28rem;
    }
  }
  .parts {
    margin: 1rem 0;
    border: 1px solid rgba(128, 128, 128, 0.35);
    border-radius: 6px;
    padding: 0.75rem 1rem;
    label {
      display: block;
      margin: 0.35rem 0;
    }
  }
  .hint {
    opacity: 0.7;
    font-size: 0.85em;
  }
  .subtle {
    opacity: 0.75;
  }
  .small {
    font-size: 0.85em;
  }
  .warning {
    color: #e0a030;
  }
  .error {
    color: #e05252;
  }
  .actions {
    display: flex;
    gap: 0.6rem;
    margin: 0.75rem 0 0.25rem;
    flex-wrap: wrap;
  }
  .snapshots {
    list-style: none;
    margin: 0;
    padding: 0;
    li {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 1rem;
      flex-wrap: wrap;
      padding: 0.6rem 0;
      border-bottom: 1px solid rgba(128, 128, 128, 0.25);
      &.auto {
        opacity: 0.85;
      }
    }
    .meta {
      display: flex;
      flex-direction: column;
      gap: 0.15rem;
      min-width: 16rem;
    }
    .note {
      font-style: italic;
      opacity: 0.8;
    }
  }
  .badge {
    display: inline-block;
    font-size: 0.75em;
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    background: rgba(128, 128, 128, 0.25);
  }
  .snap-actions {
    display: flex;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .back {
    margin-top: 1.5rem;
  }
</style>

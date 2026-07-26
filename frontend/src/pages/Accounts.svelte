<script lang="ts">
  /**
   * The main surface (REDESIGN.md §3): one column of account tiles, a persistent status
   * strip, and a Tools · Settings footer. Nothing else lives here.
   *
   * Steam-only by construction — no adapter indirection, no platform grid. Data loading
   * reuses the pipeline helpers from `lib/accounts/*` so the list/enrichment merge and the
   * avatar-epoch handling stay shared with the code they were extracted from.
   */
  import { onDestroy, onMount } from "svelte";
  import { get } from "svelte/store";
  import { Events } from "@wailsio/runtime";
  import AccountTile from "../components/AccountTile.svelte";
  import AccountListSkeleton from "../components/AccountListSkeleton.svelte";
  import { t } from "../stores/i18n";
  import { route } from "../stores/nav";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { offerRestartIfNeedsAdmin } from "../lib/adminFlow";
  import { openContextMenu } from "../stores/contextMenu";
  import {
    beginSwitch,
    clearError,
    endSwitch,
    retryTarget,
    setError,
    setIdleFacts,
    statusStripAction,
    switchingBlocked,
  } from "../stores/statusStrip";
  import {
    kitIsActive,
    kitStatus,
    leavePrompt,
    needsRecovery,
    refreshKitStatus,
    relabelKit,
  } from "../stores/sessionKit";
  import KitLeaveModal from "../components/KitLeaveModal.svelte";
  import KitRecoveryModal from "../components/KitRecoveryModal.svelte";
  import { platformActionBusy } from "../stores/platformPage";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import * as SessionKitService from "../../bindings/steamswitch/internal/steam/sessionkitservice.js";
  import {
    AccountPatch,
    SteamAccountEnrichmentDTO,
    SteamAccountListItemDTO,
  } from "../../bindings/steamswitch/internal/steam/models.js";
  import type { SteamAccountRow } from "../lib/steam/types";
  import {
    EMPTY_ROLES,
    kitTravelsTo,
    orderAccountIds,
    roleOf,
    type AccountRoleMap,
  } from "../lib/steam/accountRoles";
  import { buildAccountMenu } from "../lib/steam/accountMenu";
  import { refreshVault, vaultEntries, vaultHealth } from "../stores/vault";
  import type { TagDefRow } from "../lib/accountTagsContext";
  import * as BasicService from "../../bindings/steamswitch/internal/basic/basicservice.js";
  import { createLatestRequestGuard } from "../lib/accounts/windowFocusRefresh";
  import { buildEpochMap } from "../lib/accounts/epochManager";
  import "../styles/platformAccountsShared.scss";

  let accounts: SteamAccountRow[] = [];
  let roles: AccountRoleMap = EMPTY_ROLES;
  let avatarEpoch: Record<string, number> = {};
  let loaded = false;
  let steamRunning = false;
  let rootEl: HTMLDivElement | null = null;
  let offPatch: (() => void) | undefined;
  let addingAccount = false;
  let tagDefs: TagDefRow[] = [];

  const RETRY_SWITCH = "retry-switch";

  const listGuard = createLatestRequestGuard();

  $: byId = new Map(accounts.map((a) => [a.steamId64, a] as const));
  $: orderedIds = orderAccountIds(roles, accounts.map((a) => a.steamId64));
  $: ordered = orderedIds.map((id) => byId.get(id)).filter((a): a is SteamAccountRow => !!a);
  $: currentId = accounts.find((a) => a.currentSession)?.steamId64 ?? "";
  $: blocked = $switchingBlocked;
  $: showRecovery = needsRecovery($kitStatus);

  function labelOf(acc: SteamAccountRow | undefined): string {
    if (!acc) return "";
    return acc.displayName?.trim() || acc.personaName?.trim() || acc.steamId64;
  }

  /**
   * Keep the strip's "Now: …" line in step with whichever account Steam is set to.
   *
   * Reads `accounts` directly rather than the `$:` derivations above: this runs
   * synchronously right after an assignment to `accounts`, and Svelte has not recomputed
   * `byId`/`currentId` at that point, so those would still describe the previous list.
   */
  function refreshIdleFacts(): void {
    const cur = accounts.find((a) => a.currentSession);
    setIdleFacts({
      accountLabel: labelOf(cur),
      isHome: !!cur && roleOf(roles, cur.steamId64) === "home",
      steamRunning,
    });
  }

  async function refreshSteamRunning(): Promise<void> {
    try {
      steamRunning = await SteamService.IsSteamRunning();
    } catch {
      steamRunning = false;
    }
    refreshIdleFacts();
  }

  async function loadRoles(): Promise<void> {
    try {
      const r = await SteamService.GetAccountRoles();
      roles = { homeSteamId64: r.homeSteamId64 ?? "", sharedIds: r.sharedIds ?? [] };
    } catch {
      roles = EMPTY_ROLES;
    }
  }

  /**
   * Tag definitions for the account menu's "Tags ▸ Add" list.
   *
   * Failing to load them is not worth a toast: the menu still works, it just cannot offer
   * existing tags to pick from, and typing a name creates one.
   */
  async function loadTagDefs(): Promise<void> {
    try {
      const defs = await BasicService.ListTagDefinitions("Steam");
      tagDefs = (defs ?? []) as unknown as TagDefRow[];
    } catch {
      tagDefs = [];
    }
  }

  /**
   * Vault entries, for the tile health dots and for whether the vault menu rows are enabled.
   *
   * A locked or absent vault is the normal case on most machines and must read as "no
   * entries" without an error: the whole feature is opt-in, and a machine that never enables
   * it should never see it complain.
   */
  async function loadVault(): Promise<void> {
    try {
      await refreshVault();
    } catch {
      // Already reflected as an empty list by the store; nothing to say.
    }
  }

  async function loadAccounts(): Promise<void> {
    const seq = listGuard.begin();
    try {
      const rows = await SteamService.GetSteamAccountsList();
      if (!listGuard.isCurrent(seq)) return;
      const list = rows.map(
        (r: SteamAccountListItemDTO) =>
          ({
            steamId64: r.steamId64,
            personaName: r.personaName,
            displayName: r.displayName,
            accountName: r.accountName,
            currentSession: r.currentSession ?? false,
          }) as SteamAccountRow,
      );
      accounts = mergeById(accounts, list);
      loaded = true;
      refreshIdleFacts();

      const enrich = await SteamService.GetSteamAccountsEnrichment();
      if (!listGuard.isCurrent(seq)) return;
      // Enrichment decorates the rows the list call returned; it must never decide which
      // accounts exist. A partial or empty enrichment response would otherwise silently
      // drop rows the user can see.
      accounts = mergeEnrichment(accounts, enrich as unknown as SteamAccountRow[]);
      refreshIdleFacts();
      SteamService.StartSteamProfileRefresh();
    } catch (e) {
      if (!listGuard.isCurrent(seq)) return;
      loaded = true;
      setError(formatToastWithError(get(t)("Toast_LoadAccountsFail"), e));
    }
  }

  /**
   * Replace the list with `incoming`, keeping any field an incoming row omits.
   *
   * The incoming set defines membership, so this is only for full list/enrichment loads.
   * Single-row updates go through `patchOne`, which must not shrink the list.
   */
  function mergeById(existing: SteamAccountRow[], incoming: SteamAccountRow[]): SteamAccountRow[] {
    const prev = new Map(existing.map((a) => [a.steamId64, a] as const));
    const merged = incoming.map((row) => {
      const before = prev.get(row.steamId64);
      return before ? ({ ...before, ...row } as SteamAccountRow) : row;
    });
    avatarEpoch = buildEpochMap(
      merged as unknown as Record<string, unknown>[],
      prev as unknown as Map<string, Record<string, unknown>>,
      (r: unknown) => (r as SteamAccountRow).steamId64,
      avatarEpoch,
    );
    return merged;
  }

  /**
   * Keep only the fields a patch actually carries.
   *
   * `AccountPatch.createFrom` materialises every field, so an event that only reports a new
   * VAC status still arrives with `imageUrl: ""`, `avatarFrameUrl: ""` and friends. Spreading
   * that wholesale blanks the avatar and mini-profile mid-refresh, so empty strings are
   * treated as "not included" for the text fields that are never legitimately cleared this
   * way. Booleans are always meaningful — `vac: false` is a real value.
   */
  function meaningfulPatchFields(patch: AccountPatch): Partial<SteamAccountRow> {
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(patch as unknown as Record<string, unknown>)) {
      if (key === "steamId64") continue;
      if (typeof value === "string" && value.trim() === "") continue;
      if (value === undefined || value === null) continue;
      out[key] = value;
    }
    return out as Partial<SteamAccountRow>;
  }

  /** Fold enrichment rows into the existing list without changing its membership. */
  function mergeEnrichment(existing: SteamAccountRow[], enrich: SteamAccountRow[]): SteamAccountRow[] {
    const byIncoming = new Map(enrich.map((a) => [a.steamId64, a] as const));
    const prev = new Map(existing.map((a) => [a.steamId64, a] as const));
    const next = existing.map((row) => {
      const patch = byIncoming.get(row.steamId64);
      return patch ? ({ ...row, ...patch } as SteamAccountRow) : row;
    });
    avatarEpoch = buildEpochMap(
      next as unknown as Record<string, unknown>[],
      prev as unknown as Map<string, Record<string, unknown>>,
      (r: unknown) => (r as SteamAccountRow).steamId64,
      avatarEpoch,
    );
    return next;
  }

  /** Apply a single-account patch in place, leaving every other row alone. */
  function patchOne(id: string, patch: Partial<SteamAccountRow>): void {
    let seen = false;
    const next = accounts.map((a) => {
      if (a.steamId64 !== id) return a;
      seen = true;
      return { ...a, ...patch } as SteamAccountRow;
    });
    // A patch for an account we have never listed means the list is out of date; reload
    // rather than inventing a half-populated row.
    if (!seen) {
      void loadAccounts();
      return;
    }
    const prev = new Map(accounts.map((a) => [a.steamId64, a] as const));
    avatarEpoch = buildEpochMap(
      next as unknown as Record<string, unknown>[],
      prev as unknown as Map<string, Record<string, unknown>>,
      (r: unknown) => (r as SteamAccountRow).steamId64,
      avatarEpoch,
    );
    accounts = next;
    refreshIdleFacts();
  }

  /**
   * Switch to an account, going through the Session Kit engine rather than a bare swap.
   *
   * Two things have to happen before the engine is called. First, if a kit is currently live
   * on a shared account, leaving it is a decision the user owns — so the switch stops here
   * and raises the prompt instead. Second, the engine refuses outright while a recovery is
   * outstanding, and asking it only to show an error would be worse than not asking.
   */
  async function switchTo(steamId64: string): Promise<void> {
    if (get(switchingBlocked)) return;
    const acc = byId.get(steamId64);
    if (!acc || acc.currentSession) return;

    const kit = get(kitStatus);
    if (kitIsActive(kit) && kit.targetSteamId64 !== steamId64) {
      leavePrompt.set({
        fromLabel: labelOf(byId.get(kit.targetSteamId64)) || kit.targetSteamId64,
        toLabel: labelOf(acc),
        toSteamId64: steamId64,
        moduleNames: kit.moduleNames ?? [],
        onQuit: false,
      });
      return;
    }

    await runSwitch(steamId64, labelOf(acc));
  }

  /** Same path as `switchTo`, with an explicit persona state from the Advanced submenu. */
  async function switchToAs(steamId64: string, personaState: number): Promise<void> {
    if (get(switchingBlocked)) return;
    const acc = byId.get(steamId64);
    if (!acc) return;
    const kit = get(kitStatus);
    if (kitIsActive(kit) && kit.targetSteamId64 !== steamId64) {
      leavePrompt.set({
        fromLabel: labelOf(byId.get(kit.targetSteamId64)) || kit.targetSteamId64,
        toLabel: labelOf(acc),
        toSteamId64: steamId64,
        moduleNames: kit.moduleNames ?? [],
        onQuit: false,
      });
      return;
    }
    await runSwitch(steamId64, labelOf(acc), personaState);
  }

  /** The actual engine call, shared by a plain switch and by "retry". */
  async function runSwitch(steamId64: string, label: string, personaState = -1): Promise<void> {
    if (!beginSwitch(label)) return;
    platformActionBusy.set({ busy: true, platformKey: "Steam" });
    try {
      await SessionKitService.SwitchTo(steamId64, personaState);
      endSwitch();
      await afterSwitch();
    } catch (e) {
      // No success toast on the happy path any more: the strip is the narration channel.
      retryTarget.set(steamId64);
      setError(formatToastWithError(get(t)("Toast_SwitchFailed"), e), {
        id: RETRY_SWITCH,
        labelKey: "Kit_Action_Retry",
      });
      // A failure can be a *blocking* one (interrupted transaction, external change), and
      // only the engine knows which. Re-reading it turns a dead error line into the recovery
      // prompt that can actually clear it.
      void refreshKitStatus();
      // A permission failure is not something the user can act on from an error line. The
      // offer to restart elevated was reachable from the old account page and went missing
      // with it, leaving "switch failed" as the whole story on an install the app cannot
      // write to. No-op unless the error is actually an elevation one.
      void offerRestartIfNeedsAdmin(e, "Steam");
    } finally {
      platformActionBusy.set({ busy: false, platformKey: "" });
    }
  }

  /** Reload everything a completed switch invalidates, in the order the strip reads it. */
  async function afterSwitch(): Promise<void> {
    await refreshKitStatus();
    await loadAccounts();
    await refreshSteamRunning();
    relabelKit(labelOf(byId.get(get(kitStatus).targetSteamId64)));
  }

  /**
   * Add a new account: clear the saved session so Steam opens its own login screen.
   *
   * Kept on the main surface because without it a first run has no way forward at all — the
   * list is empty and every other route assumes accounts already exist.
   */
  async function addAccount(): Promise<void> {
    if (addingAccount || get(switchingBlocked)) return;
    addingAccount = true;
    try {
      await SteamService.SteamAddNew();
      await afterSwitch();
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_SwitchFailed"), e),
        duration: 8000,
      });
    } finally {
      addingAccount = false;
    }
  }

  async function launchSteam(): Promise<void> {
    try {
      await SteamService.LaunchSteam();
      await refreshSteamRunning();
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_LaunchFailed"), e),
        duration: 8000,
      });
    }
  }

  function onTileMenu(e: CustomEvent<{ id: string; x: number; y: number }>): void {
    const acc = byId.get(e.detail.id);
    if (!acc) return;
    openContextMenu(
      e.detail.x,
      e.detail.y,
      buildAccountMenu({
        account: acc,
        label: labelOf(acc),
        roles,
        blocked,
        tagDefs,
        hasVaultEntry: $vaultEntries.some((v) => v.steamId64 === acc.steamId64),
        t: get(t),
        onSwitch: () => void switchTo(acc.steamId64),
        // Routed through the same gate as a tile click, so "Log in as ▸ Invisible" cannot
        // start a second concurrent swap or skip the leave prompt.
        onSwitchAs: (personaState) => void switchToAs(acc.steamId64, personaState),
        onRolesChanged: (next) => {
          roles = next;
          refreshIdleFacts();
        },
        onAccountsChanged: () => void loadAccounts(),
        onNavigate: (page) => route.set({ page } as never),
        onError: (message, err) =>
          pushToast({ type: "error", message: formatToastWithError(message, err), duration: 8000 }),
        onToast: (message) => pushToast({ type: "success", message, duration: 5000 }),
      }),
    );
  }

  /**
   * Honour strip buttons.
   *
   * A subscription rather than a reactive block: `$:` with side effects re-runs whenever any
   * referenced store changes, so a second unrelated update could fire the retry again.
   */
  function onStripAction(id: string): void {
    if (id !== RETRY_SWITCH) return;
    statusStripAction.set("");
    const target = get(retryTarget);
    retryTarget.set("");
    clearError();
    if (target) {
      void runSwitch(target, labelOf(byId.get(target)) || target);
    }
  }

  function onWindowKeydown(e: KeyboardEvent): void {
    if (e.ctrlKey || e.metaKey || e.altKey || blocked) return;
    // A digit typed while a dialog or context menu is open belongs to that surface, not to
    // the tile list behind it.
    if ($leavePrompt || showRecovery) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest("input, textarea, select, [contenteditable]")) return;
    if (target?.closest("[role='dialog'], [role='alertdialog'], [role='menu']")) return;
    // `1`–`4` switch straight to the first four tiles (REDESIGN.md §3).
    const n = Number.parseInt(e.key, 10);
    if (!Number.isNaN(n) && n >= 1 && n <= 4 && ordered[n - 1]) {
      e.preventDefault();
      void switchTo(ordered[n - 1].steamId64);
    }
  }

  onMount(() => {
    void (async () => {
      await loadRoles();
      // Before the list, so an interrupted transaction blocks switching from the first paint
      // rather than after the user has had a second to click a tile.
      await refreshKitStatus();
      await loadAccounts();
      await refreshSteamRunning();
      relabelKit(labelOf(byId.get(get(kitStatus).targetSteamId64)));
      // Last: nothing on the first paint needs it, only the account menu does.
      await loadTagDefs();
      await loadVault();
    })();

    const offStrip = statusStripAction.subscribe(onStripAction);

    offPatch = Events.On("steam-account-updated", (ev) => {
      const raw = ev.data;
      const patch = raw instanceof AccountPatch ? raw : AccountPatch.createFrom(raw as Record<string, unknown>);
      const id = (patch.steamId64 ?? "").trim();
      if (!id) return;
      patchOne(id, meaningfulPatchFields(patch));
    });

    window.addEventListener("keydown", onWindowKeydown);
    // Steam can be started or closed outside the app, and a kit's safety depends on knowing
    // which; refresh whenever the window comes back to the foreground.
    window.addEventListener("focus", onWindowFocus);
    return () => {
      window.removeEventListener("keydown", onWindowKeydown);
      window.removeEventListener("focus", onWindowFocus);
      offStrip();
    };
  });

  function onWindowFocus(): void {
    void refreshSteamRunning();
    void refreshKitStatus();
  }

  onDestroy(() => {
    offPatch?.();
  });
</script>

<div class="accounts" bind:this={rootEl}>
  <div class="accounts__list" role="list">
    {#if !loaded}
      <AccountListSkeleton />
    {:else if ordered.length === 0}
      <!-- The empty state has to be actionable: with no accounts saved there is nothing to
           click, and "Add account" is the only way to get the first one. -->
      <div class="accounts__empty">
        <p>{$t("Status_NoAccount")}</p>
        <button type="button" class="accounts__cta" disabled={addingAccount} on:click={addAccount}>
          {$t("Button_AddNew")}
        </button>
      </div>
    {:else}
      {#each ordered as acc, i (acc.steamId64)}
        <div role="listitem">
          <AccountTile
            account={acc}
            role={roleOf(roles, acc.steamId64)}
            kitTravels={kitTravelsTo(roles, acc.steamId64)}
            current={acc.steamId64 === currentId}
            disabled={blocked}
            index={i + 1}
            avatarEpoch={avatarEpoch[acc.steamId64] ?? 0}
            boundary={rootEl}
            on:switch={(e) => void switchTo(e.detail)}
            on:menu={onTileMenu}
          />
        </div>
      {/each}
    {/if}
  </div>

  <footer class="accounts__footer">
    {#if ordered.length > 0}
      <button type="button" class="link" disabled={addingAccount || blocked} on:click={addAccount}
        >{$t("Button_AddNew")}</button
      >
      <span aria-hidden="true">·</span>
    {/if}
    {#if !steamRunning}
      <!-- Only offered when it would do something. A permanent Launch button on a surface
           whose whole point is "click an account" is noise (REDESIGN.md §3). -->
      <button type="button" class="link" on:click={launchSteam}>{$t("Button_LaunchSteam")}</button>
      <span aria-hidden="true">·</span>
    {/if}
    <button type="button" class="link" on:click={() => route.set({ page: "tools" })}
      >{$t("Nav_Tools")}</button
    >
    <span aria-hidden="true">·</span>
    <button type="button" class="link" on:click={() => route.set({ page: "settings" })}
      >{$t("Nav_Settings")}</button
    >
  </footer>
</div>

{#if showRecovery}
  <!-- Rendered above the leave prompt: an unresolved transaction outranks a new decision,
       and the engine will refuse the leave anyway until this is answered. -->
  <KitRecoveryModal
    status={$kitStatus}
    targetLabel={labelOf(byId.get($kitStatus.targetSteamId64))}
    on:done={() => void afterSwitch()}
  />
{:else if $leavePrompt}
  <KitLeaveModal
    prompt={$leavePrompt}
    on:cancel={() => leavePrompt.set(null)}
    on:done={() => {
      const target = $leavePrompt?.toSteamId64 ?? "";
      leavePrompt.set(null);
      void afterSwitch().then(() => {
        // LeaveKit already switched to the target; this only re-labels the strip once the
        // reloaded list knows the new account's display name.
        if (target) relabelKit(labelOf(byId.get(target)));
      });
    }}
  />
{/if}

<style>
  .accounts {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }
  .accounts__list {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--main-padding, 0.75rem);
  }
  .accounts__empty {
    color: var(--role-text-muted, var(--text-subtle-gray));
    text-align: center;
    margin-top: 2rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .accounts__cta {
    padding: 8px 16px;
    border-radius: 6px;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: var(--accent-contrast, #fff);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .accounts__cta:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .accounts__cta:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }
  .accounts__footer {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 6px;
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 12px;
  }
  .link {
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 4px;
  }
  .link:hover:not(:disabled) {
    color: var(--white);
    text-decoration: underline;
  }
  .link:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .link:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 1px;
  }
</style>

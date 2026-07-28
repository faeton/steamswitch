<script lang="ts">
  /**
   * The switcher — the app's home surface (REDESIGN_BRIEF.md A6, J1/J2).
   *
   * Three things the brief asks for shape this page:
   *
   *  - **The current account is unmistakable.** A hero card, not a row that happens to be
   *    tinted, and it tells the truth about all five states in A4 — including "we are not
   *    sure", which the old list could not express at all.
   *  - **A switch is visible.** Progress goes to `SwitchProgressDock`, replacing the old
   *    invisible button-disable. The dock is fed by `beginSwitch`/`narrate`, so it lights up
   *    on the same frame as the click.
   *  - **The roster scales.** Search and tag chips appear only once they earn their space
   *    (`shouldShowFilters`), and the grid is a keyed `{#each}` so N=500 stays a plain list.
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
  import AccountDetailPanel from "../components/AccountDetailPanel.svelte";
  import CurrentAccountHero from "../components/CurrentAccountHero.svelte";
  import ShortcutsOverlay from "../components/ShortcutsOverlay.svelte";
  import PageHeader from "../components/PageHeader.svelte";
  import { t } from "../stores/i18n";
  import { route } from "../stores/nav";
  import { openConfirm } from "../stores/modal";
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
    statusStrip,
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
  import { pendingVaultEntry, refreshVault, vaultEntries, vaultStatus } from "../stores/vault";
  import {
    EMPTY_SWITCHER_FILTERS,
    accountForQuickSwitch,
    filterAccounts,
    quickSwitchDigit,
    quickSwitchIndex,
    shouldShowFilters,
    tagsInUse,
    type SwitcherFilters,
  } from "../lib/steam/switcherView";
  import {
    asSessionVerdict,
    UNLOADED_SESSION,
    type SessionVerdict,
  } from "../lib/steam/sessionState";
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
  /** The backend's A4 verdict on who Steam is signed in as. */
  let session: SessionVerdict = UNLOADED_SESSION;
  let rootEl: HTMLDivElement | null = null;
  let offPatch: (() => void) | undefined;
  let addingAccount = false;
  let tagDefs: TagDefRow[] = [];
  let filters: SwitcherFilters = { ...EMPTY_SWITCHER_FILTERS };
  let shortcutsOpen = false;
  /** SteamID64 whose detail panel is open, "" for none. */
  let detailId = "";
  /** SteamID64 of the switch currently running, "" when idle. */
  let inFlightId = "";
  /**
   * Outcome of the last "Add another Steam login", or "" when there is nothing to report.
   *
   * A dismissable banner rather than a toast: brief A13-J3 requires that after the action the
   * user can tell what happened and reach the Steam login in one step, and a toast that has
   * already faded satisfies neither half.
   */
  let addLoginResult: "" | "opened" | "closed" = "";

  const RETRY_SWITCH = "retry-switch";

  const listGuard = createLatestRequestGuard();

  $: byId = new Map(accounts.map((a) => [a.steamId64, a] as const));
  $: orderedIds = orderAccountIds(roles, accounts.map((a) => a.steamId64));
  $: ordered = orderedIds.map((id) => byId.get(id)).filter((a): a is SteamAccountRow => !!a);
  $: currentId = accounts.find((a) => a.currentSession)?.steamId64 ?? "";
  $: currentAccount = currentId ? byId.get(currentId) : undefined;
  /**
   * The account the hero talks about, which is not always the account the grid highlights.
   *
   * Under a mismatch the backend deliberately marks no row `currentSession` — nothing may be
   * highlighted as current when Steam's own selection says otherwise — but the hero still
   * needs the name the app's records point at, so it can show both candidates instead of
   * silently dropping one. `session.steamId64` carries it.
   */
  $: heroAccount = session.steamId64 ? byId.get(session.steamId64) : undefined;
  $: blocked = $switchingBlocked;
  $: showRecovery = needsRecovery($kitStatus);

  // Health only means something when the vault is actually in use; on a machine that never
  // enabled it, every tile would otherwise carry a permanent "never checked".
  $: vaultInUse = $vaultStatus.appPasswordSet && !$vaultStatus.locked && $vaultEntries.length > 0;

  $: availableTags = tagsInUse(ordered);
  $: filtersVisible = shouldShowFilters(ordered.length, availableTags.length);
  // Filters that are not on screen must not silently exclude anything — a stale query from
  // before a delete would hide accounts with no visible way to get them back.
  $: activeFilters = filtersVisible ? filters : EMPTY_SWITCHER_FILTERS;
  $: visible = filterAccounts(ordered, activeFilters);
  $: keycaps = quickSwitchIndex(visible, currentId);
  // Which tile shows the "switching…" state. Tracked locally rather than derived from the
  // strip, because the strip carries a display *label* and two accounts can share one.
  $: switchTargetId =
    $statusStrip.kind === "switching" && !$statusStrip.finished ? inFlightId : "";
  $: detailAccount = detailId ? byId.get(detailId) : undefined;

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
      const payload = await SteamService.GetSteamAccountsList();
      if (!listGuard.isCurrent(seq)) return;
      // The verdict rides with the rows because it is derived from the same read of
      // loginusers.vdf; taking it from a separate call could describe a different moment.
      session = asSessionVerdict(payload.session);
      const list = (payload.accounts ?? []).map(
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
    // The account being left is captured before the engine touches anything: `afterSwitch`
    // reloads the list, and by the time the dock renders "Saving X's login" the old current
    // account is already gone from `currentAccount`.
    if (!beginSwitch(label, labelOf(currentAccount))) return;
    inFlightId = steamId64;
    // Opening the detail panel and switching from it should not leave the panel sitting over
    // an account that is no longer current.
    detailId = "";
    platformActionBusy.set({ busy: true, platformKey: "Steam" });
    try {
      await SessionKitService.SwitchTo(steamId64, personaState);
      // `afterSwitch` refreshes `steamRunning`; the dock's result line is only allowed to say
      // "Steam was relaunched" if Steam actually is.
      await afterSwitch();
      endSwitch(steamRunning);
    } catch (e) {
      // No success toast on the happy path any more: the strip is the narration channel.
      retryTarget.set(steamId64);
      setError(
        formatToastWithError(get(t)("Toast_SwitchFailed"), e),
        { id: RETRY_SWITCH, labelKey: "Kit_Action_Retry" },
        "switch",
      );
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
      inFlightId = "";
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
   * "Add another Steam login" — sign Steam out so the user can log in as someone new
   * (REDESIGN_BRIEF.md A5 flow 3, J3; fixes Part B #7).
   *
   * The old version was a bare `SteamAddNew()` and nothing else, which is why it read as
   * broken. `SteamAddNew` → `SwapToAccount("")` clears the active login and then relaunches
   * Steam **only when `AutoStart` is on**, silently either way. Three changes:
   *
   *  1. Explain before the click, not after (the confirm below).
   *  2. Relaunch by an explicit call to `LaunchSteam`, which routes to `LaunchSteamOnly` and
   *     always launches — so the `AutoStart` gate can no longer produce a silent no-op.
   *  3. Report the outcome as a state the user dismisses, not a toast that evaporates.
   */
  async function addAccount(): Promise<void> {
    if (addingAccount || get(switchingBlocked)) return;

    const choice = { reopenSteam: true };
    const body = await import("../components/modals/AddSteamLoginModalBody.svelte");
    const ok = await openConfirm({
      title: get(t)("AddLogin_Title"),
      positiveLabel: get(t)("AddLogin_Confirm"),
      negativeLabel: get(t)("Button_Cancel"),
      style: "okcancel",
      bodyComponent: body.default,
      bodyProps: { choice, currentLabel: labelOf(currentAccount) },
    });
    if (!ok) return;

    addingAccount = true;
    try {
      await SteamService.SteamAddNew();
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_SwitchFailed"), e),
        duration: 8000,
      });
      addingAccount = false;
      return;
    }

    /*
      From here the sign-out has already happened, so every remaining step is best-effort and
      must not be allowed to skip the reporting — the previous version wrapped the launch in
      the same `try`, so a failed launch left the user signed out with no result state at all
      and only a transient toast.
    */
    try {
      // `SteamAddNew` → `SwapToAccount("")` already relaunches Steam when the `AutoStart`
      // setting is on. Asking again would open it twice, so this only fills the gap the
      // setting leaves — which is the whole reason the button used to look like a no-op.
      await refreshSteamRunning();
      if (choice.reopenSteam && !steamRunning) {
        await SteamService.LaunchSteam();
      }
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_LaunchFailed"), e),
        duration: 8000,
      });
    }

    try {
      await afterSwitch();
    } finally {
      // Reported from what Steam is *actually* doing, not from what the user asked for:
      // with AutoStart on, opting out of the relaunch still leaves Steam open, and claiming
      // "Steam is closed" there would be the same class of lie this flow exists to fix.
      addLoginResult = steamRunning ? "opened" : "closed";
      addingAccount = false;
    }
  }

  /**
   * Re-read everything from the backend.
   *
   * Offered by the hero when the app cannot say who is signed in (brief A4 "mismatch"): the
   * honest response to "this may be out of date" is a way to find out, not a guess.
   */
  async function reloadEverything(): Promise<void> {
    await loadRoles();
    await refreshKitStatus();
    await loadAccounts();
    await refreshSteamRunning();
    await loadVault();
  }

  /**
   * Jump to this account's vault entry.
   *
   * Hands off through a store rather than a route parameter: the SteamID64 is an identifier,
   * not a view, and putting it in the URL would make the back stack full of near-identical
   * `#/vault/765611…` entries.
   */
  function openVaultEditor(steamId64: string): void {
    pendingVaultEntry.set(steamId64);
    route.set({ page: "vault" });
  }

  /** Open Steam at its login screen after an add-login that left it closed. */
  async function openSteamLogin(): Promise<void> {
    try {
      await SteamService.LaunchSteam();
      await refreshSteamRunning();
      addLoginResult = steamRunning ? "opened" : "closed";
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_LaunchFailed"), e),
        duration: 8000,
      });
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
        onNavigate: (target) => route.set(target),
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
    if ($leavePrompt || showRecovery || shortcutsOpen) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest("input, textarea, select, [contenteditable]")) return;
    if (target?.closest("[role='dialog'], [role='alertdialog'], [role='menu']")) return;

    if (e.key === "Escape" && detailId) {
      e.preventDefault();
      detailId = "";
      return;
    }

    /*
      `1`–`9` over the *visible* order, matching the keycap each tile renders — the mapping
      lives in `switcherView.ts` so the handler and the tiles cannot disagree. Shift opens
      detail instead of switching, the same modifier Shift-click uses.
    */
    const n = quickSwitchDigit(e);
    if (n === 0) return;
    const target_ = accountForQuickSwitch(visible, currentId, n);
    if (!target_) return;
    e.preventDefault();
    if (e.shiftKey) {
      detailId = target_.steamId64;
      return;
    }
    void switchTo(target_.steamId64);
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


<div class="switcher" bind:this={rootEl}>
  <PageHeader title={$t("Nav_Switcher")}>
    <svelte:fragment slot="actions">
      <button type="button" class="ss-btn" on:click={() => (shortcutsOpen = true)}>
        {$t("Shortcuts_Title")}
      </button>
      {#if ordered.length > 0}
        <!-- Hidden on an empty roster: the first-run panel below already carries this as its
             focal call to action, and two identical primary buttons on one screen is the
             fastest way to make neither look like the one to press. -->
        <button
          type="button"
          class="ss-btn ss-btn--primary"
          disabled={addingAccount || blocked}
          on:click={addAccount}
        >
          {$t("AddLogin_Title")}
        </button>
      {/if}
    </svelte:fragment>
  </PageHeader>

  <div class="switcher__body">
    <div class="switcher__main">
      <CurrentAccountHero
        account={heroAccount}
        {steamRunning}
        {loaded}
        busy={blocked || addingAccount}
        sessionState={session.state}
        conflictAccountName={session.conflictAccountName ?? ""}
        avatarEpoch={avatarEpoch[currentId] ?? 0}
        on:detail={(e) => (detailId = e.detail)}
        on:launch={() => void launchSteam()}
        on:refresh={() => void reloadEverything()}
      />

      {#if addLoginResult}
        <!-- The result of "Add another Steam login". Persistent and dismissable, because a
             toast that has already faded fails A13-J3's "the user can tell what happened". -->
        <div class="result" role="status">
          <span class="result__dot" data-open={addLoginResult === "opened"} aria-hidden="true"></span>
          <div class="result__text">
            <div class="result__title">
              {addLoginResult === "opened"
                ? $t("AddLogin_Result_Opened")
                : $t("AddLogin_Result_Closed")}
            </div>
            <div class="result__sub">{$t("AddLogin_Result_Sub")}</div>
          </div>
          {#if addLoginResult === "closed"}
            <button type="button" class="ss-btn ss-btn--primary" on:click={() => void openSteamLogin()}>
              {$t("AddLogin_Result_Open")}
            </button>
          {/if}
          <button
            type="button"
            class="ss-btn ss-btn--quiet"
            aria-label={$t("Button_Close")}
            on:click={() => (addLoginResult = "")}>✕</button
          >
        </div>
      {/if}

      <div class="switcher__list-head">
        <div class="switcher__list-title">
          <h2 class="switcher__h2">{$t("Switcher_SwitchTo")}</h2>
          <span class="switcher__count meta-mono">
            {visible.length === ordered.length
              ? $t("Switcher_Count", { count: ordered.length })
              : $t("Switcher_CountFiltered", { shown: visible.length, total: ordered.length })}
          </span>
        </div>

        {#if filtersVisible}
          <!-- Only rendered once it earns the space (`shouldShowFilters`): at three accounts
               a filter bar is taller than the thing it filters. -->
          <div class="switcher__filters">
            {#if availableTags.length}
              <div class="switcher__chips" role="group" aria-label={$t("Detail_Field_Tags")}>
                <button
                  type="button"
                  class="ss-chip"
                  aria-pressed={filters.tag === ""}
                  on:click={() => (filters = { ...filters, tag: "" })}>{$t("Switcher_Tag_All")}</button
                >
                {#each availableTags as tag (tag)}
                  <button
                    type="button"
                    class="ss-chip"
                    aria-pressed={filters.tag === tag}
                    on:click={() => (filters = { ...filters, tag: filters.tag === tag ? "" : tag })}
                    >{tag}</button
                  >
                {/each}
              </div>
            {/if}
            <input
              type="search"
              class="ss-field switcher__search"
              placeholder={$t("Switcher_SearchPlaceholder")}
              aria-label={$t("Switcher_SearchPlaceholder")}
              bind:value={filters.query}
            />
          </div>
        {/if}
      </div>

      <div class="switcher__scroll">
        {#if !loaded}
          <AccountListSkeleton />
        {:else if ordered.length === 0}
          <!-- First run. The brief asks for one clear primary action and the no-password
               story in one breath, rather than an empty grid staring back. -->
          <div class="empty">
            <h3 class="empty__title">{$t("Switcher_FirstRun_Title")}</h3>
            <p class="empty__body">{$t("Switcher_FirstRun_Body")}</p>
            <button
              type="button"
              class="ss-btn ss-btn--primary"
              disabled={addingAccount}
              on:click={addAccount}>{$t("AddLogin_Title")}</button
            >
          </div>
        {:else if visible.length === 0}
          <div class="empty">
            <p class="empty__body">{$t("Switcher_NoMatches")}</p>
            <button
              type="button"
              class="ss-btn"
              on:click={() => (filters = { ...EMPTY_SWITCHER_FILTERS })}
              >{$t("Vault_Filter_Clear")}</button
            >
          </div>
        {:else}
          <div class="grid" role="list">
            {#each visible as acc (acc.steamId64)}
              <div role="listitem">
                <AccountTile
                  account={acc}
                  role={roleOf(roles, acc.steamId64)}
                  kitTravels={kitTravelsTo(roles, acc.steamId64)}
                  current={acc.steamId64 === currentId}
                  disabled={blocked}
                  switching={acc.steamId64 === switchTargetId}
                  showHealth={vaultInUse}
                  index={keycaps[acc.steamId64] ?? 0}
                  avatarEpoch={avatarEpoch[acc.steamId64] ?? 0}
                  boundary={rootEl}
                  on:switch={(e) => void switchTo(e.detail)}
                  on:detail={(e) => (detailId = e.detail)}
                  on:menu={onTileMenu}
                />
              </div>
            {/each}

            <button
              type="button"
              class="add-tile"
              disabled={addingAccount || blocked}
              on:click={addAccount}
            >
              <span aria-hidden="true">＋</span>
              {$t("AddLogin_Tile")}
            </button>
          </div>

          {#if Object.keys(keycaps).length > 0}
            <!-- Says out loud what the digits bind to. Without it the numbers look arbitrary
                 the first time a filter renumbers them. -->
            <p class="switcher__hint">{$t("Shortcuts_OrderNote")}</p>
          {/if}
        {/if}
      </div>
    </div>

    {#if detailAccount}
      <AccountDetailPanel
        account={detailAccount}
        role={roleOf(roles, detailAccount.steamId64)}
        current={detailAccount.steamId64 === currentId}
        avatarEpoch={avatarEpoch[detailAccount.steamId64] ?? 0}
        showHealth={vaultInUse}
        switchDisabled={blocked}
        on:close={() => (detailId = "")}
        on:switch={(e) => void switchTo(e.detail)}
        on:editVault={(e) => openVaultEditor(e.detail)}
        on:menu={onTileMenu}
      />
    {/if}
  </div>
</div>

<ShortcutsOverlay open={shortcutsOpen} on:close={() => (shortcutsOpen = false)} />

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
  .switcher {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .switcher__body {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    gap: var(--space-5);
    padding: 0 var(--space-7) var(--space-5);
  }

  .switcher__main {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .switcher__list-head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    flex-wrap: wrap;
  }

  .switcher__list-title {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
  }

  .switcher__h2 {
    margin: 0;
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .switcher__count {
    color: var(--fg-muted);
  }

  .switcher__filters {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .switcher__chips {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  .switcher__search {
    width: 210px;
    flex: 0 1 210px;
  }

  .switcher__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  /*
    `auto-fill` rather than a fixed column count: the doc draws three columns at 1280 and two
    at its 1000px minimum, and letting the browser derive that from a minimum tile width means
    every window size in between is handled without a breakpoint per size.
  */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--tile-min-width, 300px), 1fr));
    gap: 14px;
    align-content: start;
  }

  .add-tile {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    min-height: 74px;
    padding: var(--space-4);
    border: 1px dashed var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-lg);
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: var(--fs-control);
    cursor: pointer;
  }

  .add-tile > :global(span) {
    line-height: inherit;
  }

  .add-tile:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent-text-bright, var(--accent));
  }

  .add-tile:disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: not-allowed;
  }

  .add-tile:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }

  .switcher__hint {
    margin: 0;
    font-size: var(--fs-meta);
    color: var(--fg-disabled);
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-7) var(--space-5);
    text-align: center;
  }

  .empty__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .empty__body {
    margin: 0;
    max-width: 56ch;
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .result {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px var(--space-5);
    border: 1px solid var(--hairline-strong, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel-raised, var(--surface-panel));
  }

  .result__dot {
    flex: 0 0 auto;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--orange);
  }

  .result__dot[data-open="true"] {
    background: var(--green);
  }

  .result__text {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .result__title {
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .result__sub {
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
  }
</style>

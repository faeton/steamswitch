<script lang="ts">
  /**
   * The Vault — a managed place, not a read-only list (REDESIGN_BRIEF.md A6, J4/J6).
   *
   * What this replaces: the vault used to be three loose actions on the Tools page, whose
   * "View all" opened a read-only modal dead-end, and whose editor could only be reached by
   * right-clicking a home tile — so an account with no tile could not be added or edited at
   * all (brief Part B #8). Everything the vault can do is now reachable from here, including
   * opening the editor blank.
   *
   * The table and the editor live in `components/vault/`; this page owns which of the four
   * states is showing (unset / locked / empty / list) and the filter model over the entries.
   */
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { t } from "../stores/i18n";
  import { appBarTitle, previousPage, route } from "../stores/nav";
  import { openAlert, openConfirm, activeModal } from "../stores/modal";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { navigateBackLikeButton } from "../stores/nav";
  import {
    deleteEntry,
    quickCheck,
    quickCheckAll,
    refreshVault,
    vaultBusy,
    vaultEntries,
    vaultLoaded,
    vaultStatus,
    takePendingVaultEntry,
    type VaultEntry,
  } from "../stores/vault";
  import { securityStatus, unlockVault } from "../stores/security";
  import * as SteamService from "../../bindings/steamswitch/internal/steam/steamservice.js";
  import { filterVaultEntries, type VaultFilters } from "../lib/vault/filter";
  import PageHeader from "../components/PageHeader.svelte";
  import VaultGate from "../components/vault/VaultGate.svelte";
  import VaultTable from "../components/vault/VaultTable.svelte";
  import VaultEntryEditor from "../components/vault/VaultEntryEditor.svelte";
  import RosterImportPanel from "../components/vault/RosterImportPanel.svelte";

  let filters: VaultFilters = { query: "", health: "any", hasPassword: false, onSwitcher: false };
  /** SteamID64 being edited, "" for none, NEW_ENTRY for a blank editor. */
  let editing: string | null = null;
  let checkingAll = false;
  /** True while the bulk-import flow is open. */
  let importing = false;

  let unlockPassword = "";
  let unlocking = false;
  let unlockError = "";
  let unlockEl: HTMLInputElement | undefined;
  const unlockId = "vault-unlock-password";
  const unlockErrorId = "vault-unlock-error";

  async function submitUnlock(): Promise<void> {
    if (unlocking) return;
    unlockError = "";
    unlocking = true;
    try {
      await unlockVault(unlockPassword);
      // Clear on success only. Leaving a wrong password in the box lets the user correct a
      // typo instead of retyping a long passphrase from scratch.
      unlockPassword = "";
      await refreshVault().catch(() => {});
    } catch {
      unlockError = $t("Security_UnlockFailed");
      requestAnimationFrame(() => unlockEl?.focus());
    } finally {
      unlocking = false;
    }
  }

  $: appBarTitle.set($t("Title_Vault"));

  type PageState = "unset" | "locked" | "empty" | "list";

  // vaultLocked, not appLocked: a vault-only lock leaves this page reachable, which is the
  // whole point of it. (appLocked implies vaultLocked, so the app gate still lands here — it
  // is just covered by the overlay.)
  $: state = (
    !$securityStatus.appPasswordSet
      ? "unset"
      : $securityStatus.vaultLocked
        ? "locked"
        : $vaultLoaded && $vaultEntries.length === 0
          ? "empty"
          : "list"
  ) satisfies PageState as PageState;
  $: gateState = state as Exclude<PageState, "list">;

  $: visible = filterVaultEntries($vaultEntries, filters);

  onMount(() => {
    previousPage.set({ page: "home" });
    void (async () => {
      await refreshVault().catch(() => {
        // A locked or absent vault is a normal state the gate already explains; the store
        // reports it as an empty list rather than an error.
      });
      // Arriving from a tile's "Edit vault": open that entry straight away, so the handoff
      // does not dump the user on a table and ask them to find the row again.
      const pending = takePendingVaultEntry();
      if (pending) editing = pending;
    })();
  });

  /** Open the editor with no account seeded — the "Add vault entry" door (brief J6). */
  function addEntry(): void {
    editing = "";
  }

  function editEntry(steamId64: string): void {
    editing = steamId64;
  }

  function closeEditor(): void {
    editing = null;
  }

  async function onSaved(): Promise<void> {
    await refreshVault();
    closeEditor();
  }

  async function runCheckAll(): Promise<void> {
    if (checkingAll) return;
    checkingAll = true;
    try {
      await quickCheckAll();
      pushToast({
        type: "success",
        message: $t("Tools_VaultCheckAll_Done", { count: $vaultEntries.length }),
        duration: 6000,
      });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_CheckFailed"), e),
        duration: 8000,
      });
    } finally {
      checkingAll = false;
    }
  }

  async function runCheckOne(steamId64: string): Promise<void> {
    try {
      await quickCheck(steamId64);
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_CheckFailed"), e),
        duration: 8000,
      });
    }
  }

  async function removeEntry(entry: VaultEntry): Promise<void> {
    const label = entry.label?.trim() || entry.accountName?.trim() || entry.steamId64;
    const ok = await openConfirm({
      title: $t("Vault_Delete_Title"),
      body: $t("Vault_Delete_Confirm", { name: label }),
      positiveLabel: $t("Vault_Action_Delete"),
      style: "yesno",
    });
    if (!ok) return;
    try {
      await deleteEntry(entry.steamId64);
      if (editing === entry.steamId64) closeEditor();
      pushToast({ type: "success", message: $t("Toast_Vault_Deleted"), duration: 3000 });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_DeleteFailed"), e),
        duration: 8000,
      });
    }
  }

  /**
   * Accepting a single-account handoff bundle. Still a modal: it is a short, linear,
   * passphrase-then-confirm flow with no table in it.
   *
   * Distinct from the roster import below, and now labelled as such — the two used to share
   * the word "Import", which made the one that takes a whole roster impossible to find and the
   * one that takes a single account look like it might take several.
   */
  async function openHandoffImport(): Promise<void> {
    const body = await import("../components/modals/VaultImportModalBody.svelte");
    await openAlert({
      title: $t("Tools_VaultImport_Title"),
      dismissLabel: $t("Button_Close"),
      bodyComponent: body.default,
    });
    await refreshVault();
  }

  /**
   * Bulk import (A7). A full-width overlay rather than a modal: the review table is the step
   * that makes a batch of credentials safe to accept, and it needs room to be read.
   */
  function openRosterImport(): void {
    importing = true;
  }

  async function closeRosterImport(): Promise<void> {
    importing = false;
    await refreshVault();
  }

  /**
   * Turn a vault-only entry into a real switcher tile, the only way that is actually possible.
   *
   * The Steam grid is Steam's own `loginusers.vdf`, which lists the accounts Steam has signed
   * into on this machine. SteamSwitch cannot write a row there for an account that has never
   * signed in — a fabricated tile would fail the moment it was clicked — so the honest action
   * is Steam's own login screen. `SteamAddNew` is the existing "add another Steam login" flow:
   * it clears the current login and opens Steam logged out.
   *
   * The confirmation is not ceremony. This closes Steam and signs the user out of whatever
   * account they are on, which is a surprising amount to happen from a row button, and the
   * account name is offered because the login screen will ask for it.
   */
  async function signInHere(entry: VaultEntry): Promise<void> {
    const login = entry.accountName?.trim() || "";
    const ok = await openConfirm({
      title: $t("Vault_SignIn_Title"),
      body: login
        ? $t("Vault_SignIn_Confirm_WithLogin", { login })
        : $t("Vault_SignIn_Confirm"),
      positiveLabel: $t("Vault_SignIn_Go"),
      style: "yesno",
    });
    if (!ok) return;
    try {
      await SteamService.SteamAddNew();
      pushToast({
        type: "success",
        message: login
          ? $t("Toast_Vault_SignIn_Opened_WithLogin", { login })
          : $t("Toast_Vault_SignIn_Opened"),
        duration: 8000,
      });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_SignInFailed"), e),
        duration: 8000,
      });
    }
  }

  async function openExport(steamId64: string): Promise<void> {
    const body = await import("../components/modals/VaultHandoffModalBody.svelte");
    await openAlert({
      title: $t("Vault_Menu_Handoff"),
      dismissLabel: $t("Button_Close"),
      bodyComponent: body.default,
      bodyProps: { steamId64 },
    });
  }

  function onWindowKeyDown(e: KeyboardEvent): void {
    if (e.key !== "Escape" || get(activeModal)) return;
    // The import panel handles its own Escape, because cancelling mid-review has to tell the
    // backend to drop the buffered roster rather than just hiding the panel.
    if (importing) return;
    e.preventDefault();
    // Escape closes the editor panel first; only an already-closed panel leaves the page.
    if (editing !== null) {
      closeEditor();
      return;
    }
    navigateBackLikeButton();
  }
</script>

<div class="vault">
  <PageHeader title={$t("Nav_Vault")} description={state === "list" ? $t("Vault_Page_Blurb") : ""}>
    {#if state === "list" || state === "empty"}
      <span class="vault__pill">
        <span class="vault__pill-dot" aria-hidden="true"></span>
        {$t("Vault_Status_Unlocked")}
      </span>
    {/if}

    <svelte:fragment slot="actions">
      {#if state === "list"}
        <button
          type="button"
          class="ss-btn"
          disabled={checkingAll || $vaultBusy}
          on:click={runCheckAll}
        >
          {checkingAll ? $t("Vault_Verdict_Checking") : $t("Tools_VaultCheckAll_Title")}
        </button>
      {/if}
      {#if state === "list" || state === "empty"}
        <button type="button" class="ss-btn" disabled={$vaultBusy} on:click={openHandoffImport}>
          {$t("Vault_Action_AcceptHandoff")}
        </button>
        <button type="button" class="ss-btn" disabled={$vaultBusy} on:click={openRosterImport}>
          {$t("Vault_Action_Import")}
        </button>
        <button type="button" class="ss-btn ss-btn--primary" disabled={$vaultBusy} on:click={addEntry}>
          {$t("Vault_Action_AddEntry")}
        </button>
      {/if}
    </svelte:fragment>
  </PageHeader>

  {#if state === "list"}
    <div class="vault__body">
      <VaultTable
        entries={visible}
        total={$vaultEntries.length}
        bind:filters
        selected={editing}
        on:open={(e) => editEntry(e.detail)}
        on:check={(e) => void runCheckOne(e.detail)}
        on:export={(e) => void openExport(e.detail)}
        on:delete={(e) => void removeEntry(e.detail)}
        on:signIn={(e) => void signInHere(e.detail)}
      />

      {#if editing !== null}
        <!-- Keyed on the entry. The editor snapshots the account into local form state on
             construction, so reusing one instance across rows would keep showing — and
             saving — the previously opened account under the newly selected SteamID64. -->
        {#key editing}
          <VaultEntryEditor
            steamId64={editing}
            on:saved={() => void onSaved()}
            on:cancel={closeEditor}
            on:delete={(e) => void removeEntry(e.detail)}
          />
        {/key}
      {/if}
    </div>
  {:else}
    <VaultGate state={gateState}>
      <svelte:fragment slot="actions">
        {#if state === "unset"}
          <button
            type="button"
            class="ss-btn ss-btn--primary"
            on:click={() => route.set({ page: "settings", category: "vault" })}
          >
            {$t("Vault_Settings_Enable")}
          </button>
        {:else if state === "empty"}
          <button type="button" class="ss-btn ss-btn--primary" on:click={addEntry}>
            {$t("Vault_Action_AddEntry")}
          </button>
          <button type="button" class="ss-btn" on:click={openRosterImport}>
            {$t("Vault_Action_Import")}
          </button>
          <button type="button" class="ss-btn" on:click={openHandoffImport}>
            {$t("Vault_Action_AcceptHandoff")}
          </button>
        {:else if state === "locked"}
          <!-- The unlock prompt lives here, not in the app-wide overlay: a vault-only lock
               leaves the rest of the app running, so there is no overlay to host it. -->
          {#if !$securityStatus.vaultLockCryptographic}
            <!-- Saved account blobs are sealed under the master key itself, so it cannot be
                 dropped without breaking switching. Saying "locked" without saying that would
                 promise a boundary the app is not enforcing. -->
            <p class="vault-unlock__note">{$t("Vault_Lock_SoftNote")}</p>
          {/if}
          <form class="vault-unlock" on:submit|preventDefault={submitUnlock}>
            <label class="vault-unlock__field" for={unlockId}>
              <span>{$t("Security_Password")}</span>
              <input
                id={unlockId}
                bind:this={unlockEl}
                bind:value={unlockPassword}
                type="password"
                autocomplete="current-password"
                class="modal-input"
                disabled={unlocking}
                aria-invalid={unlockError ? "true" : undefined}
                aria-describedby={unlockError ? unlockErrorId : undefined}
              />
            </label>
            {#if unlockError}
              <p id={unlockErrorId} class="vault-unlock__error" role="alert">{unlockError}</p>
            {/if}
            <button type="submit" class="ss-btn ss-btn--primary" disabled={unlocking}>
              {$t("Security_Unlock")}
            </button>
          </form>
        {/if}
      </svelte:fragment>
    </VaultGate>
  {/if}

  {#if importing}
    <div class="vault__editor-overlay">
      <RosterImportPanel
        on:close={() => void closeRosterImport()}
        on:imported={() => void refreshVault()}
      />
    </div>
  {/if}

  {#if state === "empty" && editing !== null}
    <!-- The first entry can be created straight from the empty state, so the editor has to
         be reachable outside the `list` branch too. -->
    <div class="vault__editor-overlay">
      {#key editing}
        <VaultEntryEditor
          steamId64={editing}
          on:saved={() => void onSaved()}
          on:cancel={closeEditor}
          on:delete={(e) => void removeEntry(e.detail)}
        />
      {/key}
    </div>
  {/if}
</div>
<svelte:window on:keydown={onWindowKeyDown} />

<style>
  .vault {
    /* The editor overlay below is `position: absolute; inset: 0`; without this it would
       resolve against `.page` and its scrim would cover the sidebar as well. */
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .vault__body {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    gap: var(--space-5);
    padding: 0 var(--space-7) var(--space-6);
  }

  .vault__pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 3px 9px;
    border: 1px solid var(--border-ok);
    border-radius: var(--radius-pill);
    background: var(--bg-ok);
    color: var(--fg-ok);
    font-size: var(--fs-meta);
    white-space: nowrap;
  }

  .vault__pill-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--green);
  }

  .vault__editor-overlay {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: var(--space-6);
    background: var(--modal-scrim, var(--backdrop-scrim-55));
    z-index: 10;
  }

  .vault-unlock {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: min(320px, 100%);
  }

  .vault-unlock__field {
    display: grid;
    gap: var(--space-1);
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .vault-unlock__error {
    margin: 0;
    font-size: var(--fs-secondary);
    color: var(--fg-danger, var(--danger, #ff6b6b));
  }

  .vault-unlock__note {
    margin: 0;
    max-width: 46ch;
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }
</style>

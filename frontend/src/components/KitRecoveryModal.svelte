<script lang="ts">
  /**
   * "Last switch didn't finish" — shown on launch when a transaction was interrupted, and
   * when a restore was blocked because the files changed outside SteamSwitch
   * (REDESIGN.md §2, "Crash mid-switch" and "External-change detection").
   *
   * Genuinely non-dismissable: no Escape, no backdrop click, no close button. The engine is
   * already refusing every switch until this is answered, so a dismissable dialog would just
   * produce an app that silently does nothing when tiles are clicked.
   *
   * The default differs by cause, because the safe answer does:
   *  - interrupted mid-write → restore the other person's setup, if one was captured;
   *  - external change → keep what is on disk, since something we did not write is there and
   *    overwriting it would destroy work that is not ours (REDESIGN.md: "Default = no write").
   */
  import { createEventDispatcher } from "svelte";
  import KitDialog from "./KitDialog.svelte";
  import { t } from "../stores/i18n";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { resolveRecovery, type KitStatus } from "../stores/sessionKit";
  import { get } from "svelte/store";

  export let status: KitStatus;
  /** Display name for the affected account; falls back to the raw id when unknown. */
  export let targetLabel = "";

  const dispatch = createEventDispatcher<{ done: void }>();

  let busy = false;
  let error = "";
  let showDetail = false;

  $: external = status.kind === "external-change";
  $: label = targetLabel || status.targetSteamId64;
  // "Restore theirs" is only meaningful when a snapshot of their setup was actually captured
  // before the crash. Offering it otherwise would promise a rollback that cannot happen.
  $: canRestore = status.canRestore;

  async function choose(action: "restore-theirs" | "keep-current" | "abandon"): Promise<void> {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await resolveRecovery(action);
      dispatch("done");
    } catch (e) {
      error = formatToastWithError(get(t)("Toast_Kit_RecoveryFailed"), e);
    } finally {
      busy = false;
    }
  }
</script>

<KitDialog
  title={external ? $t("Kit_Recovery_ExternalChange") : $t("Kit_Recovery_Interrupted")}
  tone="error"
>
  <p>
    {external
      ? $t("Kit_Recovery_ExternalBody").replace("{0}", label)
      : $t("Kit_Recovery_Body").replace("{0}", label)}
  </p>

  {#if !canRestore && !external}
    <p class="kit-rec__note">{$t("Kit_Recovery_NothingWritten")}</p>
  {/if}

  {#if status.loginMismatch}
    <!-- The crash left the files and the login disagreeing. Worth saying plainly, because
         it is the one part of the damage the user can see for themselves next time they
         open Steam. Restoring fixes it; keeping what is on disk deliberately does not. -->
    <p class="kit-rec__note">{$t("Kit_Recovery_LoginMismatch")}</p>
  {/if}

  <button
    type="button"
    class="kit-rec__disclosure"
    aria-expanded={showDetail}
    on:click={() => (showDetail = !showDetail)}
  >
    {showDetail ? "▾" : "▸"}
    {$t("Kit_Action_Inspect")}
  </button>

  {#if showDetail}
    <!-- Read-only diagnostics. Deliberately shows the phase and transaction id verbatim:
         when someone reports a problem, these are the two values that identify it. -->
    <dl class="kit-rec__detail">
      <dt>{$t("Kit_Detail_Phase")}</dt>
      <dd>{status.phase || "—"}</dd>
      <dt>{$t("Kit_Detail_Transaction")}</dt>
      <dd>{status.transactionId.slice(0, 12) || "—"}</dd>
      <dt>{$t("Kit_Detail_Started")}</dt>
      <dd>{status.startedAt || "—"}</dd>
      <dt>{$t("Kit_Detail_Modules")}</dt>
      <dd>{(status.moduleNames ?? []).join(", ") || "—"}</dd>
      <dt>{$t("Kit_Detail_SignedIn")}</dt>
      <dd>{status.signedInSteamId64 || "—"}</dd>
      {#if status.lastError}
        <dt>{$t("Kit_Detail_Error")}</dt>
        <dd>{status.lastError}</dd>
      {/if}
    </dl>
  {/if}

  {#if error}
    <p class="kit-rec__error" role="alert">{error}</p>
  {/if}

  <svelte:fragment slot="actions">
    {#if external}
      <button type="button" class="is-primary" disabled={busy} on:click={() => choose("keep-current")}>
        {$t("Kit_Action_KeepCurrent")}
      </button>
      {#if canRestore}
        <button type="button" disabled={busy} on:click={() => choose("restore-theirs")}>
          {$t("Kit_Recovery_RestoreAnyway")}
        </button>
      {/if}
    {:else}
      {#if canRestore}
        <button type="button" class="is-primary" disabled={busy} on:click={() => choose("restore-theirs")}>
          {busy ? $t("Kit_Leave_Working") : $t("Kit_Action_Restore")}
        </button>
        <button type="button" disabled={busy} on:click={() => choose("keep-current")}>
          {$t("Kit_Action_KeepCurrent")}
        </button>
      {:else}
        <button type="button" class="is-primary" disabled={busy} on:click={() => choose("abandon")}>
          {busy ? $t("Kit_Leave_Working") : $t("Kit_Recovery_Discard")}
        </button>
      {/if}
    {/if}
  </svelte:fragment>
</KitDialog>

<style lang="scss">
  p {
    margin: 0 0 6px;
  }

  .kit-rec__note {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .kit-rec__disclosure {
    background: none;
    border: none;
    padding: 2px 0;
    font: inherit;
    font-size: 11px;
    color: var(--accent);
    cursor: pointer;
    text-align: left;
  }

  .kit-rec__disclosure:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }

  .kit-rec__detail {
    margin: 4px 0 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 8px;
    max-height: 150px;
    overflow-y: auto;
    font-family: var(--mono-font, ui-monospace, "Cascadia Mono", Consolas, monospace);
    font-size: 11px;
    word-break: break-word;
  }

  .kit-rec__detail dt {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .kit-rec__detail dd {
    margin: 0;
  }

  .kit-rec__error {
    color: var(--status-error-fg, var(--danger, #e5534b));
  }
</style>

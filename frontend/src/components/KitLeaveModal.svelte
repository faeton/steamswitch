<script lang="ts">
  /**
   * "Restore X's setup?" — asked every time the user leaves a shared account while their kit
   * is active (REDESIGN.md §2, locked decision "ask each time").
   *
   * Restore-theirs is the default because the cost of the two mistakes is asymmetric: keeping
   * your kit on someone else's account changes *their* game for them without their knowledge,
   * while restoring theirs when you meant to keep yours costs you one re-switch.
   *
   * Escape cancels the switch rather than choosing. Mapping Escape to either answer would
   * make an idle keypress silently rewrite somebody's config folder.
   */
  import { createEventDispatcher } from "svelte";
  import KitDialog from "./KitDialog.svelte";
  import { t } from "../stores/i18n";
  import { formatToastWithError } from "../lib/formatWailsError";
  import { resolveLeave, type LeavePrompt } from "../stores/sessionKit";
  import { get } from "svelte/store";

  export let prompt: LeavePrompt;

  const dispatch = createEventDispatcher<{ done: void; cancel: void }>();

  let busy = false;
  let error = "";
  let showDetail = false;

  async function choose(choice: "restore-theirs" | "keep-mine"): Promise<void> {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await resolveLeave(choice, prompt.toSteamId64);
      dispatch("done");
    } catch (e) {
      // Keep the dialog open. Closing over a failure would leave the user believing a
      // decision was applied when the files were never touched.
      error = formatToastWithError(get(t)("Toast_Kit_LeaveFailed"), e);
    } finally {
      busy = false;
    }
  }
</script>

<KitDialog
  title={$t("Kit_Leave_Title").replace("{0}", prompt.fromLabel)}
  escapeCancels={!busy}
  on:cancel={() => dispatch("cancel")}
>
  <p>{$t("Kit_Leave_Body").replace("{0}", prompt.fromLabel)}</p>

  {#if prompt.onQuit}
    <!-- Answers "what if I just walk away?", which is the question a quit-time dialog
         provokes and the one a hostage dialog refuses to answer. -->
    <p class="kit-leave__quitnote">{$t("Kit_Leave_QuitNote").replace("{0}", prompt.fromLabel)}</p>
  {/if}

  <button
    type="button"
    class="kit-leave__disclosure"
    aria-expanded={showDetail}
    on:click={() => (showDetail = !showDetail)}
  >
    {showDetail ? "▾" : "▸"}
    {$t("Kit_WhatChanged")}
  </button>

  {#if showDetail}
    <!-- Inline rather than a second screen: at 420px a nested page would lose the question
         the user is still being asked. -->
    <dl class="kit-leave__detail">
      <dt>{$t("Kit_Detail_Modules")}</dt>
      <dd>{prompt.moduleNames.join(", ") || "—"}</dd>
      <dt>{$t("Kit_Detail_Scope")}</dt>
      <dd>{$t("Kit_Detail_ScopeValue")}</dd>
    </dl>
  {/if}

  {#if error}
    <p class="kit-leave__error" role="alert">{error}</p>
  {/if}

  <svelte:fragment slot="actions">
    <button type="button" class="is-primary" disabled={busy} on:click={() => choose("restore-theirs")}>
      {busy ? $t("Kit_Leave_Working") : $t("Kit_Restore_Theirs")}
    </button>
    <button type="button" disabled={busy} on:click={() => choose("keep-mine")}>
      {$t("Kit_Keep_Mine")}
    </button>
  </svelte:fragment>
</KitDialog>

<style lang="scss">
  p {
    margin: 0 0 6px;
  }

  .kit-leave__quitnote {
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-style: italic;
  }

  .kit-leave__disclosure {
    background: none;
    border: none;
    padding: 2px 0;
    font: inherit;
    font-size: var(--fs-secondary);
    color: var(--accent);
    cursor: pointer;
    text-align: left;
  }

  .kit-leave__disclosure:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }

  .kit-leave__detail {
    margin: 4px 0 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 8px;
    max-height: 140px;
    overflow-y: auto;
    font-family: var(--mono-font, ui-monospace, "Cascadia Mono", Consolas, monospace);
    font-size: var(--fs-secondary);
  }

  .kit-leave__detail dt {
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  .kit-leave__detail dd {
    margin: 0;
  }

  .kit-leave__error {
    color: var(--status-error-fg, var(--danger, #e5534b));
  }
</style>

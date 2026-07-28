<script lang="ts">
  /**
   * Bulk import — source, review, commit, summary (REDESIGN_BRIEF.md A5 flow 6, A7).
   *
   * The flow is four steps because the third one is the feature. Parsing a roster is easy;
   * what makes a batch of credentials safe to accept is the review table between "here is a
   * file" and "the vault has been rewritten" — per-row create-vs-update, which fields will be
   * filled and which of yours will be kept, and what could not be read at all.
   *
   * Two things this deliberately never renders: a password, and a claim that a file was
   * securely erased. The plan the backend returns carries presence, not values, so there is no
   * secret in this component's state to leak into a devtools inspector or a screenshot. And
   * the plaintext-file path reports "removed the file we could see" — see the summary copy.
   *
   * Intake C (the CLI's `--seal-roster`) has no UI step of its own on purpose: it produces the
   * same sealed bundle intake A consumes. What it needs from this screen is the payload shape,
   * which the source step hands over as a copyable template.
   */
  import { createEventDispatcher } from "svelte";
  import { t } from "../../stores/i18n";
  import { openFolderPicker } from "../../stores/modal";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import * as VaultService from "../../../bindings/steamswitch/internal/vault/vaultservice.js";
  import {
    canCommit,
    changedFields,
    countRows,
    decisionsFrom,
    keptFields,
    type ImportPlan,
    type ImportRow,
    type RowMode,
  } from "../../lib/vault/importPlan";

  const dispatch = createEventDispatcher<{ close: void; imported: void }>();

  type Step = "source" | "review" | "summary";
  type Source = "bundle" | "paste" | "plaintext";

  let step: Step = "source";
  let source: Source = "bundle";
  let busy = false;

  // Intake A
  let bundlePath = "";
  let passphrase = "";
  // Intake B
  let pasted = "";
  // Intake D
  let plaintextPath = "";
  let plaintextAcknowledged = false;

  let plan: ImportPlan | null = null;
  let modes: Record<number, RowMode> = {};
  let summary: {
    created: number;
    updated: number;
    skipped: number;
    invalid: number;
    total: number;
    plaintextRemoved: boolean;
    plaintextPath?: string;
    rejected?: { index: number; steamId64?: string; reason: string }[] | null;
  } | null = null;

  $: rows = plan?.rows ?? [];
  $: counts = countRows(rows);
  $: commitAllowed = canCommit(rows);

  function reportFailure(fallbackKey: string, e: unknown): void {
    pushToast({ type: "error", message: formatToastWithError($t(fallbackKey), e), duration: 8000 });
  }

  async function pickFile(titleKey: string): Promise<string | null> {
    return openFolderPicker({ title: $t(titleKey), dirsOnly: false });
  }

  async function parseSource(): Promise<void> {
    if (busy) return;
    busy = true;
    try {
      let next: ImportPlan;
      if (source === "bundle") {
        next = (await VaultService.PrepareRosterFromBundle(
          bundlePath,
          passphrase,
        )) as unknown as ImportPlan;
      } else if (source === "paste") {
        next = (await VaultService.PrepareRosterFromText(pasted)) as unknown as ImportPlan;
      } else {
        next = (await VaultService.PrepareRosterFromPlaintextFile(
          plaintextPath,
        )) as unknown as ImportPlan;
      }
      plan = next;
      modes = {};
      step = "review";
      // The passphrase and the pasted text have done their job. Dropping them now keeps them
      // out of this component's state for the rest of the flow — the parsed roster lives in
      // Go from here on, which is the whole point of the session-buffer design.
      passphrase = "";
      pasted = "";
    } catch (e) {
      reportFailure("Toast_Roster_ParseFailed", e);
    } finally {
      busy = false;
    }
  }

  /**
   * Re-plan after a per-row override.
   *
   * Round-trips to the backend rather than recomputing here, so the table always shows what
   * the committer will actually do. A client-side approximation of the merge rule is a second
   * implementation of the conflict policy, and the two would drift.
   */
  async function setMode(index: number, mode: RowMode): Promise<void> {
    if (!plan || busy) return;
    modes = { ...modes, [index]: mode };
    busy = true;
    try {
      plan = (await VaultService.RepriceRosterImport(
        plan.sessionId,
        decisionsFrom(modes),
      )) as unknown as ImportPlan;
    } catch (e) {
      reportFailure("Toast_Roster_ParseFailed", e);
    } finally {
      busy = false;
    }
  }

  async function commit(): Promise<void> {
    if (!plan || busy || !commitAllowed) return;
    busy = true;
    try {
      summary = (await VaultService.CommitRosterImport(
        plan.sessionId,
        decisionsFrom(modes),
      )) as unknown as typeof summary;
      step = "summary";
      dispatch("imported");
    } catch (e) {
      reportFailure("Toast_Roster_ImportFailed", e);
    } finally {
      busy = false;
    }
  }

  async function cancel(): Promise<void> {
    if (plan && step === "review") {
      // Tell the backend to drop the buffer now rather than leaving a roster of credentials
      // in memory until its timeout.
      try {
        await VaultService.DiscardRosterImport(plan.sessionId);
      } catch {
        // The buffer times out on its own; nothing useful to say.
      }
    }
    dispatch("close");
  }

  async function copyTemplate(): Promise<void> {
    try {
      const template = await VaultService.GetRosterTemplate();
      await navigator.clipboard.writeText(template);
      pushToast({ type: "success", message: $t("Toast_Roster_TemplateCopied"), duration: 4000 });
    } catch (e) {
      reportFailure("Toast_Roster_TemplateFailed", e);
    }
  }

  $: sourceReady =
    source === "bundle"
      ? bundlePath.trim() !== "" && passphrase.trim() !== ""
      : source === "paste"
        ? pasted.trim() !== ""
        : plaintextPath.trim() !== "" && plaintextAcknowledged;

  function rowLabel(row: ImportRow): string {
    return row.label?.trim() || row.accountName?.trim() || row.steamId64;
  }

  function fieldNames(fields: string[]): string {
    return fields.map((f) => $t(`Roster_Field_${f}`)).join(", ");
  }

  // Named rather than inlined in the template: a type assertion inside a Svelte event
  // expression does not parse.
  function onModeChange(index: number, e: Event): void {
    const value = (e.currentTarget as HTMLSelectElement).value as RowMode;
    void setMode(index, value);
  }

  /**
   * Escape is handled here rather than by the page, because leaving the review is not the same
   * as hiding a panel: the backend is holding a decrypted roster keyed to this session, and it
   * has to be told to drop it. Ignored while a request is in flight and after a commit, where
   * there is nothing left to discard and the summary is the only record of what happened.
   */
  function onKeyDown(e: KeyboardEvent): void {
    if (e.key !== "Escape" || busy || step === "summary") return;
    e.preventDefault();
    void cancel();
  }
</script>

<svelte:window on:keydown={onKeyDown} />

<section class="import" aria-label={$t("Roster_Title")}>
  <header class="import__head">
    <h2 class="import__title">{$t("Roster_Title")}</h2>
    <p class="import__blurb">{$t("Roster_Blurb")}</p>
  </header>

  {#if step === "source"}
    <div class="import__body">
      <div class="tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={source === "bundle"}
          class="tab"
          class:tab--on={source === "bundle"}
          on:click={() => (source = "bundle")}>{$t("Roster_Source_Bundle")}</button
        >
        <button
          type="button"
          role="tab"
          aria-selected={source === "paste"}
          class="tab"
          class:tab--on={source === "paste"}
          on:click={() => (source = "paste")}>{$t("Roster_Source_Paste")}</button
        >
        <button
          type="button"
          role="tab"
          aria-selected={source === "plaintext"}
          class="tab"
          class:tab--on={source === "plaintext"}
          on:click={() => (source = "plaintext")}>{$t("Roster_Source_Plaintext")}</button
        >
      </div>

      {#if source === "bundle"}
        <p class="hint">{$t("Roster_Source_Bundle_Hint")}</p>
        <div class="field">
          <span class="field__label">{$t("Roster_Field_File")}</span>
          <div class="field__row">
            <span class="meta-mono field__value">{bundlePath || "—"}</span>
            <button
              type="button"
              class="ss-btn"
              on:click={async () => {
                const picked = await pickFile("Roster_Pick_Bundle");
                if (picked) bundlePath = picked;
              }}>{$t("Roster_Action_Choose")}</button
            >
          </div>
        </div>
        <label class="field">
          <span class="field__label">{$t("Roster_Field_Passphrase")}</span>
          <input type="password" bind:value={passphrase} autocomplete="off" />
        </label>
        <!-- The CLI path lives here rather than in a tab of its own: it produces exactly the
             file this tab consumes, so what it needs from the UI is the payload shape. -->
        <details class="cli">
          <summary>{$t("Roster_CLI_Summary")}</summary>
          <p class="hint">{$t("Roster_CLI_Body")}</p>
          <pre class="meta-mono cli__code">{$t("Roster_CLI_Example")}</pre>
          <button type="button" class="ss-btn" on:click={() => void copyTemplate()}>
            {$t("Roster_Action_CopyTemplate")}
          </button>
        </details>
      {:else if source === "paste"}
        <p class="hint">{$t("Roster_Source_Paste_Hint")}</p>
        <p class="hint hint--warn">{$t("Roster_Source_Paste_Clipboard")}</p>
        <textarea
          class="paste"
          bind:value={pasted}
          spellcheck="false"
          autocomplete="off"
          placeholder={$t("Roster_Source_Paste_Placeholder")}
        ></textarea>
      {:else}
        <p class="hint hint--warn">{$t("Roster_Source_Plaintext_Warning")}</p>
        <div class="field">
          <span class="field__label">{$t("Roster_Field_File")}</span>
          <div class="field__row">
            <span class="meta-mono field__value">{plaintextPath || "—"}</span>
            <button
              type="button"
              class="ss-btn"
              on:click={async () => {
                const picked = await pickFile("Roster_Pick_Plaintext");
                if (picked) plaintextPath = picked;
              }}>{$t("Roster_Action_Choose")}</button
            >
          </div>
        </div>
        <label class="ack">
          <input type="checkbox" bind:checked={plaintextAcknowledged} />
          <span>{$t("Roster_Source_Plaintext_Ack")}</span>
        </label>
      {/if}
    </div>

    <footer class="import__foot">
      <button type="button" class="ss-btn" on:click={() => void cancel()}>
        {$t("Button_Cancel")}
      </button>
      <button
        type="button"
        class="ss-btn ss-btn--primary"
        disabled={!sourceReady || busy}
        on:click={() => void parseSource()}
      >
        {$t("Roster_Action_Review")}
      </button>
    </footer>
  {:else if step === "review"}
    <div class="import__body">
      <p class="counts">
        {$t("Roster_Counts", {
          total: counts.total,
          create: counts.create,
          update: counts.update,
          skip: counts.skip,
          invalid: counts.invalid,
        })}
      </p>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>{$t("Roster_Col_Account")}</th>
              <th>{$t("Roster_Col_Action")}</th>
              <th>{$t("Roster_Col_Effect")}</th>
              <th>{$t("Roster_Col_Mode")}</th>
            </tr>
          </thead>
          <tbody>
            {#each rows as row (row.index)}
              <tr class:row--invalid={row.action === "invalid"}>
                <td>
                  <div class="cell-name">{rowLabel(row)}</div>
                  <div class="meta-mono cell-sub">{row.steamId64 || "—"}</div>
                </td>
                <td>
                  <span class="badge" data-action={row.action}>
                    {$t(`Roster_Action_${row.action}`)}
                  </span>
                </td>
                <td class="cell-effect">
                  {#if row.action === "invalid"}
                    <span class="effect effect--bad">{row.invalid}</span>
                  {:else if row.action === "skip"}
                    <span class="effect">{$t("Roster_Effect_Skipped")}</span>
                  {:else}
                    {#if changedFields(row).length}
                      <span class="effect">
                        {$t("Roster_Effect_Sets", { fields: fieldNames(changedFields(row)) })}
                      </span>
                    {:else}
                      <span class="effect">{$t("Roster_Effect_NoChange")}</span>
                    {/if}
                    {#if keptFields(row).length}
                      <!-- The conflict, stated before the commit. "Your password is kept" has
                           to be visible here; discovering it afterwards from a login that
                           still uses the old one is not a review. -->
                      <span class="effect effect--kept">
                        {$t("Roster_Effect_Keeps", { fields: fieldNames(keptFields(row)) })}
                      </span>
                    {/if}
                  {/if}
                </td>
                <td>
                  {#if row.action !== "invalid"}
                    <select
                      value={modes[row.index] ?? "default"}
                      disabled={busy}
                      on:change={(e) => onModeChange(row.index, e)}
                    >
                      <option value="default">{$t("Roster_Mode_Default")}</option>
                      <option value="overwrite">{$t("Roster_Mode_Overwrite")}</option>
                      <option value="skip">{$t("Roster_Mode_Skip")}</option>
                    </select>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <p class="hint">{$t("Roster_Review_Standalone")}</p>
      {#if plan?.plaintextPath}
        <p class="hint hint--warn">
          {$t("Roster_Review_PlaintextRemoval", { path: plan.plaintextPath })}
        </p>
      {/if}
    </div>

    <footer class="import__foot">
      <button type="button" class="ss-btn" disabled={busy} on:click={() => void cancel()}>
        {$t("Button_Cancel")}
      </button>
      <button
        type="button"
        class="ss-btn ss-btn--primary"
        disabled={!commitAllowed || busy}
        on:click={() => void commit()}
      >
        {$t("Roster_Action_Commit", { count: counts.create + counts.update })}
      </button>
    </footer>
  {:else if summary}
    <div class="import__body">
      <p class="counts">
        {$t("Roster_Summary", {
          created: summary.created,
          updated: summary.updated,
          skipped: summary.skipped,
          invalid: summary.invalid,
          total: summary.total,
        })}
      </p>

      {#if summary.rejected?.length}
        <ul class="rejected">
          {#each summary.rejected as bad (bad.index)}
            <li>
              <span class="meta-mono">{bad.steamId64 || $t("Roster_Row", { n: bad.index + 1 })}</span
              >
              — {bad.reason}
            </li>
          {/each}
        </ul>
      {/if}

      {#if summary.plaintextPath}
        <!--
          The honest line. Never "securely deleted": on an SSD the block is remapped rather
          than rewritten, and shadow copies, cloud version history and the search index all
          keep their own copies. Claiming erasure here would teach a false belief about the
          riskiest thing the user just did.
        -->
        <p class="hint hint--warn">
          {summary.plaintextRemoved
            ? $t("Roster_Summary_PlaintextRemoved", { path: summary.plaintextPath })
            : $t("Roster_Summary_PlaintextKept", { path: summary.plaintextPath })}
        </p>
      {/if}

      <p class="hint">{$t("Roster_Summary_Standalone")}</p>
    </div>

    <footer class="import__foot">
      <button type="button" class="ss-btn ss-btn--primary" on:click={() => dispatch("close")}>
        {$t("Button_Close")}
      </button>
    </footer>
  {/if}
</section>

<style>
  .import {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    width: min(920px, 100%);
    max-height: 100%;
    padding: var(--space-6);
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-xl);
    background: var(--surface-panel);
    box-shadow: var(--shadow);
    min-height: 0;
  }

  .import__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .import__blurb,
  .hint {
    margin: 0;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .hint--warn {
    color: var(--fg-warn);
  }

  .import__body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    overflow-y: auto;
    min-height: 0;
  }

  .import__foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    flex: 0 0 auto;
  }

  .tabs {
    display: flex;
    gap: var(--space-2);
  }

  .tab {
    padding: 6px 12px;
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-pill);
    background: transparent;
    color: var(--fg-muted);
    font-size: var(--fs-secondary);
    cursor: pointer;
  }

  .tab--on {
    border-color: var(--accent-text-bright, var(--border-ok));
    color: var(--fg-primary);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .field__label {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
  }

  .field__row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .field__value {
    flex: 1 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .paste {
    min-height: 180px;
    resize: vertical;
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-secondary);
  }

  .ack {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
  }

  .cli__code {
    margin: var(--space-2) 0;
    padding: var(--space-3);
    border-radius: var(--radius-sm, 6px);
    background: var(--button-bg);
    font-size: var(--fs-meta);
    overflow-x: auto;
    white-space: pre;
  }

  .counts {
    margin: 0;
    font-size: var(--fs-secondary);
    color: var(--fg-primary);
  }

  /* The table scrolls inside its own box so a long roster never makes the panel itself
     scroll horizontally. */
  .table-wrap {
    overflow: auto;
    max-height: 46vh;
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-sm, 6px);
  }

  .table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-secondary);
  }

  .table th,
  .table td {
    padding: var(--space-3);
    text-align: left;
    vertical-align: top;
    border-bottom: 1px solid var(--hairline);
  }

  .table th {
    position: sticky;
    top: 0;
    background: var(--surface-panel);
    font-size: var(--fs-meta);
    color: var(--fg-muted);
    font-weight: var(--fw-semibold);
  }

  .row--invalid {
    opacity: 0.75;
  }

  .cell-name {
    color: var(--fg-primary);
  }

  .cell-sub {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
  }

  .cell-effect {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .effect {
    color: var(--fg-muted);
    font-size: var(--fs-meta);
  }

  .effect--kept {
    color: var(--fg-warn);
  }

  .effect--bad {
    color: var(--fg-danger, var(--fg-warn));
  }

  .badge {
    display: inline-block;
    padding: 2px 8px;
    border: 1px solid var(--hairline-strong);
    border-radius: var(--radius-pill);
    font-size: var(--fs-meta);
    white-space: nowrap;
  }

  .badge[data-action="create"] {
    border-color: var(--border-ok);
    color: var(--fg-ok);
  }

  .badge[data-action="update"] {
    border-color: var(--border-warn);
    color: var(--fg-warn);
  }

  .badge[data-action="invalid"] {
    border-color: var(--border-danger, var(--border-warn));
    color: var(--fg-danger, var(--fg-warn));
  }

  .rejected {
    margin: 0;
    padding-left: var(--space-5);
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
  }
</style>

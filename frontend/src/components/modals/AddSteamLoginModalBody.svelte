<script lang="ts">
  /**
   * "Add another Steam login", explained *before* the click (REDESIGN_BRIEF.md A5 flow 3, J3).
   *
   * The old button was the app's most-reported bug that wasn't a crash: it called
   * `SteamAddNew()` → `SwapToAccount("")`, which clears the active login, and then relaunched
   * Steam **only if `AutoStart` was on** — with no toast either way. So on a default install
   * the button greyed out for a moment and then, visibly, nothing (brief Part B #7).
   *
   * Two things fix it, and both are needed. This dialog is the first: it says plainly that the
   * action signs Steam *out* and does not add a stored account, and it makes the relaunch an
   * explicit choice rather than a setting the user has never seen. The second is on the page —
   * the result is reported as a dismissable state, never silence.
   */
  import { t } from "../../stores/i18n";

  /**
   * Mutable holder the host reads after the modal resolves.
   *
   * `bodyProps` are one-way, so a plain boolean prop could never carry the answer back. The
   * host owns the object and reads `choice.reopenSteam` once `openConfirm` settles.
   */
  export let choice: { reopenSteam: boolean };
  /** Display name of the account whose login is about to be saved, if any. */
  export let currentLabel = "";
</script>

<div class="add-login">
  <p class="add-login__lead">
    {#if currentLabel}
      {@html $t("AddLogin_Body_WithCurrent", { name: currentLabel })}
    {:else}
      {@html $t("AddLogin_Body_NoCurrent")}
    {/if}
  </p>

  <label class="add-login__choice">
    <input type="checkbox" class="ss-checkbox" bind:checked={choice.reopenSteam} />
    <span class="add-login__choice-text">
      <span class="add-login__choice-label">{$t("AddLogin_Reopen")}</span>
      <span class="ss-help">{$t("AddLogin_Reopen_Hint")}</span>
    </span>
  </label>

  <p class="ss-help">{$t("AddLogin_Footnote")}</p>
</div>

<style>
  .add-login {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .add-login__lead {
    margin: 0;
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-secondary);
  }

  /* The one emphasis in the copy is on "not", because that is the misunderstanding. */
  .add-login__lead :global(b) {
    color: var(--fg-primary);
    font-weight: var(--fw-semibold);
  }

  .add-login__choice {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    padding: var(--space-4);
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-field, transparent);
    cursor: pointer;
  }

  .add-login__choice :global(.ss-checkbox) {
    margin-top: 2px;
  }

  .add-login__choice-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .add-login__choice-label {
    font-size: var(--fs-body);
    color: var(--fg-primary);
  }
</style>

<script lang="ts">
  /**
   * A blocking decision dialog for Session Kit prompts (REDESIGN.md §2).
   *
   * Deliberately not `AppModal`/`ModalShell`: those are draggable, resizable, dismissable
   * windows with a close button. A kit prompt is a decision the app cannot proceed without,
   * so it has no close affordance and no backdrop dismiss, and it announces itself as an
   * `alertdialog` rather than a `dialog` so screen readers interrupt instead of queueing.
   *
   * `onEscape` is optional and deliberately not wired to "dismiss". Escape must never be a
   * silent third answer to a two-way question — a caller that has a safe cancel (the leave
   * prompt, where cancelling means "stay where I am") passes one; recovery does not.
   */
  import { createEventDispatcher } from "svelte";
  import { fade, scale } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { DUR, motionEnabled } from "../lib/animation";
  import { modalFocus } from "../lib/modalFocus";

  export let title: string;
  /** Set when Escape has a meaning that is not "answer the question". */
  export let escapeCancels = false;
  /** Marks the dialog as reporting a fault, which switches the accent to the error tone. */
  export let tone: "warn" | "error" = "warn";

  const dispatch = createEventDispatcher<{ cancel: void }>();

  function onEscape(): void {
    if (escapeCancels) dispatch("cancel");
  }

  let primaryEl: HTMLElement | null = null;
</script>

<div
  class="kitdlg__scrim"
  role="presentation"
  in:fade={{ duration: motionEnabled() ? DUR.fast : 0, easing: cubicOut }}
  out:fade={{ duration: motionEnabled() ? DUR.fast : 0, easing: cubicOut }}
>
  <div
    class="kitdlg"
    class:kitdlg--error={tone === "error"}
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="kitdlg-title"
    aria-describedby="kitdlg-body"
    use:modalFocus={{ onEscape, initialFocus: () => primaryEl }}
    in:scale={{ start: 0.97, duration: motionEnabled() ? DUR.normal : 0, easing: cubicOut }}
  >
    <h2 id="kitdlg-title" class="kitdlg__title">{title}</h2>
    <div id="kitdlg-body" class="kitdlg__body"><slot /></div>
    <div class="kitdlg__actions" bind:this={primaryEl}><slot name="actions" /></div>
  </div>
</div>

<style lang="scss">
  .kitdlg__scrim {
    position: absolute;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 12px;
    box-sizing: border-box;
    background: var(--modal-scrim, var(--backdrop-scrim-55));
  }

  .kitdlg {
    width: 100%;
    max-width: 372px;
    max-height: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    box-sizing: border-box;
    border-radius: 10px;
    background: var(--modal-bg, var(--mainContentBackground, var(--program-bg)));
    /* A hairline in the tone colour, so the severity is carried by more than fill. */
    border: 1px solid var(--status-warn-border, var(--accent));
    box-shadow: 0 12px 40px var(--shadow-color-45);
  }

  .kitdlg--error {
    border-color: var(--status-error-border, var(--danger, #e5534b));
  }

  .kitdlg__title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--modal-body-fg, var(--whiteSecondary));
  }

  .kitdlg__body {
    min-height: 0;
    overflow-y: auto;
    font-size: 12px;
    line-height: 1.45;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }

  /* Stacked, full-width buttons: at 372px a row of three wraps unpredictably, and the
     primary action must never be the one that wraps out of sight. */
  .kitdlg__actions {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  :global(.kitdlg__actions button) {
    width: 100%;
    padding: 8px 10px;
    border-radius: 6px;
    border: 1px solid var(--modal-input-border, var(--button-bg));
    background: var(--button-bg);
    color: var(--modal-body-fg, var(--whiteSecondary));
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  :global(.kitdlg__actions button.is-primary) {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-contrast, #fff);
    font-weight: 600;
  }

  :global(.kitdlg__actions button:disabled) {
    opacity: 0.5;
    cursor: not-allowed;
  }

  :global(.kitdlg__actions button:focus-visible) {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 2px;
  }
</style>

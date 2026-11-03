<script lang="ts">
  import { tick, onMount } from "svelte";
  import { createEventDispatcher } from "svelte";
  import { fade, scale } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { DUR, motionEnabled } from "../../lib/animation";
  import { modalFocus } from "../../lib/modalFocus";
  import { t } from "../../stores/i18n";
  import {
    MODAL_FRAME_MIN_H,
    MODAL_FRAME_MIN_W,
    MODAL_FRAME_MIN_W_FOLDER,
    MODAL_FRAME_MIN_H_FOLDER,
    MODAL_RESIZE_EDGES,
    MODAL_RESIZE_CURSOR,
    clampRect,
    fitFrameToContent,
    getModalBounds,
    startModalDrag,
    startModalResize,
    type ModalFrameRect,
    type ResizeEdge,
  } from "../../lib/modalFrame";

  export let kind = "";
  export let title = "";
  export let modalId = 0;

  const dispatch = createEventDispatcher<{ cancel: void }>();

  $: modalMinSize = {
    minW: kind === "folder" ? MODAL_FRAME_MIN_W_FOLDER : MODAL_FRAME_MIN_W,
    minH: kind === "folder" ? MODAL_FRAME_MIN_H_FOLDER : MODAL_FRAME_MIN_H,
  };

  let backdropEl: HTMLDivElement | undefined;
  let modalFgEl: HTMLDivElement | undefined;
  let headerEl: HTMLElement | undefined;
  let modalFrame: ModalFrameRect = { left: 0, top: 0, width: 0, height: 0 };
  let frameReady = false;

  let lastInitId = -1;
  let contentResizeObserver: ResizeObserver | undefined;
  let refitQueued = false;
  let userAdjustedFrame = false;
  let titleId = "";

  function initModalFrame(): void {
    if (!backdropEl || !modalFgEl) return;
    modalFrame = fitFrameToContent(
      modalFgEl,
      getModalBounds(backdropEl),
      modalMinSize,
    );
    frameReady = true;
    attachContentObservers();
  }

  function refitModalFrame(): void {
    if (!frameReady || !backdropEl || !modalFgEl || userAdjustedFrame) return;
    contentResizeObserver?.disconnect();
    const next = fitFrameToContent(
      modalFgEl,
      getModalBounds(backdropEl),
      modalMinSize,
    );
    if (
      next.width !== modalFrame.width ||
      next.height !== modalFrame.height ||
      next.left !== modalFrame.left ||
      next.top !== modalFrame.top
    ) {
      modalFrame = next;
    }
    attachContentObservers();
  }

  function queueRefitModalFrame(): void {
    if (!frameReady || refitQueued || userAdjustedFrame) return;
    refitQueued = true;
    requestAnimationFrame(() => {
      refitQueued = false;
      refitModalFrame();
    });
  }

  function attachContentObservers(): void {
    contentResizeObserver?.disconnect();
    if (!modalFgEl) return;

    const scroll = modalFgEl.querySelector(".modal-scroll");
    if (!(scroll instanceof HTMLElement)) return;

    contentResizeObserver = new ResizeObserver(() => queueRefitModalFrame());
    for (const child of scroll.children) {
      if (child instanceof HTMLElement) {
        contentResizeObserver.observe(child);
      }
    }
  }

  function detachContentObservers(): void {
    contentResizeObserver?.disconnect();
    contentResizeObserver = undefined;
  }

  function reclampModalFrame(): void {
    if (!frameReady || !backdropEl) return;
    modalFrame = clampRect(modalFrame, getModalBounds(backdropEl), modalMinSize);
  }

  $: if (modalId !== lastInitId) {
    lastInitId = modalId;
    titleId = `modal-title-label-${modalId}`;
    detachContentObservers();
    frameReady = false;
    userAdjustedFrame = false;
    void tick().then(() =>
      requestAnimationFrame(() => {
        if (!backdropEl) return;
        initModalFrame();
      }),
    );
  }

  function onHeaderPointerDown(e: PointerEvent): void {
    if (!frameReady || !headerEl || !backdropEl) return;
    startModalDrag(e, headerEl, modalFrame, {
      bounds: getModalBounds(backdropEl),
      minSize: modalMinSize,
      onUpdate: (rect) => {
        userAdjustedFrame = true;
        modalFrame = rect;
      },
    });
  }

  function onResizePointerDown(e: PointerEvent, edge: ResizeEdge): void {
    if (!frameReady || !backdropEl) return;
    const handle = e.currentTarget;
    if (!(handle instanceof HTMLElement)) return;
    startModalResize(e, handle, edge, modalFrame, {
      bounds: getModalBounds(backdropEl),
      minSize: modalMinSize,
      onUpdate: (rect) => {
        userAdjustedFrame = true;
        modalFrame = rect;
      },
    });
  }

  function onBackdropDown(e: MouseEvent): void {
    if (e.target === backdropEl) dispatch("cancel");
  }

  function onCancel(): void {
    dispatch("cancel");
  }

  onMount(() => {
    window.addEventListener("resize", reclampModalFrame);
    return () => {
      window.removeEventListener("resize", reclampModalFrame);
      detachContentObservers();
    };
  });
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="modalBG"
  bind:this={backdropEl}
  on:mousedown={onBackdropDown}
  role="presentation"
  in:fade={{ duration: motionEnabled() ? DUR.fast : 0, easing: cubicOut }}
  out:fade={{ duration: motionEnabled() ? DUR.fast : 0, easing: cubicOut }}
>
  <div
    class="modalFG"
    class:modalFG--ready={frameReady}
    class:modalFilePicker={kind === "folder"}
    bind:this={modalFgEl}
    use:modalFocus={{ onEscape: onCancel }}
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
    style={frameReady
      ? `left:${modalFrame.left}px;top:${modalFrame.top}px;width:${modalFrame.width}px;height:${modalFrame.height}px`
      : undefined}
    in:scale={{ start: 0.96, duration: motionEnabled() ? DUR.normal : 0, easing: cubicOut, delay: motionEnabled() ? 20 : 0 }}
    out:scale={{ start: 0.96, duration: motionEnabled() ? DUR.fast : 0, easing: cubicOut }}
  >
    {#each MODAL_RESIZE_EDGES as edge (edge)}
      <div
        class="modal-resize-handle modal-resize-{edge}"
        style:cursor={MODAL_RESIZE_CURSOR[edge]}
        role="presentation"
        on:pointerdown={(e) => onResizePointerDown(e, edge)}
      ></div>
    {/each}
    <div class="modalFG-inner">
      <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
      <header class="modal-headerbar" bind:this={headerEl} on:pointerdown={onHeaderPointerDown}>
        <span id={titleId} class="modal-title-drag">
          {title}
        </span>
        <span class="modal-window-controls" role="toolbar">
          <button
            type="button"
            class="win-btn win-btn-close"
            aria-label={$t("Button_Close")}
            on:click={onCancel}
          >
            <svg class="win-btn__glyph win-btn__glyph--close" viewBox="0 0 10 10" aria-hidden="true">
              <path d="M2 2l6 6" />
              <path d="M8 2L2 8" />
            </svg>
          </button>
        </span>
      </header>

      <div class="modal-scroll">
        <slot />
      </div>
    </div>
  </div>
</div>

<style lang="scss">
  .modalBG {
    position: absolute;
    inset: 0;
    z-index: 50;
    background: var(--modal-scrim, var(--backdrop-scrim-55));
    padding: 1rem;
    box-sizing: border-box;
  }

  /*
    One dialog look for every modal: a rounded panel with a hairline edge and the panel shadow,
    the title as the dialog's own heading rather than a second title bar with a logo in it,
    and a single close control. Still draggable by its header and resizable from any edge.
  */
  .modalFG {
    display: flex;
    flex-direction: column;
    position: absolute;
    background: var(--modal-bg, var(--mainContentBackground, var(--program-bg)));
    border: var(--modal-frame-border-width, 1px) solid var(--modal-border, var(--hairline-strong));
    border-radius: var(--radius-xl);
    box-shadow: var(--shadow-panel, 0 10px 26px rgba(0, 0, 0, 0.5));
    overflow: visible;
    box-sizing: border-box;
    color: var(--fg-primary);
    font-size: var(--fs-body);
    line-height: var(--lh-body);
  }

  .modalFG-inner {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    height: 100%;
    overflow: hidden;
    border-radius: inherit;
  }

  .modalFG:not(.modalFG--ready) {
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    width: max-content;
    min-width: min(360px, calc(100% - 2rem));
    /* A readable line, not 80% of a maximised window: a one-sentence confirmation used to
       stretch to ~140 characters. The frame is fitted from this first layout. */
    max-width: min(560px, calc(100% - 2rem));
    max-height: min(75%, calc(100% - 2rem));
    height: auto;

    .modalFG-inner {
      flex: none;
      height: auto;
      overflow: visible;
    }

    .modal-scroll {
      flex: none;
      min-height: auto;
      overflow: visible;
    }
  }

  .modalFG.modalFG--ready {
    transform: none;
    min-width: 0;
    max-width: none;
    max-height: none;
  }

  .modalFG.modalFilePicker:not(.modalFG--ready) {
    min-width: min(720px, calc(100% - 2rem));
  }

  $modal-resize-handle: 16px;
  $modal-resize-outset: 8px;

  .modal-resize-handle {
    position: absolute;
    z-index: 3;
    touch-action: none;
  }

  .modal-resize-n,
  .modal-resize-s {
    left: -$modal-resize-outset;
    right: -$modal-resize-outset;
    height: $modal-resize-handle;
  }

  .modal-resize-n {
    top: -$modal-resize-outset;
    cursor: ns-resize;
  }

  .modal-resize-s {
    bottom: -$modal-resize-outset;
    cursor: ns-resize;
  }

  .modal-resize-e,
  .modal-resize-w {
    top: -$modal-resize-outset;
    bottom: -$modal-resize-outset;
    width: $modal-resize-handle;
  }

  .modal-resize-e {
    right: -$modal-resize-outset;
    cursor: ew-resize;
  }

  .modal-resize-w {
    left: -$modal-resize-outset;
    cursor: ew-resize;
  }

  .modal-resize-ne,
  .modal-resize-nw,
  .modal-resize-se,
  .modal-resize-sw {
    width: $modal-resize-handle;
    height: $modal-resize-handle;
    z-index: 4;
  }

  .modal-resize-ne {
    top: -$modal-resize-outset;
    right: -$modal-resize-outset;
    cursor: nesw-resize;
  }

  .modal-resize-nw {
    top: -$modal-resize-outset;
    left: -$modal-resize-outset;
    cursor: nwse-resize;
  }

  .modal-resize-se {
    bottom: -$modal-resize-outset;
    right: -$modal-resize-outset;
    cursor: nwse-resize;
  }

  .modal-resize-sw {
    bottom: -$modal-resize-outset;
    left: -$modal-resize-outset;
    cursor: nesw-resize;
  }

  .modal-headerbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    min-height: 52px;
    padding: var(--space-3) var(--space-3) 0 var(--space-6);
    /* Part of the panel, not a band across it: no fill, no rule underneath. */
    background: transparent;
    border: 0;
    color: var(--fg-primary);
    flex-shrink: 0;
    user-select: none;
    cursor: grab;
    position: relative;
    z-index: 2;
    touch-action: none;

    &:active {
      cursor: grabbing;
    }
  }

  .modal-title-drag {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    line-height: var(--lh-tight);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .modal-window-controls {
    display: flex;
    cursor: default;
  }

  .modal-window-controls .win-btn {
    border-radius: var(--radius-md);
    background: none;
    border: 0;
    margin: 0;
    display: flex;
    justify-content: center;
    align-items: center;
    width: 32px;
    height: 32px;
    cursor: pointer;
    padding: 0;
    color: var(--fg-muted);
    &:hover {
      background: var(--row-hover-strong);
      color: var(--fg-primary);
    }
    &:focus-visible {
      outline: 2px solid var(--role-focus-ring, var(--accent));
      outline-offset: 0;
    }
  }

  .modal-window-controls .win-btn__glyph {
    width: 10px;
    height: 10px;
    display: block;
    overflow: visible;
    color: currentColor;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.2;
    stroke-linecap: square;
    stroke-linejoin: miter;
    vector-effect: non-scaling-stroke;
    forced-color-adjust: auto;
  }

  .modal-scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-4) var(--space-6) var(--space-6);
  }

  :global(.modal-block) {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  :global(.modal-inline-actions) {
    display: flex;
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    margin-top: var(--space-3);

    button {
      min-width: 88px;
    }
  }

  /*
    Button order is a platform convention, so it is decided here rather than in each body.
    Windows puts the preferred action first ([OK] [Cancel]); macOS and GNOME put it last, next
    to the window edge. Bodies were written both ways, which is how the same app ended up
    asking "Cancel / Send" in one dialog and "Yes / No" in the next.
  */
  :global(.modal-inline-actions .modal-actions-spacer) {
    order: 0;
  }
  :global(.modal-inline-actions > .ss-btn) {
    order: 2;
  }
  :global(.modal-inline-actions > .ss-btn--primary) {
    order: 1;
  }
  :global(html:is([data-os="macos"], [data-os="linux"]) .modal-inline-actions > .ss-btn--primary) {
    order: 3;
  }

  :global(.modal-inline-actions .ss-btn:disabled) {
    opacity: 0.45;
    cursor: not-allowed;
    pointer-events: none;
  }

  :global(.modal-actions-spacer) {
    flex: 1;
    min-width: 0;
  }

  :global(.modal-input-row) {
    display: flex;
    width: 100%;
  }

  /* The same field as `.ss-field`, kept under its own name because theme packs target it. */
  :global(.modal-input) {
    flex: 1;
    min-height: var(--control-height);
    padding: 6px 11px;
    margin: 0;
    width: 100%;
    box-sizing: border-box;
    background: var(--modal-input-bg, var(--surface-field, var(--role-field-bg)));
    border: 1px solid var(--modal-input-border, var(--hairline-strong));
    border-radius: var(--radius-md);
    color: var(--modal-body-fg, var(--fg-primary));
    font: inherit;
    font-size: var(--fs-control);
    &:focus-visible {
      outline: 2px solid var(--role-focus-ring, var(--accent));
      outline-offset: 1px;
    }
  }

  :global(.modal-input--multiline) {
    min-height: 7.5rem;
    resize: vertical;
    line-height: 1.35;
    white-space: pre-wrap;
  }
</style>

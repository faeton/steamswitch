<script lang="ts">
  /**
   * The switch's progress and result (REDESIGN_BRIEF.md A6 "Switch experience", A13-J1).
   *
   * This is the piece the brief gives the most attention to, and the one the app was most
   * missing: a switch used to be communicated by *disabling a button*, so a multi-second
   * operation that closes and relaunches Steam looked like nothing was happening.
   *
   * It is a dock rather than a modal or an inline tile state, for three reasons: the operation
   * outlives whichever tile started it, it must stay legible while the user scrolls the grid,
   * and it has to be able to carry a result the user dismisses rather than one that vanishes.
   *
   * Progress appears within one frame of the click — `beginSwitch` sets the state before the
   * backend call is made — which is what A13's "visible within 200ms" asks for.
   */
  import { createEventDispatcher } from "svelte";
  import { t } from "../stores/i18n";
  import { clearError, statusStrip, dismissSwitch } from "../stores/statusStrip";
  import { motionEnabled } from "../lib/animation";
  import {
    SWITCH_STEP_LABEL_KEYS,
    stepIndex,
    stepMarks,
    stepPercent,
    visibleSteps,
  } from "../lib/steam/switchSteps";

  const dispatch = createEventDispatcher<{ done: void; action: string }>();

  const MARK_GLYPH = { done: "✓", active: "▸", pending: "·", skipped: "–" } as const;

  $: s = $statusStrip;
  /*
    The dock owns the whole switch, including its ending. A failed switch used to replace the
    `switching` state with `error`, which made the dock disappear at exactly the moment the
    user most needed it and left a one-line strip carrying "Switch failed · Retry".
  */
  $: open = s.kind === "switching" || (s.kind === "error" && s.scope === "switch");
  $: failed = s.kind === "error" && s.scope === "switch";
  $: finished = s.kind === "switching" && s.finished;
  /*
    The kit step only appears once the engine has reported kit work — most switches move no
    game settings, and a chip that stays pending forever describes work that never happens.
  */
  $: sawKit = s.kind === "switching" && s.step !== null && stepIndex(s.step) >= stepIndex("kit");
  // Observed after the switch, not derived from the step: the engine reports "starting Steam"
  // even when the AutoStart setting makes the launch a no-op.
  $: launched = s.kind === "switching" && s.launched;
  $: steps = visibleSteps(sawKit);
  $: marks = s.kind === "switching" ? stepMarks(s.step, s.finished, launched) : null;
  $: percent = s.kind === "switching" ? stepPercent(s.step, s.finished) : 0;

  function onDone(): void {
    dismissSwitch();
    dispatch("done");
  }
</script>

{#if failed && s.kind === "error"}
  <div class="dock dock--failed" role="alert" aria-live="assertive">
    <div class="dock__top">
      <div class="dock__lead">
        <div class="dock__title">{$t("Switch_Failed_Title")}</div>
        <div class="dock__sub">{s.message}</div>
      </div>
      <div class="dock__actions">
        {#if s.action}
          <button
            type="button"
            class="ss-btn ss-btn--primary"
            on:click={() => s.action && dispatch("action", s.action.id)}
          >
            {$t(s.action.labelKey)}
          </button>
        {/if}
        <button type="button" class="ss-btn" on:click={clearError}>{$t("Button_Close")}</button>
      </div>
    </div>
  </div>
{:else if open && s.kind === "switching"}
  <div class="dock" class:dock--done={finished} role="status" aria-live="polite">
    <div class="dock__top">
      <div class="dock__lead">
        <div class="dock__title">
          {finished
            ? $t("Switch_Done_Title", { name: s.toLabel })
            : $t("Switch_Running_Title", { name: s.toLabel })}
        </div>
        <div class="dock__sub">
          {#if finished}
            {launched
              ? $t("Switch_Done_Sub", { from: s.fromLabel || $t("Status_NoAccount") })
              : $t("Switch_Done_SubNoLaunch", { from: s.fromLabel || $t("Status_NoAccount") })}
          {:else}
            {s.phase || $t("Switch_Running_Sub", { from: s.fromLabel || $t("Status_NoAccount") })}
          {/if}
        </div>
      </div>

      {#if finished}
        <div class="dock__actions">
          <button type="button" class="ss-btn ss-btn--primary" on:click={onDone}>
            {$t("Switch_Done_Dismiss")}
          </button>
        </div>
      {/if}
    </div>

    <div class="dock__bar" aria-hidden="true">
      <div class="dock__bar-fill" class:dock__bar-fill--animated={motionEnabled()} style:width="{percent}%"></div>
    </div>

    <div class="dock__steps">
      {#each steps as id (id)}
        {@const mark = marks?.[id] ?? "pending"}
        <span class="step meta-mono" data-mark={mark}>
          <span class="step__glyph" aria-hidden="true">{MARK_GLYPH[mark]}</span>
          <span>
            {$t(SWITCH_STEP_LABEL_KEYS[id], {
              from: s.fromLabel || $t("Status_NoAccount"),
              to: s.toLabel,
            })}
          </span>
        </span>
      {/each}
    </div>

    {#if !finished}
      <!-- Not decoration: the swap writes Steam's live login files, and a window closed
           mid-write is exactly what leaves an interrupted transaction behind. -->
      <p class="dock__warn">{$t("Switch_Running_KeepOpen")}</p>
    {/if}
  </div>
{/if}

<style>
  .dock {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5) var(--space-7) var(--space-5);
    background: var(--surface-panel-raised, var(--footer-bg));
    border-top: 1px solid var(--hairline-strong, var(--border-bar-bg));
    box-shadow: var(--shadow-dock);
  }

  .dock--done {
    border-top-color: var(--border-ok);
  }

  .dock--failed {
    border-top-color: var(--border-fail);
  }

  .dock--failed .dock__title {
    color: var(--fg-fail);
  }

  .dock__actions {
    display: flex;
    gap: var(--space-3);
    flex: 0 0 auto;
  }

  .dock__top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-6);
  }

  .dock__lead {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .dock__title {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .dock--done .dock__title {
    color: var(--fg-ok);
  }

  .dock__sub {
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .dock__bar {
    height: 4px;
    border-radius: 3px;
    background: var(--button-bg);
    overflow: hidden;
  }

  .dock__bar-fill {
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
  }

  .dock--done .dock__bar-fill {
    background: var(--green);
  }

  .dock__bar-fill--animated {
    transition: width var(--dur-slow) var(--ease-out);
  }

  .dock__steps {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3) var(--space-6);
  }

  /*
    Each step carries a glyph as well as a colour, so the sequence is readable without colour
    perception and survives a heavily-tinted classic theme.
  */
  .step {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    color: var(--fg-disabled);
  }

  .step__glyph {
    width: 1em;
    text-align: center;
  }

  .step[data-mark="done"] {
    color: var(--fg-ok);
  }

  .step[data-mark="active"] {
    color: var(--fg-primary);
  }

  .step[data-mark="skipped"] {
    color: var(--fg-disabled);
    text-decoration: line-through;
  }

  .dock__warn {
    margin: 0;
    font-size: var(--fs-meta);
    color: var(--fg-muted);
  }

  @media (prefers-reduced-motion: reduce) {
    .dock__bar-fill--animated {
      transition: none;
    }
  }
</style>

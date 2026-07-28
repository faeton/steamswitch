<script lang="ts">
  /**
   * One account's health, as a state rather than a colour (REDESIGN_BRIEF.md A9).
   *
   * Colour is never the only carrier: the label spells the state out in words, so a
   * colour-blind user or a heavily-tinted classic theme loses nothing. The dot is the
   * at-a-glance layer on top of that, not instead of it.
   *
   * Visually distinct from the keyboard keycap on the switcher tile *by shape* — this is a
   * dot plus prose, that is a bordered key. The brief calls out that confusion specifically.
   */
  import { t } from "../../stores/i18n";
  import { healthState } from "../../lib/vault/health";
  import type { HealthReport } from "../../../bindings/steamswitch/internal/vault/models";

  export let report: HealthReport | null | undefined = null;
  export let checking = false;
  /** Show the "what to do next" line under the label. Off in dense rows. */
  export let showAction = false;

  $: state = healthState(report, { checking });
</script>

<span class="health" data-tone={state.tone}>
  <span class="health__row">
    <span class="health__dot" aria-hidden="true"></span>
    <span class="health__label">{$t(state.labelKey)}</span>
  </span>
  {#if showAction && state.actionKey}
    <span class="health__action">{$t(state.actionKey)}</span>
  {/if}
</span>

<style>
  .health {
    display: inline-flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .health__row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .health__dot {
    flex: 0 0 auto;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--dot-neutral);
  }

  .health__label {
    font-size: var(--fs-secondary);
    line-height: var(--lh-tight);
    color: var(--fg-neutral);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .health__action {
    font-size: var(--fs-meta);
    line-height: var(--lh-tight);
    color: var(--fg-muted);
  }

  .health[data-tone="ok"] .health__dot {
    background: var(--green);
  }
  .health[data-tone="ok"] .health__label {
    color: var(--fg-ok);
  }

  .health[data-tone="warn"] .health__dot {
    background: var(--orange);
  }
  .health[data-tone="warn"] .health__label {
    color: var(--fg-warn);
  }

  .health[data-tone="fail"] .health__dot {
    background: var(--red);
  }
  .health[data-tone="fail"] .health__label {
    color: var(--fg-fail);
  }

  .health[data-tone="busy"] .health__dot {
    background: var(--accent);
  }
  .health[data-tone="busy"] .health__label {
    color: var(--accent-text-bright, var(--accent));
  }

  :global(.animations-enabled) .health[data-tone="busy"] .health__dot {
    animation: health-pulse 1.1s ease-in-out infinite;
  }

  @keyframes health-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.animations-enabled) .health[data-tone="busy"] .health__dot {
      animation: none;
    }
  }
</style>

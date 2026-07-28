<script lang="ts">
  /**
   * One setting: what it is on the left, the control that changes it on the right
   * (REDESIGN_BRIEF.md A11 "a consistent control vocabulary … designed once and applied").
   *
   * The description is not optional decoration. Most of these settings act on files Steam
   * owns, and a row that says only "Run as admin" leaves the user to guess what it costs.
   * Room for that sentence is the reason the control sits beside the text rather than under it.
   */
  export let label: string;
  export let description = "";
  /** Marks a row whose effect is destructive or irreversible. */
  export let danger = false;
  /** Set when the control is a plain input the label should be bound to. */
  export let controlId = "";
</script>

<div class="row" class:row--danger={danger}>
  <div class="row__text">
    {#if controlId}
      <label class="row__label" for={controlId}>{label}</label>
    {:else}
      <span class="row__label">{label}</span>
    {/if}
    {#if description}
      <span class="row__desc">{description}</span>
    {/if}
    <slot name="detail" />
  </div>
  <div class="row__control">
    <slot />
  </div>
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    padding: 15px var(--space-5);
  }

  .row__text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .row__label {
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .row--danger .row__label {
    color: var(--fg-fail);
  }

  .row__desc {
    max-width: 62ch;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .row__control {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  /* Below the doc's minimum width the control drops under the text rather than squeezing the
     description into a column two words wide. */
  /* +190px nav +230px settings rail. */
  @media (max-width: 1140px) {
    .row {
      flex-direction: column;
      align-items: flex-start;
      gap: var(--space-3);
    }
  }
</style>

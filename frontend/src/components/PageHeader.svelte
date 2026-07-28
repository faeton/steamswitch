<script lang="ts">
  /**
   * The title row every top-level page opens with (REDESIGN_BRIEF.md A6).
   *
   * Shared rather than repeated so the title, the optional status pill beside it, and the
   * right-hand action cluster keep the same size, weight and spacing on Switcher, Vault,
   * Tools, Settings and About. Actions go in the `actions` slot; anything that qualifies the
   * title (a count, a lock state) goes in the default slot beside it.
   */
  export let title: string;
  /** Optional one-line explanation under the title. Prose, not a label. */
  export let description = "";
</script>

<header class="page-header">
  <div class="page-header__row">
    <div class="page-header__lead">
      <h1 class="page-header__title">{title}</h1>
      <slot />
    </div>
    <div class="page-header__actions">
      <slot name="actions" />
    </div>
  </div>
  {#if description}
    <p class="page-header__desc">{description}</p>
  {/if}
</header>

<style>
  .page-header {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-6) var(--space-7) var(--space-5);
  }

  .page-header__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    min-width: 0;
  }

  .page-header__lead {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-width: 0;
  }

  .page-header__title {
    margin: 0;
    font-size: var(--fs-title);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.005em;
    line-height: var(--lh-tight);
    color: var(--fg-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .page-header__actions {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 0 0 auto;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .page-header__desc {
    margin: 0;
    max-width: 640px;
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }
</style>

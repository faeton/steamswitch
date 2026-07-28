<script lang="ts">
  /**
   * A group of related settings (REDESIGN_BRIEF.md A11).
   *
   * Half of the "still looks like TcNo" tell was that Settings was one flat scrolling column
   * of `<h2 class="SettingsHeader">` followed by loose controls, with nothing tying a heading
   * to the rows beneath it. A card draws that boundary, so "which of these belong together"
   * is answered by the layout instead of by reading.
   */
  export let title = "";
  /** One line under the title. Explains what the group is *for*, not what each row does. */
  export let description = "";
</script>

<section class="card">
  {#if title || description}
    <header class="card__head">
      {#if title}<h2 class="card__title">{title}</h2>{/if}
      {#if description}<p class="card__desc">{description}</p>{/if}
    </header>
  {/if}
  <div class="card__body">
    <slot />
  </div>
</section>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .card__head {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .card__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .card__desc {
    margin: 0;
    max-width: 60ch;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  /*
    Rows are separated by hairlines inside one bordered surface rather than each being its own
    box: a settings page is a list of statements about one subject, and boxing every line
    makes ten unrelated things look like ten sections.
  */
  .card__body {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
    overflow: hidden;
  }

  .card__body > :global(* + *) {
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }
</style>

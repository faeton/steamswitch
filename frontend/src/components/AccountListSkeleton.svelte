<script lang="ts">
  /**
   * Placeholder rows while the account list loads.
   *
   * Same geometry as `AccountRow` — 52px rows, 32px avatar, two text lines — so nothing jumps
   * when the real list replaces it. It used to draw the inherited 100×135px card grid, which
   * the switcher had stopped using, so the first paint changed layout under the user.
   */
  import { t } from "../stores/i18n";

  export let count = 3;
</script>

<ul class="skeleton" aria-busy="true" aria-label={$t("Button_Loading")}>
  {#each Array.from({ length: count }) as _, i (i)}
    <li class="skeleton__row" aria-hidden="true">
      <span class="skeleton__key"></span>
      <span class="skeleton__block skeleton__avatar"></span>
      <span class="skeleton__lines">
        <span class="skeleton__block skeleton__line skeleton__line--name"></span>
        <span class="skeleton__block skeleton__line"></span>
      </span>
    </li>
  {/each}
</ul>

<style>
  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: 1px solid var(--hairline, var(--button-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
    pointer-events: none;
  }

  .skeleton__row {
    display: grid;
    grid-template-columns: 22px 32px minmax(0, 1fr);
    align-items: center;
    gap: var(--space-4);
    height: 52px;
    padding: 0 var(--space-4);
  }

  .skeleton__block {
    display: block;
    border-radius: var(--radius-xs);
    background: var(--row-hover-strong);
  }

  :global(.animations-enabled) .skeleton__block {
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

  .skeleton__avatar {
    width: 32px;
    height: 32px;
    border-radius: var(--radius-sm);
  }

  .skeleton__lines {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .skeleton__line {
    width: 140px;
    height: 10px;
  }

  .skeleton__line--name {
    width: 200px;
    height: 12px;
  }

  @keyframes skeleton-pulse {
    50% {
      opacity: 0.5;
    }
  }
</style>

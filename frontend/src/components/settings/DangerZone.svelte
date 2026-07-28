<script lang="ts">
  /**
   * Irreversible actions, kept visually apart (REDESIGN_BRIEF.md A11).
   *
   * Not a style flourish: these rows destroy data that has no backup by design — deleting the
   * vault is unrecoverable precisely *because* the encryption works. Putting them in the same
   * uniform list as "show account username" is how someone clicks one while skim-scrolling.
   */
  import { t } from "../../stores/i18n";

  export let title = "";
</script>

<section class="danger">
  <h2 class="danger__title">{title || $t("Settings_DangerZone")}</h2>
  <div class="danger__body">
    <slot />
  </div>
</section>

<style>
  .danger {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-5);
    border: 1px solid var(--border-fail);
    border-radius: var(--radius-lg);
    background: var(--bg-fail);
  }

  .danger__title {
    margin: 0;
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-fail);
  }

  .danger__body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  /* The row component's own padding is for a bordered card; inside the zone the border is on
     the zone itself, so the rows sit flush. */
  .danger__body :global(.row) {
    padding: 0;
  }
</style>

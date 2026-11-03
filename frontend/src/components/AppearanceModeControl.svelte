<script lang="ts">
  /**
   * The System / Light / Dark segmented control (REDESIGN.md §6).
   *
   * System is the default and follows `prefers-color-scheme` live. While a classic theme
   * pack is loaded the control still reflects the stored preference, but the pack supplies
   * the palette, so the row explains that rather than silently doing nothing.
   */
  import { t } from "../stores/i18n";
  import {
    appearanceMode,
    currentThemeId,
    isBaseThemeId,
    setUserAppearanceMode,
    type AppearanceMode,
  } from "../lib/themes";

  const MODES: { id: AppearanceMode; labelKey: string }[] = [
    { id: "system", labelKey: "Appearance_Mode_System" },
    { id: "light", labelKey: "Appearance_Mode_Light" },
    { id: "dark", labelKey: "Appearance_Mode_Dark" },
  ];

  $: overriddenByClassicTheme = !isBaseThemeId($currentThemeId);
</script>

<div class="rowDropdown">
  <span>{$t("Appearance_Mode")}</span>
  <div class="segmented" role="radiogroup" aria-label={$t("Appearance_Mode")}>
    {#each MODES as mode (mode.id)}
      <button
        type="button"
        role="radio"
        aria-checked={$appearanceMode === mode.id}
        class="segmented__btn"
        class:segmented__btn--on={$appearanceMode === mode.id}
        disabled={overriddenByClassicTheme}
        on:click={() => void setUserAppearanceMode(mode.id)}>{$t(mode.labelKey)}</button
      >
    {/each}
  </div>
</div>

{#if overriddenByClassicTheme}
  <p class="hint">{$t("Appearance_Mode_ClassicOverride")}</p>
{/if}

<style>
  /* A segmented control the way Windows 11 and macOS draw one: a recessed track, and the
     chosen segment raised out of it. The accent is kept for the switch and the primary
     button; three accent-filled blocks read as three buttons, not one choice. */
  .segmented {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-md);
    background: var(--surface-field, var(--role-field-bg));
  }
  .segmented__btn {
    min-height: 28px;
    margin: 0;
    padding: 0 14px;
    width: auto;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: var(--fs-control);
    line-height: var(--lh-tight);
    cursor: pointer;
  }
  .segmented__btn:hover:not(:disabled):not(.segmented__btn--on) {
    color: var(--fg-primary);
  }
  .segmented__btn--on {
    border-color: var(--hairline-strong, var(--button-bg));
    background: var(--button-bg);
    color: var(--fg-primary);
    font-weight: var(--fw-semibold);
    box-shadow: 0 1px 2px var(--shadow-color-30, rgba(0, 0, 0, 0.3));
  }
  .segmented__btn:disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: default;
  }
  .segmented__btn:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: 1px;
  }
  @media (forced-colors: active) {
    .segmented__btn--on {
      border-color: Highlight;
      background: Highlight;
      color: HighlightText;
    }
  }
  .hint {
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: var(--fs-secondary);
    margin: 4px 0 0;
  }
</style>

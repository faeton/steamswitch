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
  .segmented {
    display: inline-flex;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: 8px;
    overflow: hidden;
  }
  .segmented__btn {
    font: inherit;
    padding: 4px 12px;
    border: none;
    background: var(--button-bg);
    color: inherit;
    cursor: pointer;
  }
  .segmented__btn + .segmented__btn {
    border-left: 1px solid var(--hairline-strong, var(--button-bg));
  }
  .segmented__btn:hover:not(:disabled) {
    background: var(--button-bg-hover);
  }
  .segmented__btn--on {
    background: var(--accent);
    color: var(--text-on-bright-bg);
  }
  .segmented__btn:disabled {
    opacity: var(--role-disabled-opacity, 0.55);
    cursor: default;
  }
  .segmented__btn:focus-visible {
    outline: 2px solid var(--role-focus-ring, var(--accent));
    outline-offset: -2px;
  }
  .hint {
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 11px;
    margin: 4px 0 0;
  }
</style>

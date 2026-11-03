<script lang="ts">
  /**
   * One on/off setting: what it does on the left, a switch on the right — the row Windows 11
   * Settings uses, and the same shape as `SettingRow` on the Vault settings page.
   *
   * This used to be the inherited `input + label.form-check-label` pair: a hidden checkbox and
   * an empty label painted as a 20px box, followed by the text. The explanation lived in a
   * hover tooltip, so on a page of seventeen of these nobody could see what any of them cost
   * without pointing at each one. It is now written under the label.
   */
  export let id: string;
  export let checked: boolean = false;
  export let disabled: boolean = false;
  export let label: string = "";
  /** Shown under the label. Named `tooltip` for the callers written before it was visible. */
  export let tooltip: string = "";
</script>

<div class="toggle-row" class:toggle-row--disabled={disabled}>
  <label class="toggle-row__text" for={id}>
    <span class="toggle-row__label">{label}</span>
    {#if tooltip && tooltip !== label}
      <span class="toggle-row__desc">{tooltip}</span>
    {/if}
  </label>
  <input
    type="checkbox"
    role="switch"
    class="ss-toggle"
    {id}
    bind:checked
    {disabled}
    on:change
    on:click
  />
</div>

<style>
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    /* The settings pane is a flex column; without this a long description was squeezed back
       to the minimum height and spilled over the next row. */
    flex-shrink: 0;
    min-height: 52px;
    /* Lives inside a SettingsCard, which draws the dividers between rows. */
    padding: var(--space-3) var(--space-5);
    box-sizing: border-box;
  }

  .toggle-row__text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    /* Excluded from the legacy global `label` padding in Settings.scss. */
    margin: 0;
    cursor: pointer;
  }

  .toggle-row__label {
    font-size: var(--fs-body);
    line-height: var(--lh-tight);
    color: var(--fg-primary);
  }

  .toggle-row__desc {
    max-width: 62ch;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .toggle-row--disabled .toggle-row__text {
    cursor: not-allowed;
  }

  .toggle-row--disabled .toggle-row__label {
    color: var(--fg-disabled);
  }
</style>

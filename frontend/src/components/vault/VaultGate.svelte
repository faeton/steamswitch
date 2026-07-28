<script lang="ts">
  /**
   * What the Vault page shows when there is no table to show (REDESIGN_BRIEF.md A6, A10).
   *
   * The brief is specific that a locked vault must show an unlock gate and a first-run vault
   * must explain itself — "never a silent empty list". Three distinct states, and conflating
   * any two of them misleads:
   *
   *   - `unset`  — no app password on this machine. There is no vault yet, and nothing is
   *                hidden. Offering "unlock" here would imply data exists behind it.
   *   - `locked` — a vault exists and is sealed. The only way forward is the app password,
   *                and the copy has to say that forgetting it is unrecoverable *before* the
   *                user has anything to lose.
   *   - `empty`  — unlocked, working, no entries yet. This is a starting point, not a fault.
   */
  import { t } from "../../stores/i18n";

  export let state: "unset" | "locked" | "empty";

  const HEADING: Record<typeof state, string> = {
    unset: "Vault_Gate_Unset_Title",
    locked: "Vault_Gate_Locked_Title",
    empty: "Vault_Gate_Empty_Title",
  };
  const BODY: Record<typeof state, string> = {
    unset: "Vault_Gate_Unset_Body",
    locked: "Vault_Gate_Locked_Body",
    empty: "Vault_Gate_Empty_Body",
  };
</script>

<div class="gate" data-state={state}>
  <div class="gate__card">
    <h2 class="gate__title">{$t(HEADING[state])}</h2>
    <p class="gate__body">{$t(BODY[state])}</p>

    {#if state !== "empty"}
      <ul class="gate__points">
        <li>{$t("Vault_Gate_Point_Encrypted")}</li>
        <li>{$t("Vault_Gate_Point_Optional")}</li>
        <li>{$t("Vault_Gate_Point_Local")}</li>
      </ul>
      <p class="gate__warn">{$t("Vault_Gate_Point_Unrecoverable")}</p>
    {/if}

    <div class="gate__actions">
      <slot name="actions" />
    </div>
  </div>
</div>

<style>
  .gate {
    flex: 1 1 auto;
    min-height: 0;
    display: grid;
    place-items: center;
    padding: var(--space-6) var(--space-7);
    overflow-y: auto;
  }

  .gate__card {
    width: min(560px, 100%);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-6);
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-xl);
    background: var(--surface-panel, var(--mainContentBackground));
  }

  .gate__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .gate__body {
    margin: 0;
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-secondary);
  }

  .gate__points {
    margin: 0;
    padding-left: 1.2em;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  /*
    The unrecoverable-loss warning is styled as a warning and not as another bullet: it is
    the one line here whose consequence cannot be undone, and it has to be read before the
    user commits, not discovered afterwards.
  */
  .gate__warn {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border: 1px solid var(--border-warn);
    border-radius: var(--radius-md);
    background: var(--bg-warn);
    color: var(--fg-warn);
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
  }

  .gate__actions {
    display: flex;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .gate__actions:empty {
    display: none;
  }
</style>

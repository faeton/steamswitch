<script lang="ts">
  /**
   * The health report for one account, and the three ways to refresh it (VAULT.md §5).
   *
   * The split between the buttons is the point of the whole screen, ordered by cost:
   * **Check** is free and has no side effect; **Check session** signs in briefly with the
   * stored token to confirm it is still honoured (no password, no Guard email), so it is only
   * offered when a token is stored; **Verify** logs in for real and usually costs the user a
   * Steam Guard email. They are labelled and warned about differently for that reason, and
   * neither Check session nor Verify is ever triggered by anything but a click.
   */
  import { onMount } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { rankedSignals, verdictLabelKey } from "../../lib/vault/health";
  import { deepCheck, sessionCheck, quickCheck, vaultHealth, vaultStatus, vaultEntryFor } from "../../stores/vault";
  import type { HealthReport } from "../../../bindings/steamswitch/internal/vault/models";

  export let steamId64: string;

  let busy = false;
  const entry = vaultEntryFor(steamId64);

  $: report = ($vaultHealth[steamId64] ?? entry?.health ?? null) as HealthReport | null;
  $: signals = rankedSignals(report);

  onMount(() => {
    // Only if nothing has ever been recorded. Re-running on every open would spend the
    // user's API quota to redraw a screen they just closed.
    if (!report) {
      void run("quick");
    }
  });

  type CheckMode = "quick" | "session" | "deep";

  const failedToastKey: Record<CheckMode, string> = {
    quick: "Toast_Vault_CheckFailed",
    session: "Toast_Vault_SessionCheckFailed",
    deep: "Toast_Vault_VerifyFailed",
  };

  async function run(mode: CheckMode): Promise<void> {
    busy = true;
    try {
      if (mode === "deep") {
        await deepCheck(steamId64);
      } else if (mode === "session") {
        await sessionCheck(steamId64);
      } else {
        await quickCheck(steamId64);
      }
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t(failedToastKey[mode]), e),
        duration: 8000,
      });
    } finally {
      busy = false;
    }
  }

  function statusClass(status: string): string {
    return status === "ok" || status === "warn" || status === "fail" ? status : "unknown";
  }

  /**
   * The Go side models `Params` as `map[string]string`, which the bindings widen to allow
   * `undefined` values. The translator takes neither undefined values nor a missing map, so
   * the holes are dropped here rather than rendered as the literal word "undefined" inside a
   * sentence about someone's account.
   */
  function vars(params: { [k: string]: string | undefined } | null | undefined): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(params ?? {})) {
      if (v !== undefined) {
        out[k] = v;
      }
    }
    return out;
  }
</script>

<div class="health">
  {#if report}
    <p class="verdict {statusClass(report.verdict)}">
      <strong>{$t(verdictLabelKey(report.verdict))}</strong>
      <span class="small">{$t("Vault_Health_ProbedAt")}: {report.probedAt}</span>
    </p>

    <ul>
      {#each signals as s (s.name)}
        <li class={statusClass(s.status)}>
          <span class="dot" aria-hidden="true"></span>
          <span class="name">{$t(`Vault_SignalName_${s.name}`)}</span>
          <span class="detail">{$t(s.detail, vars(s.params))}</span>
          {#if s.blocker}
            <span class="blocker">{$t("Vault_Signal_Blocker")}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {:else}
    <p class="small">{$t("Vault_Health_NeverChecked")}</p>
  {/if}

  {#if !$vaultStatus.hasApiKey}
    <p class="small warnText">{$t("Vault_Health_NoAPIKey")}</p>
  {/if}

  <div class="actions">
    <button type="button" disabled={busy} on:click={() => run("quick")}>{$t("Vault_Action_QuickCheck")}</button>
    <button
      type="button"
      disabled={busy || $vaultStatus.rateLimited || !entry?.hasRefreshToken}
      title={$t("Vault_Action_SessionCheckTooltip")}
      on:click={() => run("session")}
    >
      {$t("Vault_Action_SessionCheck")}
    </button>
    <button
      type="button"
      class="danger"
      disabled={busy || $vaultStatus.rateLimited || !entry?.hasPassword}
      title={$t("Vault_Action_DeepCheckTooltip")}
      on:click={() => run("deep")}
    >
      {$t("Vault_Action_DeepCheck")}
    </button>
  </div>
  <p class="small">{$t("Vault_Action_DeepCheckWarning")}</p>
  {#if $vaultStatus.rateLimited}
    <p class="small warnText">{$t("Vault_Health_RateLimited")}</p>
  {/if}
</div>

<style lang="scss">
  .health {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-height: 60vh;
    overflow-y: auto;
  }

  .verdict {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    margin: 0;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  li {
    display: grid;
    grid-template-columns: auto minmax(6rem, auto) 1fr auto;
    gap: 0.4rem;
    align-items: baseline;
  }

  .dot {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    background: var(--border-bar-bg);
    align-self: center;
  }

  li.ok .dot {
    background: #3fa45b;
  }

  li.warn .dot {
    background: #d19a20;
  }

  li.fail .dot {
    background: #c0392b;
  }

  .name {
    font-weight: 600;
    font-size: 0.85rem;
  }

  .detail {
    font-size: 0.85rem;
    opacity: 0.9;
  }

  .blocker {
    font-size: 0.75rem;
    text-transform: uppercase;
    opacity: 0.85;
  }

  .small {
    font-size: 0.8rem;
    opacity: 0.85;
    margin: 0;
  }

  .warnText {
    opacity: 1;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.5rem;
  }
</style>

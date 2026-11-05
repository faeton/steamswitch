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
  import { Events } from "@wailsio/runtime";
  import { dismissModal } from "../../stores/modal";
  import { route } from "../../stores/nav";
  import { healthState, rankedSignals } from "../../lib/vault/health";
  import { checkActivity, deepCheck, sessionCheck, quickCheck, vaultEntries, vaultHealth, vaultStatus, vaultEntryFor } from "../../stores/vault";
  import type { ActivityStep, HealthReport } from "../../../bindings/steamswitch/internal/vault/models";

  export let steamId64: string;
  /**
   * Run a session check on opening, for Re-check. Only the session check: it sends no email, and
   * Verify password must stay a deliberate press because it usually does.
   */
  export let runSession = false;

  let busy = false;
  $: entry = $vaultEntries && vaultEntryFor(steamId64);

  $: report = ($vaultHealth[steamId64] ?? entry?.health ?? null) as HealthReport | null;
  $: signals = rankedSignals(report);
  $: state = healthState(report, { checking: busy });

  /** What the checks have done this session, newest first. Holds no secrets; see activity.go. */
  let steps: ActivityStep[] = [];
  async function loadActivity(): Promise<void> {
    const loaded = (await checkActivity(steamId64).catch(() => [])) ?? [];
    steps = mergeSteps(steps, loaded);
  }

  /** Newest first, each step once: events and the initial read can overlap and arrive unordered. */
  function mergeSteps(current: ActivityStep[], incoming: ActivityStep[]): ActivityStep[] {
    const bySeq = new Map([...current, ...incoming].map((s) => [s.seq, s]));
    return [...bySeq.values()].sort((a, b) => b.seq - a.seq);
  }

  function openKeySetting(): void {
    dismissModal();
    route.set({ page: "settings", category: "vault" });
  }

  function stepText(step: ActivityStep): string {
    return $t(step.key, vars(step.params));
  }

  function when(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  async function copyActivity(): Promise<void> {
    const text = steps
      .slice()
      .reverse()
      .map((s) => `${s.at} [${s.status}] ${stepText(s)}`)
      .join("\n");
    try {
      await navigator.clipboard.writeText(text);
      pushToast({ type: "success", message: $t("Toast_Copied"), duration: 2000 });
    } catch {
      /* Clipboard access can be refused; the list is on screen anyway. */
    }
  }

  onMount(() => {
    void loadActivity();
    const off = Events.On("vault-check-activity", (ev) => {
      const step = ev.data as ActivityStep | undefined;
      if (step?.steamId64 === steamId64) steps = mergeSteps(steps, [step]);
    });
    // Only if nothing has ever been recorded. Re-running on every open would spend the
    // user's API quota to redraw a screen they just closed.
    if (runSession) {
      void run("session");
    } else if (!report) {
      void run("quick");
    }
    return () => off();
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
        const rep = await deepCheck(steamId64);
        // The sign-in told us this login's SteamID, so the entry now lives under that.
        if (rep.movedTo) {
          steamId64 = rep.movedTo;
          await loadActivity();
        }
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
  <header class="summary" data-tone={state.tone}>
    <span class="dot" aria-hidden="true"></span>
    <div class="summary__text">
      <strong role="status">{$t(state.labelKey)}</strong>
      {#if state.actionKey && !busy}
        <span class="small">{$t(state.actionKey)}</span>
      {/if}
      {#if report}
        <span class="small">{$t("Vault_Health_ProbedAt")}: {when(report.probedAt)}</span>
      {/if}
    </div>
  </header>

  {#if signals.length > 0}
    <ul class="signals">
      {#each signals as s (s.name)}
        <li class={statusClass(s.status)}>
          <span class="dot" aria-hidden="true"></span>
          <span class="name">{$t(`Vault_SignalName_${s.name}`)}</span>
          <span class="detail">
            {$t(s.detail, vars(s.params))}
            {#if s.at}
              <span class="small">{$t("Vault_Signal_LearnedAt", { when: when(s.at) })}</span>
            {/if}
            {#if s.blocker}
              <span class="blocker">{$t("Vault_Signal_Blocker")}</span>
            {/if}
          </span>
        </li>
      {/each}
    </ul>
  {:else if !busy}
    <p class="small">{$t("Vault_Health_NeverChecked")}</p>
  {/if}

  {#if !$vaultStatus.hasApiKey}
    <div class="key-notice">
      <p class="small">{$t("Vault_Health_NoAPIKey")}</p>
      <button type="button" class="ss-btn" on:click={openKeySetting}>{$t("Vault_Action_AddApiKey")}</button>
    </div>
  {/if}

  <!-- Ordered by what each one costs, with the cost beside it. -->
  <div class="actions">
    <button class="ss-btn" type="button" disabled={busy} on:click={() => run("quick")}>
      {$t("Vault_Action_QuickCheck")}
    </button>
    <span class="small">{$t("Vault_Action_QuickCheckTooltip")}</span>

    <button
      class="ss-btn"
      type="button"
      disabled={busy || $vaultStatus.rateLimited || !entry?.hasRefreshToken}
      on:click={() => run("session")}
    >
      {$t("Vault_Action_SessionCheck")}
    </button>
    <span class="small">
      {entry?.hasRefreshToken ? $t("Vault_Action_SessionCheckTooltip") : $t("Vault_Action_SessionNeedsToken")}
    </span>

    <button
      class="ss-btn"
      type="button"
      disabled={busy || $vaultStatus.rateLimited || !entry?.hasPassword}
      on:click={() => run("deep")}
    >
      {$t("Vault_Action_DeepCheck")}
    </button>
    <span class="small">{$t("Vault_Action_DeepCheckTooltip")}</span>
  </div>

  {#if $vaultStatus.rateLimited}
    <p class="small warnText">{$t("Vault_Health_RateLimited")}</p>
  {/if}

  <details class="activity" open={busy || undefined}>
    <summary>{$t("Vault_Activity_Title")}{steps.length ? ` (${steps.length})` : ""}</summary>
    {#if steps.length === 0}
      <p class="small">{$t("Vault_Activity_Empty")}</p>
    {:else}
      <ol>
        {#each steps as step (step.seq)}
          <li class={statusClass(step.status)}>
            <span class="dot" aria-hidden="true"></span>
            <time class="small">{new Date(step.at).toLocaleTimeString()}</time>
            <span class="detail">{stepText(step)}</span>
          </li>
        {/each}
      </ol>
      <button type="button" class="ss-btn" on:click={copyActivity}>{$t("Vault_Activity_Copy")}</button>
    {/if}
  </details>
</div>

<style lang="scss">
  .health {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    /* The frame stops at 75% of the window. This leaves room inside it for the title and the
       Close button, which must never be cut off; the report scrolls instead. */
    max-height: calc(75vh - 180px);
    overflow-y: auto;
    text-align: left;
    min-width: min(480px, 80vw);
  }

  .small {
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
    margin: 0;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--dot-neutral, var(--fg-muted));
    flex: 0 0 auto;
  }

  .summary {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
  }

  .summary__text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .summary strong {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .summary[data-tone="ok"] .dot { background: var(--green); }
  .summary[data-tone="warn"] .dot { background: var(--orange); }
  .summary[data-tone="fail"] .dot { background: var(--red); }
  .summary[data-tone="busy"] .dot { background: var(--accent); }

  .signals,
  .activity ol {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .signals {
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .signals li {
    display: grid;
    grid-template-columns: auto 130px 1fr;
    gap: var(--space-3);
    align-items: baseline;
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
  }

  li.ok .dot { background: var(--green); }
  li.warn .dot { background: var(--orange); }
  li.fail .dot { background: var(--red); }

  .name {
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .detail {
    font-size: var(--fs-secondary);
    color: var(--fg-primary);
    min-width: 0;
  }

  .blocker {
    margin-left: var(--space-2);
    font-size: var(--fs-meta);
    font-weight: var(--fw-semibold);
    color: var(--fg-fail);
  }

  .actions {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-2) var(--space-3);
    align-items: center;
  }

  .actions .ss-btn {
    justify-content: center;
  }

  .key-notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .key-notice .ss-btn {
    flex: 0 0 auto;
  }

  .warnText {
    color: var(--fg-warn);
  }

  .activity summary {
    cursor: pointer;
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .activity ol {
    gap: 2px;
    margin: var(--space-2) 0;
    max-height: 11rem;
    overflow-y: auto;
  }

  .activity li {
    display: grid;
    grid-template-columns: auto auto 1fr;
    gap: var(--space-2);
    align-items: baseline;
  }

  .activity .detail {
    font-size: var(--fs-meta);
    color: var(--fg-secondary);
  }
</style>

<script lang="ts">
  /**
   * The persistent status strip (REDESIGN.md §3).
   *
   * One line, always present on the main surface: current account, switch narration,
   * kit-active warning, recovery prompts. Tone is carried by an icon and a text label as
   * well as colour — never colour alone.
   *
   * Actions are dispatched by id rather than handled here, so the host page owns what
   * "Restore theirs" or "Retry" actually does.
   */
  import { createEventDispatcher, onDestroy } from "svelte";
  import { t } from "../stores/i18n";
  import { statusStrip, statusTone, guardCode, type StatusAction } from "../stores/statusStrip";
  import { motionEnabled } from "../lib/animation";

  const dispatch = createEventDispatcher<{ action: string; copyCode: string }>();

  let copiedAt = 0;
  let copyFeedbackTimer: ReturnType<typeof setTimeout> | undefined;

  $: s = $statusStrip;
  $: tone = $statusTone;
  /*
    The strip and the dock must not narrate the same thing at once. The dock owns a switch
    from start to finish — progress, result, and failure — so the strip stands down for those
    and keeps what it is good at: the resting "Now: X · Steam running" line, kit state,
    recovery prompts, and failures that belong to no switch.
  */
  $: ownedByDock = s.kind === "switching" || (s.kind === "error" && s.scope === "switch");
  // Recovery is the one state the user must act on before anything else can proceed.
  $: assertive = s.kind === "recovery";
  // Idle carries no glyph: a mark on the resting state would train the eye to ignore it.
  $: toneGlyph = tone === "error" ? "✕" : tone === "warn" ? "!" : tone === "busy" ? "…" : "";

  function run(action: StatusAction): void {
    dispatch("action", action.id);
  }

  async function copyCode(code: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(code);
      copiedAt = Date.now();
      clearTimeout(copyFeedbackTimer);
      copyFeedbackTimer = setTimeout(() => {
        copiedAt = 0;
      }, 2000);
    } catch {
      // Clipboard denied: the code stays selectable on screen, which is the fallback.
    }
    dispatch("copyCode", code);
  }

  // The strip is mounted app-wide and survives navigation, but a pending "copied!" timer
  // would still fire into a destroyed component on teardown.
  onDestroy(() => clearTimeout(copyFeedbackTimer));
</script>

{#if !ownedByDock}
<div
  class="strip"
  data-tone={tone}
  class:animated={motionEnabled()}
  role="status"
  aria-live={assertive ? "assertive" : "polite"}
>
  <div class="strip__line">
    {#if toneGlyph}
      <!-- The severity has to survive a colour-blind user and a heavily themed palette, so
           it is carried by a glyph as well as by `data-tone`. Decorative: the accompanying
           text already states the condition, and a screen reader announcing "warning sign"
           before every kit line would be noise. -->
      <span class="strip__glyph" aria-hidden="true">{toneGlyph}</span>
    {/if}
    {#if s.kind === "idle"}
      <span class="strip__text">
        {$t("Status_Now", { name: s.accountLabel || $t("Status_NoAccount") })}
        {#if s.isHome}<span class="badge">{$t("Badge_Home")}</span>{/if}
      </span>
      <span class="strip__meta meta-mono">
        {s.steamRunning ? $t("Status_SteamRunning") : $t("Status_SteamClosed")}
      </span>
    {:else if s.kind === "switching"}
      <span class="strip__text">
        {s.phase || $t("Status_SwitchingTo", { name: s.toLabel })}
      </span>
      <span class="strip__meta meta-mono">{s.toLabel}</span>
    {:else if s.kind === "kit-active"}
      <span class="strip__text">
        {$t("Status_Kit_Active", { name: s.accountLabel })}
        {#if s.modules.length}<span class="strip__sep">·</span>{s.modules.join(", ")}{/if}
      </span>
      {#if s.cloudRisk}
        <span class="strip__meta">{$t("Status_Kit_CloudMayOverride")}</span>
      {/if}
      {#if s.nerd}
        <span class="strip__meta meta-mono">{s.nerd}</span>
      {/if}
    {:else if s.kind === "recovery"}
      <span class="strip__text">{s.detail}</span>
      <span class="strip__actions">
        {#each s.actions as action (action.id)}
          <button
            type="button"
            class="strip__btn"
            class:strip__btn--primary={action.primary}
            on:click={() => run(action)}>{$t(action.labelKey)}</button
          >
        {/each}
      </span>
    {:else if s.kind === "error"}
      <span class="strip__text">{s.message}</span>
      {#if s.action}
        <span class="strip__actions">
          <button type="button" class="strip__btn" on:click={() => s.action && run(s.action)}
            >{$t(s.action.labelKey)}</button
          >
        </span>
      {/if}
    {/if}
  </div>

  {#if $guardCode}
    {@const g = $guardCode}
    <div class="strip__line strip__line--guard">
      {#if g.state === "fetching"}
        <span class="strip__text"
          >{$t("Status_GuardCode_Fetching", { seconds: g.elapsedSeconds })}</span
        >
      {:else if g.state === "ready"}
        <span class="strip__text">{$t("Status_GuardCode_Label")}</span>
        <code class="strip__code meta-mono">{g.code}</code>
        <button
          type="button"
          class="strip__btn strip__btn--primary"
          on:click={() => copyCode(g.code)}
          >{copiedAt ? $t("Status_GuardCode_Copied") : $t("Status_GuardCode_Copy")}</button
        >
      {:else}
        <span class="strip__text">{g.message}</span>
      {/if}
    </div>
  {/if}
</div>
{/if}

<style>
  .strip {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px var(--main-padding, 0.75rem);
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
    background: var(--footer-bg, var(--mainContentBackground));
    color: var(--fg-secondary, var(--white));
    font-size: var(--fs-secondary);
    min-height: 30px;
    flex: 0 0 auto;
  }
  .strip.animated {
    transition:
      background-color 160ms ease-out,
      color 160ms ease-out;
  }
  .strip__line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .strip__glyph {
    flex: 0 0 auto;
    width: 14px;
    text-align: center;
    font-weight: 700;
    line-height: 1;
  }
  .strip__text {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .strip__meta {
    flex: 0 0 auto;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }
  .strip__sep {
    margin: 0 4px;
    color: var(--role-text-muted, var(--text-subtle-gray));
  }
  .strip__actions {
    display: flex;
    gap: 6px;
    flex: 0 0 auto;
  }
  .strip__btn {
    font: inherit;
    padding: 2px 10px;
    border-radius: 8px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    background: var(--button-bg);
    color: inherit;
    cursor: pointer;
  }
  .strip__btn:hover {
    background: var(--button-bg-hover);
  }
  .strip__btn--primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--text-on-bright-bg);
  }
  .strip__code {
    letter-spacing: 0.18em;
    font-size: 13px;
    padding: 1px 6px;
    border-radius: 6px;
    background: var(--role-field-bg, var(--code-background));
    user-select: text;
  }

  /*
    Tone is a *secondary* cue: every state above already spells out what is happening in
    words, so a user who cannot distinguish these colours loses nothing.
  */
  .strip[data-tone="busy"] {
    color: var(--accent-text-bright, var(--accent));
  }
  .strip[data-tone="warn"] {
    background: color-mix(in srgb, var(--orange) 12%, var(--footer-bg));
    border-top-color: var(--orange);
  }
  .strip[data-tone="error"] {
    background: color-mix(in srgb, var(--red) 12%, var(--footer-bg));
    border-top-color: var(--red);
  }

  @media (prefers-reduced-motion: reduce) {
    .strip.animated {
      transition: none;
    }
  }
</style>

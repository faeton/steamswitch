<script lang="ts">
  /**
   * "Who am I signed in as right now" (REDESIGN_BRIEF.md A4, J2).
   *
   * The brief is blunt that "show the live account unmistakably" is only *safe* if the design
   * admits the app does not always know Steam's ground truth. So this renders every state the
   * backend can distinguish, and the ones that matter most are those the old tinted-row could
   * not express at all:
   *
   *   running   — Steam is up as this account. Strongest treatment; switching will close it.
   *   closed    — the cache says this account, Steam is not running. Calm, not loud.
   *   none      — after an add-login, a failed switch, or a fresh install. Says "not signed
   *               in", rather than showing a stale last account as though it were live.
   *   mismatch  — Steam's own auto-login selection names somebody else. Somebody signed in
   *               outside SteamSwitch, or a switch stopped half way. Shows both candidates
   *               and offers a refresh; asserts neither.
   *   unknown   — the two sources disagree in a way that names nobody, or several accounts
   *               are marked current. The app cannot pick one and says so.
   *
   * A stale-but-confident "Signed in as X" is the specific failure this exists to prevent,
   * which is why `sessionState` comes from the backend's ResolveSession rather than being
   * inferred here from whether an account happens to be present.
   */
  import { createEventDispatcher } from "svelte";
  import { t, locale } from "../stores/i18n";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import { formatLastLoginForLocale } from "../lib/formatLastLogin";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { SessionState } from "../lib/steam/sessionState";

  /** The account Steam is set to log in as, or undefined when there is none. */
  export let account: SteamAccountRow | undefined = undefined;
  export let steamRunning = false;
  export let avatarEpoch = 0;
  /** False until the roster has loaded, so "no account" is not claimed prematurely. */
  export let loaded = false;
  export let busy = false;
  /** The backend's verdict. "ok" is the only one that permits a confident claim. */
  export let sessionState: SessionState = "none";
  /** The login name Steam's own selection carries when it disagrees with the file. */
  export let conflictAccountName = "";

  const dispatch = createEventDispatcher<{
    detail: string;
    launch: void;
    refresh: void;
  }>();

  $: state = !loaded
    ? "loading"
    : sessionState === "mismatch"
      ? "mismatch"
      : sessionState === "unknown"
        ? "unknown"
        : account
          ? steamRunning
            ? "running"
            : "closed"
          : "none";
  $: label = account
    ? account.displayName?.trim() || account.personaName?.trim() || account.steamId64
    : "";
  $: lastLogin = account?.lastLogin ? formatLastLoginForLocale(account.lastLogin, $locale) : "";
  // Under a mismatch the app knows two names and cannot rank them, so both are shown: the one
  // its own records point at, and the one Steam will actually sign in as.
  $: mismatchBody =
    conflictAccountName && label
      ? $t("Hero_Mismatch_Body_Both", { known: label, selected: conflictAccountName })
      : conflictAccountName
        ? $t("Hero_Mismatch_Body_Selected", { selected: conflictAccountName })
        : $t("Hero_Mismatch_Body_Unknown");
</script>

<section class="hero" data-state={state} aria-label={$t("Hero_Label")}>
  {#if state === "mismatch" || state === "unknown"}
    <!--
      The whole point of A4. Neither branch shows an avatar or a "Signed in as" eyebrow:
      those are the visual grammar of a confident answer, and reusing them here would be the
      same assertion in a warning colour. What is on offer instead is the fact and a refresh.
    -->
    <div class="hero__empty">
      <div class="hero__lead">
        <div class="ss-eyebrow hero__eyebrow--warn">
          {state === "mismatch" ? $t("Hero_Eyebrow_Mismatch") : $t("Hero_Eyebrow_Unknown")}
        </div>
        <div class="hero__none-title">
          {state === "mismatch" ? $t("Hero_Mismatch_Title") : $t("Hero_Unknown_Title")}
        </div>
        <p class="hero__none-body">
          {state === "mismatch" ? mismatchBody : $t("Hero_Unknown_Body")}
        </p>
      </div>
      <div class="hero__actions">
        <button
          type="button"
          class="ss-btn ss-btn--primary"
          disabled={busy}
          on:click={() => dispatch("refresh")}
        >
          {$t("Hero_Action_Refresh")}
        </button>
      </div>
    </div>
  {:else if state === "none"}
    <div class="hero__empty">
      <div class="hero__lead">
        <div class="ss-eyebrow">{$t("Hero_Eyebrow_NotSignedIn")}</div>
        <div class="hero__none-title">{$t("Hero_NotSignedIn_Title")}</div>
        <p class="hero__none-body">{$t("Hero_NotSignedIn_Body")}</p>
      </div>
      <div class="hero__actions">
        <!-- Refresh only. "Add another Steam login" is the page header's primary action,
             directly above this card; repeating it here put the same button on screen twice
             and made neither read as *the* thing to do. -->
        <button type="button" class="ss-btn" disabled={busy} on:click={() => dispatch("refresh")}>
          {$t("Hero_Action_Refresh")}
        </button>
      </div>
    </div>
  {:else if account}
    <div class="hero__identity">
      <SteamAccountAvatar {account} epoch={avatarEpoch} fallback="/img/BasicDefault.webp" />
      <div class="hero__lead">
        <div class="hero__top">
          <span class="ss-eyebrow hero__eyebrow">{$t("Hero_Eyebrow_SignedInAs")}</span>
          <!-- Steam's state is a pill of its own rather than a colour on the card: whether
               Steam is *running* changes what a switch will do to the user (it gets closed),
               so it has to be legible, not inferred. -->
          <span class="pill" data-running={steamRunning}>
            <span class="pill__dot" aria-hidden="true"></span>
            {steamRunning ? $t("Status_SteamRunning") : $t("Status_SteamClosed")}
          </span>
        </div>
        <div class="hero__name" title={label}>{label}</div>
        <div class="hero__meta meta-mono">
          {account.accountName ? `${account.accountName} · ` : ""}{account.steamId64}
        </div>
        {#if lastLogin && !steamRunning}
          <div class="hero__meta">{lastLogin}</div>
        {/if}
      </div>
    </div>

    <div class="hero__actions">
      <button
        type="button"
        class="ss-btn"
        on:click={() => dispatch("detail", account?.steamId64 ?? "")}
      >
        {$t("Hero_Action_Detail")}
      </button>
      {#if !steamRunning}
        <!-- Only offered when it would do something. A permanent Launch button on a surface
             whose whole point is "click an account" is noise. -->
        <button type="button" class="ss-btn" disabled={busy} on:click={() => dispatch("launch")}>
          {$t("Button_LaunchSteam")}
        </button>
      {/if}
    </div>
  {:else}
    <!-- Loading. A skeleton rather than an empty box, so the card does not resize under the
         grid the moment the roster arrives. -->
    <div class="hero__identity">
      <div class="hero__skeleton-avatar" aria-hidden="true"></div>
      <div class="hero__lead">
        <div class="hero__skeleton-line hero__skeleton-line--short" aria-hidden="true"></div>
        <div class="hero__skeleton-line" aria-hidden="true"></div>
      </div>
    </div>
  {/if}
</section>

<style>
  .hero {
    --avatar-size: var(--avatar-hero-size, 60px);
    --avatar-radius: 7px;
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    padding: var(--space-5);
    border: 1px solid var(--surface-hero-border, var(--hairline-strong));
    border-radius: var(--radius-xl);
    background: var(--surface-hero, var(--surface-panel));
    box-shadow: var(--shadow);
    min-width: 0;
  }

  /*
    The "we do not know" state is visually distinct from a confident one — softer border, no
    accent — so it never reads as an assertion about who is signed in.
  */
  .hero[data-state="none"] {
    border-style: dashed;
    border-color: var(--hairline-strong, var(--border-bar-bg));
    background: var(--surface-panel, var(--mainContentBackground));
  }

  /*
    Mismatch and unknown are warnings, not errors: nothing is broken and no data is at risk,
    the app simply cannot vouch for its answer. So they take the warn border and tint rather
    than the danger colours, which are reserved for a failed switch.
  */
  .hero[data-state="mismatch"],
  .hero[data-state="unknown"] {
    border-color: var(--border-warn);
    background: var(--bg-warn, var(--surface-panel));
  }

  .hero__eyebrow--warn {
    color: var(--fg-warn, var(--fg-muted));
  }

  .hero__identity {
    display: flex;
    align-items: center;
    gap: 18px;
    min-width: 0;
  }

  .hero__empty {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-6);
    width: 100%;
    min-width: 0;
  }

  .hero__lead {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .hero__top {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
  }

  .hero__eyebrow {
    color: var(--accent-text-bright, var(--fg-muted));
  }

  .hero__name {
    font-size: var(--fs-hero);
    font-weight: var(--fw-semibold);
    line-height: var(--lh-tight);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hero__none-title {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .hero__none-body {
    margin: 0;
    max-width: 52ch;
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .hero__meta {
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hero__actions {
    display: flex;
    gap: var(--space-3);
    flex: 0 0 auto;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 3px 9px;
    border: 1px solid var(--hairline-strong, var(--button-bg));
    border-radius: var(--radius-pill);
    font-size: var(--fs-meta);
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .pill__dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--dot-neutral);
  }

  .pill[data-running="true"] {
    border-color: var(--border-ok);
    background: var(--bg-ok);
    color: var(--fg-ok);
  }

  .pill[data-running="true"] .pill__dot {
    background: var(--green);
  }

  .hero__skeleton-avatar {
    width: var(--avatar-hero-size, 60px);
    height: var(--avatar-hero-size, 60px);
    border-radius: 7px;
    background: var(--button-bg);
  }

  .hero__skeleton-line {
    height: 12px;
    width: 220px;
    border-radius: var(--radius-xs);
    background: var(--button-bg);
  }

  .hero__skeleton-line--short {
    width: 110px;
  }

  /* At the doc's 1000px minimum the actions wrap under the identity rather than crushing the
     account name, which is the one thing on this card that must stay readable. */
  /* +190px nav, and the hero shares its row with a 340px detail panel when one is open. */
  @media (max-width: 1010px) {
    .hero,
    .hero__empty {
      flex-direction: column;
      align-items: flex-start;
    }
    .hero__actions {
      justify-content: flex-start;
    }
  }
</style>

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
  import { t } from "../stores/i18n";
  import SteamAccountAvatar from "./SteamAccountAvatar.svelte";
  import Icon from "./Icon.svelte";
  import type { SteamAccountRow } from "../lib/steam/types";
  import type { SessionState } from "../lib/steam/sessionState";
  import { heroLoginCopy, NO_SIGN_IN, type SignInStatus } from "../lib/steam/signIn";

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
  /**
   * Who a running switch is heading to, or "" when none is running.
   *
   * Mid-switch the login files are being rewritten, so "Signed in as A" is no longer true and
   * "B" is not true yet. The hero says that instead of asserting either.
   */
  export let switchingTo = "";
  /** Steam's answer on the last launch. The login files alone cannot say who is signed in. */
  export let signIn: SignInStatus = NO_SIGN_IN;

  const dispatch = createEventDispatcher<{
    detail: string;
    copyconfig: string;
    launch: void;
    refresh: void;
  }>();

  $: state = !loaded
    ? "loading"
    : switchingTo
      ? "switching"
      : sessionState === "mismatch"
      ? "mismatch"
      : sessionState === "unknown"
        ? "unknown"
        : account
          ? steamRunning
            ? "running"
            : "closed"
          : "none";
  $: loginCopy = heroLoginCopy(signIn, account?.steamId64 ?? "", steamRunning);
  $: label = account
    ? account.displayName?.trim() || account.personaName?.trim() || account.steamId64
    : "";
  // Under a mismatch the app knows two names and cannot rank them, so both are shown: the one
  // its own records point at, and the one Steam will actually sign in as.
  $: mismatchBody =
    conflictAccountName && label
      ? $t("Hero_Mismatch_Body_Both", { known: label, selected: conflictAccountName })
      : conflictAccountName
        ? $t("Hero_Mismatch_Body_Selected", { selected: conflictAccountName })
        : $t("Hero_Mismatch_Body_Unknown");
</script>

<section class="hero" data-state={state} aria-label={$t("Hero_Label")} aria-busy={state === "switching"}>
  {#if state === "switching"}
    <div class="hero__empty">
      <div class="hero__lead">
        <div class="hero__none-title">{$t("Hero_Switching_Title", { name: switchingTo })}</div>
        <p class="hero__none-body">
          {label ? $t("Hero_Switching_Body", { name: label }) : $t("Hero_Switching_Body_NoFrom")}
        </p>
      </div>
    </div>
  {:else if state === "mismatch" || state === "unknown"}
    <!--
      The whole point of A4. Neither branch shows an avatar or a "Signed in as" line:
      those are the visual grammar of a confident answer, and reusing them here would be the
      same assertion in a warning colour. What is on offer instead is the fact and a refresh.
    -->
    <div class="hero__empty">
      <div class="hero__lead">
        <div class="hero__none-title hero__none-title--warn">
          <Icon name="warn" size={15} />
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
        <div class="hero__none-title">{$t("Hero_NotSignedIn_Title")}</div>
        <p class="hero__none-body">{$t("Hero_NotSignedIn_Body")}</p>
      </div>
      <div class="hero__actions">
        <!-- Refresh only. "Add another Steam login" is in the page header directly above;
             repeating it here would put the same button on screen twice. -->
        <button type="button" class="ss-btn" disabled={busy} on:click={() => dispatch("refresh")}>
          {$t("Hero_Action_Refresh")}
        </button>
      </div>
    </div>
  {:else if account}
    <div class="hero__identity">
      <SteamAccountAvatar {account} epoch={avatarEpoch} fallback="/img/BasicDefault.webp" />
      <div class="hero__lead">
        <div
          class="hero__name"
          class:hero__name--local={!!account?.steamName}
          title={account?.steamName ? $t("Account_LocalName_Title", { name: account.steamName }) : label}
        >{label}</div>
        <div class="hero__meta">
          <span class="hero__login" class:hero__login--warn={loginCopy.warn}>
            {#if loginCopy.warn}<Icon name="warn" size={13} />{/if}
            {$t(loginCopy.key, { login: account.accountName || label })}
          </span>
          <!-- Steam's state is a pill of its own rather than a colour on the bar: whether
               Steam is *running* changes what a switch will do to the user (it gets closed),
               so it has to be legible, not inferred. -->
          <span class="pill" data-running={steamRunning}>
            <span class="pill__dot" aria-hidden="true"></span>
            {steamRunning ? $t("Status_SteamRunning") : $t("Status_SteamClosed")}
          </span>
        </div>
      </div>
    </div>

    <div class="hero__actions">
      <button
        type="button"
        class="ss-btn"
        disabled={busy}
        on:click={() => dispatch("copyconfig", account?.steamId64 ?? "")}
      >
        {$t("Hero_Action_CopyConfig")}
      </button>
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
  /*
    The current-account bar: one compact row, because the list below it is the point of the
    page. It used to be a 270px card with an uppercase eyebrow, a 60px avatar, the SteamID and
    a last-login date — the same account then appeared again as the first tile and a third
    time in the status strip.
  */
  .hero {
    --avatar-size: 44px;
    --avatar-radius: var(--radius-md);
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    min-height: 68px;
    padding: var(--space-4) var(--space-5);
    border: 1px solid var(--surface-hero-border, var(--hairline-strong));
    border-radius: var(--radius-lg);
    background: var(--surface-hero, var(--surface-panel));
    line-height: var(--lh-tight);
    min-width: 0;
  }

  /* "We do not know" must never look like a confident answer: no accent, no avatar. */
  .hero[data-state="none"] {
    border-style: dashed;
    border-color: var(--hairline-strong, var(--border-bar-bg));
    background: transparent;
  }

  /* Mismatch and unknown are warnings, not errors: nothing is broken, the app simply cannot
     vouch for its answer. The danger colours are kept for a failed switch. */
  .hero[data-state="mismatch"],
  .hero[data-state="unknown"] {
    border-color: var(--border-warn);
    background: var(--bg-warn, var(--surface-panel));
  }

  .hero[data-state="switching"] {
    border-color: var(--accent);
    background: var(--accent-fill-very-soft, var(--surface-panel));
  }

  .hero__identity {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-width: 0;
  }

  .hero__empty {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    width: 100%;
    min-width: 0;
  }

  .hero__lead {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .hero__name.hero__name--local {
    color: var(--accent-text-bright, var(--accent));
  }

  .hero__name {
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hero__none-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-body);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .hero__none-title--warn :global(.icon) {
    color: var(--fg-warn);
  }

  .hero__none-body {
    margin: 0;
    max-width: 68ch;
    font-size: var(--fs-secondary);
    line-height: var(--lh-body);
    color: var(--fg-muted);
  }

  .hero__meta {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
  }

  .hero__login {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hero__actions {
    display: flex;
    gap: var(--space-3);
    flex: 0 0 auto;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: 20px;
    padding: 0 var(--space-3);
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
    background: var(--fg-ok);
  }

  .hero__skeleton-avatar {
    width: 44px;
    height: 44px;
    border-radius: var(--radius-md);
    background: var(--row-hover-strong);
  }

  .hero__skeleton-line {
    height: 12px;
    width: 200px;
    border-radius: var(--radius-xs);
    background: var(--row-hover-strong);
  }

  .hero__skeleton-line--short {
    width: 120px;
  }

  .hero__login--warn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--fg-warn);
  }
</style>

<script lang="ts">
  /**
   * The app-lock gate (REDESIGN_BRIEF.md A10).
   *
   * This is the *app* gate — the startup lock, and `SecurityService.LockApp`. Behind it
   * nothing works: `security.RequireUnlocked` guards the account paths too, so listing,
   * switching and launching all fail while it is up. That is why it has no dismiss control.
   *
   * It is deliberately NOT what the "lock vault" controls call. Those call `LockVault`, which
   * seals the credential vault and leaves the switcher usable; its unlock prompt lives on the
   * Vault page, since there is no overlay in that state.
   *
   * This comment used to claim "switching accounts never needed a password", and the copy said
   * the same. Both were wrong, and believing them is how "lock vault" ended up raising a modal
   * that froze the whole app with no way out.
   *
   * The other thing the brief requires here: **a forgotten app password is unrecoverable, and
   * the user is told so** — before they are staring at a box they cannot get past. That is the
   * encryption working as intended, not a bug, and pretending otherwise sets up a support
   * conversation that ends in "there is nothing we can do".
   */
  import { modalFocus } from "../lib/modalFocus";
  import Icon from "./Icon.svelte";
  import { t } from "../stores/i18n";
  import {
    securityStatus,
    securityStatusLoaded,
    unlockApp,
  } from "../stores/security";

  let password = "";
  let loading = false;
  let error = "";
  let passwordEl: HTMLInputElement | undefined;
  const titleId = "app-lock-title";
  const passwordId = "app-lock-password";
  const errorId = "app-lock-error";

  async function submit(): Promise<void> {
    if (loading) return;
    error = "";
    loading = true;
    try {
      await unlockApp(password);
      password = "";
    } catch (e) {
      error = $t("Security_UnlockFailed");
      requestAnimationFrame(() => {
        passwordEl?.focus();
      });
    } finally {
      loading = false;
    }
  }

</script>

{#if $securityStatusLoaded && $securityStatus.appLocked}
  <div class="app-lock-overlay">
    <form
      class="app-lock-panel"
      use:modalFocus={{ initialFocus: () => passwordEl }}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      on:submit|preventDefault={submit}
    >
      <span class="app-lock-mark" aria-hidden="true"><Icon name="lock" size={20} /></span>
      <h2 id={titleId} class="app-lock-title">{$t("Security_Locked_Title")}</h2>
      <p class="app-lock-body">{$t("Security_Locked_Body")}</p>
      <p class="app-lock-sub">{$t("Security_Locked_Sub")}</p>

      <label class="app-lock-field" for={passwordId}>{$t("Security_Password")}</label>
      <input
        id={passwordId}
        bind:this={passwordEl}
        bind:value={password}
        type="password"
        autocomplete="current-password"
        class="ss-field"
        disabled={loading}
        aria-invalid={error ? "true" : undefined}
        aria-describedby={error ? errorId : undefined}
      />
      {#if error}
        <p id={errorId} class="app-lock-error" role="alert">
          <Icon name="warn" size={14} />{error}
        </p>
      {/if}
      <button type="submit" class="ss-btn ss-btn--primary app-lock-submit" disabled={loading}>
        {$t("Security_Unlock")}
      </button>

      <details class="app-lock-forgot">
        <summary>{$t("Security_Forgot_Summary")}</summary>
        <p class="app-lock-forgot-body">{$t("Security_Locked_Forgot")}</p>
      </details>
    </form>
  </div>
{/if}

<style lang="scss">
  .app-lock-overlay {
    position: absolute;
    inset: 0;
    z-index: 50;
    display: grid;
    place-items: center;
    padding: var(--space-5);
    box-sizing: border-box;
    background: var(--modal-scrim, var(--backdrop-scrim-55));
  }

  /* The same panel as every other dialog (ModalShell), minus the header: there is nothing to
     close and nowhere to drag it, so a title bar would only be decoration. */
  .app-lock-panel {
    width: min(400px, 100%);
    max-height: 100%;
    overflow: auto;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    padding: var(--space-7);
    border: 1px solid var(--modal-border, var(--hairline-strong));
    border-radius: var(--radius-xl);
    background: var(--modal-bg, var(--mainContentBackground, var(--program-bg)));
    box-shadow: var(--shadow-panel, 0 10px 26px rgba(0, 0, 0, 0.5));
    color: var(--fg-primary);
    font-size: var(--fs-body);
    line-height: var(--lh-body);
  }

  .app-lock-mark {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    margin-bottom: var(--space-5);
    border-radius: var(--radius-lg);
    background: var(--accent-fill-very-soft, var(--row-hover-strong));
    color: var(--accent-text-bright, var(--accent));
  }

  .app-lock-title {
    margin: 0 0 var(--space-2);
    font-size: var(--fs-title);
    font-weight: var(--fw-semibold);
    line-height: var(--lh-tight);
  }

  .app-lock-body {
    margin: 0 0 var(--space-2);
    color: var(--fg-secondary);
  }

  .app-lock-sub {
    margin: 0 0 var(--space-6);
    font-size: var(--fs-secondary);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }

  .app-lock-field {
    /* Excluded from the legacy global `label` padding in Settings.scss. */
    margin: 0 0 var(--space-2);
    font-size: var(--fs-secondary);
    font-weight: var(--fw-medium);
    color: var(--fg-secondary);
  }

  .app-lock-error {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-2) 0 0;
    font-size: var(--fs-secondary);
    color: var(--fg-fail);
  }

  .app-lock-submit {
    width: 100%;
    margin-top: var(--space-4);
  }

  /*
    Collapsed rather than always shown: the answer is bad news, and a user who *has* their
    password should not have to read it every launch. It is one click away, not hidden.
  */
  .app-lock-forgot {
    margin-top: var(--space-5);
    font-size: var(--fs-secondary);

    summary {
      cursor: pointer;
      user-select: none;
      color: var(--fg-muted);
      width: fit-content;
    }

    summary:hover {
      color: var(--fg-primary);
    }
  }

  .app-lock-forgot-body {
    margin: var(--space-2) 0 0;
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
    color: var(--fg-muted);
  }
</style>

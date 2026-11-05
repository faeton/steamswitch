<script lang="ts">
  /**
   * The vault editor for one account (REDESIGN_BRIEF.md A6 "Account editor", VAULT.md §3–4).
   *
   * Reached from three doors — Vault "Add entry" (blank), a tile's "Edit vault", and a row —
   * so it is one component, not three forms that drift.
   *
   * Two rules shape this and are easy to break by accident. Both are inherited from the modal
   * this replaces and neither is negotiable:
   *
   *  - **Secrets are never rendered until asked for.** The password field starts empty with a
   *    "stored" marker beside it, not with the stored value masked. A masked field still has
   *    the value sitting in the DOM.
   *  - **An untouched field is left alone.** Only fields the user actually edited are sent, so
   *    opening this and pressing Save cannot wipe a password that was never displayed. That is
   *    what the `touched` set is for, and why the Go draft uses pointers.
   *
   * New here versus the old modal: it opens blank (`steamId64 === ""`), which is what makes an
   * account with no home tile manageable at all (brief Part B #8), and the reveal delay and the
   * clipboard policy come from settings rather than a hardcoded constant (A10).
   *
   * The whole form is login and password, tabbed one after the other. Everything else is
   * optional and folded away, and says what it is for.
   */
  import { createEventDispatcher, onDestroy } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { clipboardClearEnabled, revealMs, vaultSecurityPrefs } from "../../stores/autoLock";
  import { CLIPBOARD_CLEAR_SECONDS, copySecret } from "../../lib/clipboardSecret";
  import { extractSteamId64, isValidSteamId64 } from "../../lib/steam/steamId";
  import { isOnThisMachine } from "../../lib/vault/filter";
  import { switcherRoster } from "../../stores/switcherRoster";
  import {
    VAULT_FIELDS,
    checkEmailDraft,
    detectEmail,
    revealField,
    linkSteamId,
    saveEntry,
    vaultEntries,
    vaultEntryFor,
    type VaultEntry,
    type VaultField,
  } from "../../stores/vault";
  import type { VaultEmailCheck, VaultEmailDetection } from "../../stores/vault";
  import { canCheckEmail, emailCheckView } from "../../lib/vault/emailCheck";

  /** SteamID64 to edit, or "" to open blank for a new entry. */
  export let steamId64: string;
  export let accountName = "";

  const dispatch = createEventDispatcher<{ saved: string; cancel: void; delete: VaultEntry }>();

  const existing: VaultEntry | undefined = steamId64 ? vaultEntryFor(steamId64) : undefined;
  const isNew = !existing;

  /** The real SteamID if there is one. A login-only entry's stand-in key is never shown. */
  let idInput = existing?.pending ? "" : steamId64;
  let label = existing?.label ?? "";
  let name = existing?.accountName ?? accountName;
  let source = existing?.source ?? "";
  let acquiredAt = existing?.acquiredAt ?? "";
  let secretNote = existing?.secretNote ?? "";

  let password = "";
  let sharedSecret = "";
  let identitySecret = "";

  let emailAddress = existing?.emailAddress ?? "";
  let emailSource = existing?.emailSource ?? "none";
  let imapHost = existing?.emailHost ?? "";
  let imapPort = existing?.emailPort || 993;
  let imapUser = existing?.emailUser ?? "";
  let imapPassword = "";
  let mailboxUrl = existing?.mailboxUrl ?? "";
  let mailboxToken = "";
  let mailboxId = existing?.mailboxId ?? "";

  let emailOpen = emailSource !== "none" || !!emailAddress;
  let serverOpen = emailSource === "mailbox-api";
  let authenticatorOpen = false;
  let detailsOpen = !!(label || source || acquiredAt || secretNote);

  /** Whose fault Save stopped: shown beside the field, so the button never just does nothing. */
  let loginError = "";
  let formError = "";
  let loginEl: HTMLInputElement | undefined;
  let idEl: HTMLInputElement | undefined;

  function focusNow(el: HTMLElement): void {
    if (isNew) requestAnimationFrame(() => el.focus());
  }

  /** Which inputs the user actually changed. Only these are sent. */
  const touched = new Set<string>();
  const EMAIL_FIELDS = new Set([
    "emailAddress",
    "emailSource",
    "imapHost",
    "imapPort",
    "imapUser",
    "imapPassword",
    "mailboxUrl",
    "mailboxToken",
    "mailboxId",
  ]);
  let emailResult: VaultEmailCheck | null = existing?.emailCheck ?? null;
  /** What this address's provider requires, once known. Replaced on each detection. */
  let emailDetection: VaultEmailDetection | null = null;
  $: emailResultView = emailCheckView(emailResult);
  /** Bumped by every edit to the email settings, so a slow answer about older ones is dropped. */
  let emailGeneration = 0;
  function touch(field: string): void {
    touched.add(field);
    // A result describes the settings it tested, so it goes as soon as they change.
    if (EMAIL_FIELDS.has(field)) {
      emailResult = null;
      emailGeneration += 1;
    }
    if (field === "accountName") loginError = "";
  }

  /** An address is enough to mean "this inbox", unless the user has said otherwise. */
  function onEmailBlur(): void {
    if (!touched.has("emailAddress")) return;
    if (emailSource === "none" && !touched.has("emailSource") && emailAddress.includes("@")) {
      emailSource = "imap";
      touched.add("emailSource");
    }
    if (emailSource === "imap") void autodetect(true);
  }

  let busy = false;
  let detecting = false;
  let checkingEmail = false;
  let revealed: { field: VaultField; value: string } | null = null;
  let revealSecondsLeft = 0;
  let revealTimer: ReturnType<typeof setInterval> | null = null;
  /**
   * Bumped by every clearReveal and by teardown.
   *
   * `revealField` is async, so without this a request in flight when the user blurs the
   * window, hides the value, or closes the editor would still land — putting the secret back
   * on screen and starting a fresh interval that nothing owns. Two overlapping reveals had
   * the same problem: only the newest timer handle stayed reachable, so the older one ran on
   * forever.
   */
  let revealGeneration = 0;
  let destroyed = false;
  let clipboardTimer: ReturnType<typeof setTimeout> | null = null;

  function clearReveal(): void {
    revealGeneration += 1;
    revealed = null;
    revealSecondsLeft = 0;
    if (revealTimer) {
      clearInterval(revealTimer);
      revealTimer = null;
    }
  }

  onDestroy(() => {
    destroyed = true;
    clearReveal();
    // Deliberately *not* clearing `clipboardTimer`. It holds no component state — just the
    // copied string — and cancelling it on teardown meant closing the editor within the
    // 20-second window left the password in the clipboard for good, which is the exact
    // outcome the setting exists to prevent.
  });

  /**
   * Reveal auto-hides on a visible countdown.
   *
   * The countdown is the point, not decoration: a secret that vanishes without warning reads
   * as a glitch, and one that lingers silently outlives the reason it was shown — the user
   * walks away, or screen-shares, and the panel is still open behind everything else.
   */
  async function reveal(field: VaultField): Promise<void> {
    clearReveal();
    if (!existing) return;
    const generation = revealGeneration;
    try {
      const value = await revealField(existing.steamId64, field);
      // Anything that cancelled the reveal while the call was in flight bumped the
      // generation. Dropping the value here is the whole point of the guard.
      if (destroyed || generation !== revealGeneration) return;
      if (!value) {
        pushToast({ type: "info", message: $t("Toast_Vault_NothingStored"), duration: 4000 });
        return;
      }
      revealed = { field, value };
      revealSecondsLeft = Math.round(revealMs() / 1000);
      revealTimer = setInterval(() => {
        revealSecondsLeft -= 1;
        if (revealSecondsLeft <= 0) clearReveal();
      }, 1000);
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_RevealFailed"), e),
        duration: 8000,
      });
    }
  }

  /** Re-hide whenever the window loses focus — the brief asks for this explicitly (A10). */
  function onWindowBlur(): void {
    clearReveal();
  }

  async function copyRevealed(): Promise<void> {
    if (!revealed) return;
    const outcome = await copySecret(revealed.value, {
      clearEnabled: clipboardClearEnabled,
      writeText: (v) => navigator.clipboard.writeText(v),
      readText: () => navigator.clipboard.readText(),
      setTimer: (fn, ms) => {
        if (clipboardTimer) clearTimeout(clipboardTimer);
        clipboardTimer = setTimeout(fn, ms);
        return clipboardTimer;
      },
    });
    if (outcome === "refused") {
      // Clipboard access can be refused; the value is on screen, so this is not fatal.
      return;
    }
    pushToast({
      type: "success",
      message:
        outcome === "copied-will-clear"
          ? $t("Toast_CopiedClearsIn", { seconds: CLIPBOARD_CLEAR_SECONDS })
          : $t("Toast_Copied"),
      duration: outcome === "copied-will-clear" ? 4000 : 2000,
    });
  }

  function draft(): Record<string, unknown> {
    const d: Record<string, unknown> = {
      steamId64: existing ? existing.steamId64 : extractSteamId64(idInput),
    };
    const put = (key: string, value: unknown): void => {
      if (touched.has(key)) d[key] = value;
    };
    put("accountName", name);
    // A login handed in from a tile was never typed, and still belongs to the new entry.
    if (isNew && name.trim()) d.accountName = name.trim();
    put("label", label);
    // Not a choice the user makes: Steam's own sign-ins decide the switcher. A new entry
    // records where the account actually stands now; an existing one is left alone.
    if (isNew) d.standalone = onThisPC !== true;
    put("password", password);
    put("sharedSecret", sharedSecret);
    put("identitySecret", identitySecret);
    put("emailAddress", emailAddress);
    put("emailSource", emailSource);
    put("imapHost", imapHost);
    put("imapPort", imapPort);
    put("imapUser", imapUser);
    put("imapPassword", imapPassword);
    put("mailboxUrl", mailboxUrl);
    put("mailboxToken", mailboxToken);
    put("mailboxId", mailboxId);
    put("source", source);
    put("acquiredAt", acquiredAt);
    put("secretNote", secretNote);
    return d;
  }

  /*
    Whether this account has a tile on this PC's switcher — read, never set.

    This used to be a "Show on switcher" toggle, defaulted on for a new entry. It could not do
    what it said: the switcher is the accounts Steam itself has signed into here, so flipping
    it on created no tile. `null` while there is no valid SteamID64 to ask about.
  */
  $: entryId = existing && !existing.pending ? existing.steamId64 : extractSteamId64(idInput);
  $: onThisPC = isValidSteamId64(entryId)
    ? existing
      ? isOnThisMachine(existing, $switcherRoster)
      : ($switcherRoster?.has(entryId) ?? null)
    : null;

  /** The ID box is only editable while there is no real SteamID yet. */
  $: idEditable = !existing || !!existing.pending;
  $: idError = idEditable && idInput.trim() !== "" && !extractSteamId64(idInput);

  /** Says why Save cannot go ahead, and takes the user to the field. Empty means it can. */
  function validate(): boolean {
    loginError = "";
    formError = "";
    if (idError) {
      detailsOpen = true;
      requestAnimationFrame(() => idEl?.focus());
      return false;
    }
    const login = name.trim().toLowerCase();
    // The login is all a login-only entry has to go on, so it cannot be emptied.
    if (existing?.pending && !login && !extractSteamId64(idInput)) {
      loginError = $t("Vault_Error_LoginRequired");
      loginEl?.focus();
      return false;
    }
    if (!isNew) return true;

    const id = extractSteamId64(idInput);
    if (!login && !id) {
      loginError = $t("Vault_Error_LoginRequired");
      loginEl?.focus();
      return false;
    }
    // Saving over an existing entry would quietly merge two accounts' details.
    const duplicate = $vaultEntries.find(
      (e) => (id && e.steamId64 === id) || (login && e.accountName?.trim().toLowerCase() === login),
    );
    if (duplicate) {
      loginError = $t("Vault_Error_AlreadyInVault", {
        login: duplicate.accountName || duplicate.steamId64,
      });
      loginEl?.focus();
      return false;
    }
    return true;
  }

  async function save(): Promise<void> {
    if (busy || !validate()) return;
    busy = true;
    try {
      const d = draft();
      await saveEntry(d);
      touched.clear();
      password = "";
      sharedSecret = "";
      identitySecret = "";
      imapPassword = "";
      mailboxToken = "";
      if (existing?.pending && extractSteamId64(idInput)) {
        await linkSteamId(existing.steamId64, extractSteamId64(idInput));
      }
      pushToast({ type: "success", message: $t("Toast_Vault_Saved"), duration: 3000 });
      dispatch("saved", String(d.steamId64));
    } catch (e) {
      formError = formatToastWithError($t("Toast_Vault_SaveFailed"), e);
    } finally {
      busy = false;
    }
  }

  async function runEmailCheck(): Promise<void> {
    busy = true;
    checkingEmail = true;
    const generation = emailGeneration;
    try {
      const check = await checkEmailDraft(draft());
      if (generation === emailGeneration) emailResult = check;
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_CheckFailed"), e),
        duration: 8000,
      });
    } finally {
      busy = false;
      checkingEmail = false;
    }
  }

  /**
   * Fill in the server from the address alone. `quiet` is the automatic run when the address
   * is finished or IMAP is chosen: it never replaces a host the user has typed and stays silent
   * if nothing answers, because nobody asked for it.
   */
  async function autodetect(quiet = false): Promise<void> {
    const address = emailAddress.trim();
    if (!address.includes("@")) {
      if (!quiet) {
        pushToast({ type: "info", message: $t("Toast_Vault_AutodetectNeedsAddress"), duration: 5000 });
      }
      return;
    }
    if (quiet && imapHost.trim()) return;
    detecting = true;
    const generation = emailGeneration;
    try {
      const found = await detectEmail(address);
      // Anything typed while this was out on the network is newer, and wins.
      if (generation !== emailGeneration) return;
      emailDetection = found;
      if (!found.found) serverOpen = found.access !== "no-imap";
      if (found.found && found.host && (!quiet || !imapHost.trim())) {
        imapHost = found.host;
        imapPort = found.port || 993;
        touch("imapHost");
        touch("imapPort");
        if (!imapUser.trim()) {
          imapUser = address;
          touch("imapUser");
        }
        if (!quiet) {
          pushToast({ type: "success", message: $t("Toast_Vault_AutodetectFound"), duration: 4000 });
        }
      } else if (!found.found && !quiet && found.access !== "no-imap") {
        pushToast({ type: "error", message: $t("Toast_Vault_AutodetectFailed"), duration: 8000 });
      }
    } catch (e) {
      if (!quiet) {
        pushToast({
          type: "error",
          message: formatToastWithError($t("Toast_Vault_AutodetectFailed"), e),
          duration: 8000,
        });
      }
    } finally {
      detecting = false;
    }
  }
</script>

<svelte:window on:blur={onWindowBlur} />

<aside class="editor" aria-label={isNew ? $t("Vault_Action_AddEntry") : $t("Vault_Editor_Title")}>
  <form class="editor__form" novalidate on:submit|preventDefault={save}>
  <header class="editor__head">
    <h2 class="editor__title">{isNew ? $t("Vault_Action_AddEntry") : $t("Vault_Editor_Title")}</h2>
    {#if existing}
      <span class="editor__subject">{existing.label || existing.accountName || existing.steamId64}</span>
    {/if}
  </header>

  <div class="editor__scroll">
    <section class="editor__section">
      <label class="field">
        <span class="field__label">{$t("Vault_Field_AccountName")}</span>
        <input
          type="text"
          class="ss-field ss-field--mono"
          bind:this={loginEl}
          bind:value={name}
          on:input={() => touch("accountName")}
          use:focusNow
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          aria-invalid={loginError ? "true" : undefined}
        />
        {#if loginError}
          <span class="field__error" role="alert">{loginError}</span>
        {/if}
      </label>

      <label class="field">
        <span class="field__head">
          <span class="field__label">{$t("Vault_Field_Password")}</span>
          {#if existing?.hasPassword}
            <span class="field__state field__state--set">{$t("Vault_Stored")}</span>
          {/if}
        </span>
        <span class="field__with-action">
          <input
            type="password"
            class="ss-field"
            bind:value={password}
            on:input={() => touch("password")}
            placeholder={existing?.hasPassword ? $t("Vault_Placeholder_Stored") : ""}
            autocomplete="new-password"
          />
          {#if existing?.hasPassword}
            <button type="button" class="ss-btn" disabled={busy} on:click={() => reveal(VAULT_FIELDS.password)}>
              {$t("Vault_Action_RevealFor", { seconds: $vaultSecurityPrefs.revealSeconds })}
            </button>
          {/if}
        </span>
      </label>
    </section>

    <section class="editor__section">
      <details bind:open={emailOpen}>
        <summary class="disclosure">
          <span class="disclosure__title">{$t("Vault_Section_Email")}</span>
          <span class="disclosure__state">
            {emailAddress.trim() || $t("Vault_Optional")}
          </span>
        </summary>

        <div class="disclosure__body">
          <p class="ss-help">{$t("Vault_Email_Explainer")}</p>
          <label class="field">
            <span class="field__label">{$t("Vault_Field_EmailAddress")}</span>
            <input
              type="text"
              class="ss-field"
              bind:value={emailAddress}
              on:input={() => touch("emailAddress")}
              on:blur={onEmailBlur}
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
            />
          </label>

          {#if emailSource === "imap"}
            {#if emailDetection?.access === "no-imap"}
              <p class="callout">
                {$t("Vault_EmailCheck_Unsupported", { provider: emailDetection.provider ?? "" })}
              </p>
            {:else if emailDetection?.access === "app-password"}
              <p class="ss-help">
                {$t("Vault_EmailHint_AppPassword", { provider: emailDetection.provider ?? "" })}
              </p>
            {/if}
            <label class="field">
              <span class="field__label">{$t("Vault_Field_IMAPPassword")}</span>
              <span class="field__with-action">
                <input
                  type="password"
                  class="ss-field"
                  bind:value={imapPassword}
                  on:input={() => touch("imapPassword")}
                  placeholder={existing?.hasEmailAuth ? $t("Vault_Placeholder_Stored") : ""}
                  autocomplete="new-password"
                />
                {#if existing?.hasEmailAuth}
                  <button
                    type="button"
                    class="ss-btn"
                    disabled={busy}
                    on:click={() => reveal(VAULT_FIELDS.emailPassword)}
                  >
                    {$t("Vault_Action_RevealFor", { seconds: $vaultSecurityPrefs.revealSeconds })}
                  </button>
                {/if}
              </span>
              <!-- Least privilege, stated where the credential is entered. -->
              <span class="ss-help">{$t("Vault_Email_AppPasswordWarning")}</span>
            </label>
            {#if emailDetection?.foreign && emailDetection.host === imapHost.trim()}
              <!-- The name came from an unverified certificate or DNS. This is the moment to
                   recognise it, before the password goes there. -->
              <p class="callout">{$t("Vault_EmailHint_Foreign", { host: emailDetection.host })}</p>
            {/if}
          {/if}

          {#if canCheckEmail(emailSource)}
            <div class="email-check">
              <button type="button" class="ss-btn" disabled={busy || detecting} on:click={runEmailCheck}>
                {checkingEmail ? $t("Vault_Verdict_Checking") : $t("Vault_Action_CheckEmail")}
              </button>
              {#if emailResultView && emailResult}
                <p class="email-check__result" data-tone={emailResultView.tone} role="status">
                  {$t(emailResultView.detailKey, emailResultView.params)}
                  <span class="email-check__when">
                    {$t("Vault_EmailCheck_When", {
                      when: new Date(emailResult.checkedAt).toLocaleString(),
                    })}
                  </span>
                </p>
              {/if}
            </div>
          {/if}

          <details class="nested" bind:open={serverOpen}>
            <summary class="disclosure disclosure--quiet">
              <span class="disclosure__title">{$t("Vault_Section_MailServer")}</span>
              {#if emailSource === "imap" && imapHost.trim()}
                <span class="disclosure__state">{imapHost.trim()}</span>
              {/if}
            </summary>
            <div class="disclosure__body">
              <label class="field">
                <span class="field__label">{$t("Vault_Field_EmailSource")}</span>
                <select
                  class="ss-field"
                  bind:value={emailSource}
                  on:change={() => {
                    touch("emailSource");
                    if (emailSource === "imap") void autodetect(true);
                  }}
                >
                  <option value="none">{$t("Vault_EmailSource_None")}</option>
                  <option value="imap">{$t("Vault_EmailSource_IMAP")}</option>
                  <option value="mailbox-api">{$t("Vault_EmailSource_Mailbox")}</option>
                  <option value="manual">{$t("Vault_EmailSource_Manual")}</option>
                </select>
              </label>

              {#if emailSource === "imap"}
                <label class="field">
                  <span class="field__label">{$t("Vault_Field_IMAPHost")}</span>
                  <span class="field__with-action">
                    <input
                      type="text"
                      class="ss-field ss-field--mono"
                      bind:value={imapHost}
                      on:input={() => touch("imapHost")}
                      autocomplete="off"
                    />
                    <button
                      type="button"
                      class="ss-btn"
                      disabled={busy || detecting}
                      on:click={() => void autodetect()}
                    >
                      {$t("Vault_Action_Autodetect")}
                    </button>
                  </span>
                </label>
                <div class="field-row">
                  <label class="field field--port">
                    <span class="field__label">{$t("Vault_Field_IMAPPort")}</span>
                    <input
                      type="number"
                      class="ss-field ss-field--mono"
                      bind:value={imapPort}
                      on:input={() => touch("imapPort")}
                      min="1"
                      max="65535"
                    />
                  </label>
                  <label class="field">
                    <span class="field__label">{$t("Vault_Field_IMAPUser")}</span>
                    <input
                      type="text"
                      class="ss-field ss-field--mono"
                      bind:value={imapUser}
                      on:input={() => touch("imapUser")}
                      autocomplete="off"
                    />
                  </label>
                </div>
              {:else if emailSource === "mailbox-api"}
                <p class="ss-help">{$t("Vault_Field_MailboxHint")}</p>
                <label class="field">
                  <span class="field__label">{$t("Vault_Field_MailboxURL")}</span>
                  <input
                    type="text"
                    class="ss-field ss-field--mono"
                    bind:value={mailboxUrl}
                    on:input={() => touch("mailboxUrl")}
                    autocomplete="off"
                  />
                </label>
                <label class="field">
                  <span class="field__label">{$t("Vault_Field_MailboxID")}</span>
                  <input
                    type="text"
                    class="ss-field ss-field--mono"
                    bind:value={mailboxId}
                    on:input={() => touch("mailboxId")}
                    autocomplete="off"
                  />
                </label>
                <label class="field">
                  <span class="field__label">{$t("Vault_Field_MailboxToken")}</span>
                  <input
                    type="password"
                    class="ss-field"
                    bind:value={mailboxToken}
                    on:input={() => touch("mailboxToken")}
                    placeholder={existing?.hasEmailAuth ? $t("Vault_Placeholder_Stored") : ""}
                    autocomplete="new-password"
                  />
                </label>
              {:else if emailSource === "manual"}
                <p class="ss-help">{$t("Vault_EmailSource_ManualHint")}</p>
              {/if}
            </div>
          </details>
        </div>
      </details>

      <details bind:open={authenticatorOpen}>
        <summary class="disclosure">
          <span class="disclosure__title">{$t("Vault_Section_Authenticator")}</span>
          <span class="disclosure__state" class:disclosure__state--set={existing?.hasSharedSecret}>
            {existing?.hasSharedSecret ? $t("Vault_Stored") : $t("Vault_Optional")}
          </span>
        </summary>
        <div class="disclosure__body">
          <p class="ss-help">{$t("Vault_Authenticator_Explainer")}</p>
          <label class="field">
            <span class="field__label">{$t("Vault_Field_SharedSecret")}</span>
            <span class="field__with-action">
              <input
                type="password"
                class="ss-field"
                bind:value={sharedSecret}
                on:input={() => touch("sharedSecret")}
                placeholder={existing?.hasSharedSecret ? $t("Vault_Placeholder_Stored") : ""}
                autocomplete="off"
              />
              {#if existing?.hasSharedSecret}
                <button
                  type="button"
                  class="ss-btn"
                  disabled={busy}
                  on:click={() => reveal(VAULT_FIELDS.sharedSecret)}
                >
                  {$t("Vault_Action_RevealFor", { seconds: $vaultSecurityPrefs.revealSeconds })}
                </button>
              {/if}
            </span>
            <span class="ss-help">{$t("Vault_Field_SharedSecretHint")}</span>
          </label>
          <label class="field">
            <span class="field__label">{$t("Vault_Field_IdentitySecret")}</span>
            <input
              type="password"
              class="ss-field"
              bind:value={identitySecret}
              on:input={() => touch("identitySecret")}
              placeholder={existing?.hasIdentitySecret ? $t("Vault_Placeholder_Stored") : ""}
              autocomplete="off"
            />
            <span class="ss-help">{$t("Vault_Field_IdentitySecretHint")}</span>
          </label>
        </div>
      </details>

      <details bind:open={detailsOpen}>
        <summary class="disclosure">
          <span class="disclosure__title">{$t("Vault_Section_Provenance")}</span>
          <span class="disclosure__state">{label.trim() || $t("Vault_Optional")}</span>
        </summary>
        <div class="disclosure__body">
          <label class="field">
            <span class="field__label">{$t("Vault_Field_Label")}</span>
            <input type="text" class="ss-field" bind:value={label} on:input={() => touch("label")} />
            <span class="ss-help">{$t("Vault_Field_LabelHint")}</span>
          </label>

          <label class="field">
            <span class="field__label">{$t("Vault_Field_SteamId")}</span>
            {#if idEditable}
              <input
                type="text"
                class="ss-field ss-field--mono"
                bind:this={idEl}
                bind:value={idInput}
                autocomplete="off"
                placeholder="76561198000000000"
                aria-invalid={idError ? "true" : undefined}
              />
              {#if idError}
                <span class="field__error" role="alert">{$t("Vault_Field_SteamId_Invalid")}</span>
              {:else}
                <span class="ss-help">{$t("Vault_Field_SteamId_Hint")}</span>
              {/if}
            {:else}
              <!-- Immutable once real: the SteamID64 is the entry's key, and "editing" it would
                   silently create a second entry while orphaning the first. -->
              <output class="ss-field ss-field--mono field__readonly">{existing?.steamId64}</output>
            {/if}
          </label>

          {#if onThisPC !== null}
            <div class="toggle-row">
              <span class="toggle-row__text">
                <span class="field__label">{$t("Vault_Field_OnThisPC")}</span>
                <span class="ss-help">{onThisPC ? $t("Vault_OnThisPC_Yes") : $t("Vault_OnThisPC_No")}</span>
              </span>
            </div>
          {/if}

          <label class="field">
            <span class="field__label">{$t("Vault_Field_Source")}</span>
            <input type="text" class="ss-field" bind:value={source} on:input={() => touch("source")} />
          </label>
          <label class="field">
            <span class="field__label">{$t("Vault_Field_AcquiredAt")}</span>
            <input
              type="text"
              class="ss-field ss-field--mono"
              bind:value={acquiredAt}
              on:input={() => touch("acquiredAt")}
              placeholder="YYYY-MM-DD"
            />
          </label>
          <label class="field">
            <span class="field__label">{$t("Vault_Field_SecretNote")}</span>
            <textarea
              class="ss-field field__textarea"
              rows="3"
              bind:value={secretNote}
              on:input={() => touch("secretNote")}
            ></textarea>
          </label>
        </div>
      </details>
    </section>
  </div>

  {#if revealed}
    <!-- Outside the scroll area, so it stays in view whichever folded section asked for it. -->
    <div class="revealed" role="status">
      <code class="revealed__value">{revealed.value}</code>
      <div class="revealed__actions">
        <button type="button" class="ss-btn" on:click={copyRevealed}>{$t("Vault_Action_Copy")}</button>
        <button type="button" class="ss-btn" on:click={clearReveal}>{$t("Vault_Action_Hide")}</button>
      </div>
      <span class="revealed__countdown meta-mono">
        {$t("Vault_Reveal_HidesIn", { seconds: Math.max(0, revealSecondsLeft) })}
      </span>
    </div>
  {/if}

  {#if formError}
    <p class="editor__error" role="alert">{formError}</p>
  {/if}

  <footer class="editor__foot">
    {#if existing}
      <button
        type="button"
        class="ss-btn ss-btn--danger ss-btn--quiet"
        disabled={busy}
        on:click={() => dispatch("delete", existing)}
      >
        {$t("Vault_Action_Delete")}
      </button>
    {/if}
    <span class="editor__foot-spacer"></span>
    <button type="button" class="ss-btn" disabled={busy} on:click={() => dispatch("cancel")}>
      {$t("Button_Cancel")}
    </button>
    <button type="submit" class="ss-btn ss-btn--primary" disabled={busy}>
      {$t("Vault_Action_Save")}
    </button>
  </footer>
  </form>
</aside>

<style>
  .editor {
    flex: 0 0 auto;
    width: 420px;
    max-width: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
    border: 1px solid var(--hairline-strong, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel-raised, var(--mainContentBackground));
    overflow: hidden;
  }

  /* Only here so Enter saves. The panel's own layout carries on as if it were not there. */
  .editor__form {
    display: contents;
  }

  .editor__error {
    flex: 0 0 auto;
    margin: 0;
    padding: var(--space-2) var(--space-5);
    border-top: 1px solid var(--border-fail);
    background: var(--bg-fail);
    color: var(--fg-fail);
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
  }

  .editor__head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: 14px var(--space-5);
    border-bottom: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .editor__title {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .editor__subject {
    font-size: var(--fs-secondary);
    color: var(--fg-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .editor__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .editor__section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .field-row {
    display: flex;
    gap: var(--space-4);
  }

  .field-row .field {
    flex: 1 1 0;
    min-width: 0;
  }

  .field-row .field--port {
    flex: 0 0 88px;
  }

  .field__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .field__label {
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  /* "stored / not stored" is a required state in the brief's field matrix: without it an
     empty password box is ambiguous between "nothing saved" and "saved, just not shown". */
  .field__state {
    font-size: var(--fs-meta);
    color: var(--fg-disabled);
  }

  .field__state--set {
    color: var(--fg-ok);
  }

  .field__error {
    font-size: var(--fs-meta);
    color: var(--fg-fail);
  }

  .field__with-action {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    min-width: 0;
  }

  .field__with-action .ss-field {
    flex: 1 1 auto;
    min-width: 0;
  }

  .field__readonly {
    display: flex;
    align-items: center;
    color: var(--fg-muted);
    background: var(--button-bg);
  }

  .field__textarea {
    min-height: 68px;
    padding: var(--space-2) 11px;
    resize: vertical;
  }

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: 10px var(--space-4);
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-md);
    background: var(--surface-field, transparent);
  }

  .toggle-row__text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }


  .disclosure {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: 11px var(--space-4);
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-md);
    background: var(--surface-field, transparent);
    font-size: var(--fs-control);
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .disclosure::-webkit-details-marker {
    display: none;
  }

  .disclosure::before {
    content: "›";
    flex: 0 0 auto;
    color: var(--fg-muted);
    transition: transform 0.1s ease;
  }

  details[open] > .disclosure::before {
    transform: rotate(90deg);
  }

  .disclosure__title {
    flex: 1 1 auto;
    min-width: 0;
  }

  .disclosure__state {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
    max-width: 55%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .disclosure__state--set {
    color: var(--fg-ok);
  }

  .disclosure--quiet {
    padding: 6px 0;
    border: 0;
    background: transparent;
    font-size: var(--fs-secondary);
    color: var(--fg-secondary);
  }

  .disclosure__body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding-top: var(--space-3);
  }

  .callout {
    margin: 0;
    padding: 10px var(--space-4);
    border: 1px solid var(--border-warn);
    border-radius: var(--radius-md);
    background: var(--bg-warn);
    color: var(--fg-warn);
    font-size: var(--fs-meta);
    line-height: var(--lh-prose);
  }

  .revealed {
    flex: 0 0 auto;
    margin: 0 var(--space-5) var(--space-3);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border: 1px solid var(--accent-overlay-border, var(--hairline-strong));
    border-radius: var(--radius-md);
    background: var(--surface-field, transparent);
  }

  .revealed__value {
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-control);
    word-break: break-all;
    user-select: text;
    color: var(--fg-primary);
  }

  .revealed__actions {
    display: flex;
    gap: var(--space-2);
  }

  .revealed__countdown {
    color: var(--fg-muted);
  }

  .editor__foot {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 13px var(--space-5);
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
  }

  .editor__foot-spacer {
    flex: 1 1 auto;
  }

  .email-check {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .email-check__result {
    margin: 0;
    font-size: var(--fs-meta);
    line-height: var(--lh-tight);
  }

  .email-check__result[data-tone="ok"] {
    color: var(--fg-ok);
  }
  .email-check__result[data-tone="warn"] {
    color: var(--fg-warn);
  }
  .email-check__result[data-tone="fail"] {
    color: var(--fg-fail);
  }

  .email-check__when {
    display: block;
    color: var(--fg-muted);
  }
</style>

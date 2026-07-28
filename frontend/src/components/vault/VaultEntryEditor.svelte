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
   * account with no home tile manageable at all (brief Part B #8); the reveal delay and the
   * clipboard policy come from settings rather than a hardcoded constant (A10); and the
   * sections match the brief's field matrix — identity required, everything else optional.
   */
  import { createEventDispatcher, onDestroy } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { clipboardClearEnabled, revealMs, vaultSecurityPrefs } from "../../stores/autoLock";
  import { isValidSteamId64 } from "../../lib/steam/steamId";
  import {
    VAULT_FIELDS,
    discoverIMAP,
    revealField,
    saveEntry,
    testEmailBinding,
    vaultEntryFor,
    type VaultEntry,
    type VaultField,
  } from "../../stores/vault";

  /** SteamID64 to edit, or "" to open blank for a new entry. */
  export let steamId64: string;
  export let accountName = "";

  const dispatch = createEventDispatcher<{ saved: string; cancel: void; delete: VaultEntry }>();

  const existing: VaultEntry | undefined = steamId64 ? vaultEntryFor(steamId64) : undefined;
  const isNew = !existing;

  let idInput = steamId64;
  let label = existing?.label ?? "";
  let name = existing?.accountName ?? accountName;
  /** The plain-language inverse of the stored `standalone` flag. */
  let showOnSwitcher = existing ? !existing.standalone : true;
  let source = existing?.source ?? "";
  let acquiredAt = existing?.acquiredAt ?? "";
  let secretNote = existing?.secretNote ?? "";

  let password = "";
  let sharedSecret = "";
  let identitySecret = "";

  let emailAddress = existing?.emailAddress ?? "";
  let emailSource = existing?.emailSource ?? "none";
  let imapHost = "";
  let imapPort = 993;
  let imapUser = "";
  let imapPassword = "";
  let mailboxUrl = "";
  let mailboxToken = "";
  let mailboxId = "";

  let emailOpen = emailSource !== "none";
  let provenanceOpen = !!(source || acquiredAt || secretNote);

  /** Which inputs the user actually changed. Only these are sent. */
  const touched = new Set<string>();
  function touch(field: string): void {
    touched.add(field);
  }

  let busy = false;
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
    const value = revealed.value;
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      // Clipboard access can be refused; the value is on screen, so this is not fatal.
      return;
    }
    if (!clipboardClearEnabled()) {
      pushToast({ type: "success", message: $t("Toast_Copied"), duration: 2000 });
      return;
    }
    const seconds = 20;
    pushToast({ type: "success", message: $t("Toast_CopiedClearsIn", { seconds }), duration: 4000 });
    if (clipboardTimer) clearTimeout(clipboardTimer);
    clipboardTimer = setTimeout(() => {
      /*
        Prefer to clear only what we put there — overwriting whatever the user copied since
        would be its own small betrayal. But the app denies clipboard *read* permission
        (`PermissionClipboardRead: PermissionDeny` in gui.go), so on the real build that read
        rejects and the earlier version silently swallowed it and cleared nothing at all.

        So: try the read-back, and if it is unavailable, clear anyway. The user turned this
        setting on; leaving a password on the clipboard indefinitely is the worse failure.
      */
      void navigator.clipboard
        .readText()
        .then((current) => {
          if (current === value) return navigator.clipboard.writeText("");
          return undefined;
        })
        .catch(() => navigator.clipboard.writeText("").catch(() => {}));
    }, seconds * 1000);
  }

  function draft(): Record<string, unknown> {
    const d: Record<string, unknown> = { steamId64: (existing?.steamId64 ?? idInput).trim() };
    const put = (key: string, value: unknown): void => {
      if (touched.has(key)) d[key] = value;
    };
    put("accountName", name);
    put("label", label);
    // Stored inverted: the field is "standalone" (vault-only), the control is "show on
    // switcher". Inverting at the boundary keeps the negation in one place.
    if (touched.has("showOnSwitcher")) d.standalone = !showOnSwitcher;
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
    A brand-new entry has to carry `standalone` explicitly rather than relying on the default,
    because the user's answer to "show this on the switcher" is a real decision — the brief
    defaults it on for a manual add. Marking it touched at construction is how it gets sent.
  */
  if (isNew) {
    touched.add("showOnSwitcher");
  }

  $: idError = isNew && idInput.trim() !== "" && !isValidSteamId64(idInput);
  $: canSave = !busy && (!isNew || (idInput.trim() !== "" && !idError));

  async function save(): Promise<void> {
    if (!canSave) return;
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
      pushToast({ type: "success", message: $t("Toast_Vault_Saved"), duration: 3000 });
      dispatch("saved", String(d.steamId64));
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_SaveFailed"), e),
        duration: 8000,
      });
    } finally {
      busy = false;
    }
  }

  async function testBinding(): Promise<void> {
    if (!existing) return;
    busy = true;
    try {
      await testEmailBinding(existing.steamId64);
      pushToast({ type: "success", message: $t("Toast_Vault_EmailOK"), duration: 4000 });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_EmailFailed"), e),
        duration: 8000,
      });
    } finally {
      busy = false;
    }
  }

  async function autodetect(): Promise<void> {
    if (!emailAddress.trim() || !imapPassword.trim()) {
      pushToast({ type: "info", message: $t("Toast_Vault_AutodetectNeedsCreds"), duration: 5000 });
      return;
    }
    busy = true;
    try {
      imapHost = await discoverIMAP(emailAddress, imapPassword);
      imapPort = 993;
      imapUser = emailAddress;
      touch("imapHost");
      touch("imapPort");
      touch("imapUser");
      pushToast({ type: "success", message: $t("Toast_Vault_AutodetectFound"), duration: 4000 });
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError($t("Toast_Vault_AutodetectFailed"), e),
        duration: 8000,
      });
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window on:blur={onWindowBlur} />

<aside class="editor" aria-label={isNew ? $t("Vault_Action_AddEntry") : $t("Vault_Editor_Title")}>
  <header class="editor__head">
    <h2 class="editor__title">{isNew ? $t("Vault_Action_AddEntry") : $t("Vault_Editor_Title")}</h2>
    {#if existing}
      <span class="editor__subject">{existing.label || existing.accountName || existing.steamId64}</span>
    {/if}
  </header>

  <div class="editor__scroll">
    <section class="editor__section">
      <h3 class="ss-eyebrow">{$t("Vault_Section_Identity")}</h3>

      <label class="field">
        <span class="field__label">{$t("Vault_Field_SteamId")}</span>
        {#if isNew}
          <input
            type="text"
            class="ss-field ss-field--mono"
            bind:value={idInput}
            inputmode="numeric"
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
          <!-- Immutable once set: the SteamID64 is the entry's key, and "editing" it would
               silently create a second entry while orphaning the first. -->
          <output class="ss-field ss-field--mono field__readonly">{existing?.steamId64}</output>
        {/if}
      </label>

      <div class="field-row">
        <label class="field">
          <span class="field__label">{$t("Vault_Field_AccountName")}</span>
          <input
            type="text"
            class="ss-field ss-field--mono"
            bind:value={name}
            on:input={() => touch("accountName")}
            autocomplete="off"
          />
        </label>
        <label class="field">
          <span class="field__label">{$t("Vault_Field_Label")}</span>
          <input type="text" class="ss-field" bind:value={label} on:input={() => touch("label")} />
        </label>
      </div>

      <div class="toggle-row">
        <span class="toggle-row__text">
          <span class="field__label">{$t("Vault_Field_ShowOnSwitcher")}</span>
          <span class="ss-help">{$t("Vault_Field_ShowOnSwitcher_Hint")}</span>
        </span>
        <input
          type="checkbox"
          class="ss-toggle"
          bind:checked={showOnSwitcher}
          on:change={() => touch("showOnSwitcher")}
          aria-label={$t("Vault_Field_ShowOnSwitcher")}
        />
      </div>
    </section>

    <section class="editor__section">
      <h3 class="ss-eyebrow">{$t("Vault_Section_Credentials")}</h3>

      <label class="field">
        <span class="field__head">
          <span class="field__label">{$t("Vault_Field_Password")}</span>
          <span class="field__state" class:field__state--set={existing?.hasPassword}>
            {existing?.hasPassword ? $t("Vault_Stored") : $t("Vault_NotStored")}
          </span>
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

      <label class="field">
        <span class="field__head">
          <span class="field__label">{$t("Vault_Field_SharedSecret")}</span>
          <span class="field__state" class:field__state--set={existing?.hasSharedSecret}>
            {existing?.hasSharedSecret ? $t("Vault_Stored") : $t("Vault_NotStored")}
          </span>
        </span>
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
      </label>

      {#if revealed}
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
    </section>

    <section class="editor__section">
      <details bind:open={emailOpen}>
        <summary class="disclosure">
          <span>{$t("Vault_Section_Email")}</span>
          <span class="disclosure__state">
            {emailSource === "none" ? $t("Vault_NotStored") : emailSource}
          </span>
        </summary>

        <div class="disclosure__body">
          <label class="field">
            <span class="field__label">{$t("Vault_Field_EmailAddress")}</span>
            <input
              type="text"
              class="ss-field"
              bind:value={emailAddress}
              on:input={() => touch("emailAddress")}
              autocomplete="off"
            />
          </label>

          <label class="field">
            <span class="field__label">{$t("Vault_Field_EmailSource")}</span>
            <select class="ss-field" bind:value={emailSource} on:change={() => touch("emailSource")}>
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
                <button type="button" class="ss-btn" disabled={busy} on:click={autodetect}>
                  {$t("Vault_Action_Autodetect")}
                </button>
              </span>
            </label>
            <div class="field-row">
              <label class="field">
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
            </label>
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

          {#if emailSource !== "none" && emailSource !== "manual"}
            <!-- Least privilege, stated where the credential is entered rather than buried in
                 docs: this is the moment someone is about to paste their main password. -->
            <p class="callout">{$t("Vault_Email_AppPasswordWarning")}</p>
            {#if existing}
              <button type="button" class="ss-btn" disabled={busy} on:click={testBinding}>
                {$t("Vault_Action_TestEmail")}
              </button>
            {/if}
          {/if}
        </div>
      </details>
    </section>

    <section class="editor__section">
      <details bind:open={provenanceOpen}>
        <summary class="disclosure">
          <span>{$t("Vault_Section_Provenance")}</span>
        </summary>
        <div class="disclosure__body">
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
    <button type="button" class="ss-btn ss-btn--primary" disabled={!canSave} on:click={save}>
      {$t("Vault_Action_Save")}
    </button>
  </footer>
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
    gap: var(--space-6);
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

  .disclosure__state {
    font-size: var(--fs-meta);
    color: var(--fg-muted);
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
</style>

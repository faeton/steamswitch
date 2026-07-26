<script lang="ts">
  /**
   * The vault editor for one account (VAULT.md §3, §4).
   *
   * Two rules shape this form and are easy to break by accident:
   *
   *  - **Secrets are never rendered until asked for.** The password field starts empty with a
   *    "set" marker beside it, not with the stored value masked. A masked field still has the
   *    value in the DOM.
   *  - **An untouched field is left alone.** Only fields the user actually edited are sent, so
   *    opening this dialog and pressing Save cannot wipe a password that was never displayed.
   *    That is what the `touched` set is for, and it is why the Go draft uses pointers.
   */
  import { onDestroy } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import {
    VAULT_FIELDS,
    deleteEntry,
    discoverIMAP,
    revealField,
    saveEntry,
    testEmailBinding,
    vaultEntryFor,
    type VaultField,
  } from "../../stores/vault";

  export let steamId64: string;
  export let accountName = "";

  const existing = vaultEntryFor(steamId64);

  let label = existing?.label ?? "";
  let name = existing?.accountName ?? accountName;
  let standalone = existing?.standalone ?? false;
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

  /** Which inputs the user actually changed. Only these are sent. */
  const touched = new Set<string>();
  function touch(field: string): void {
    touched.add(field);
  }

  let busy = false;
  let revealed: { field: VaultField; value: string } | null = null;
  let revealTimer: ReturnType<typeof setTimeout> | null = null;

  /**
   * Reveal auto-hides. A secret left on screen outlives the reason it was shown — the user
   * walks away, or screen-shares, and the dialog is still open behind everything else.
   */
  const REVEAL_MS = 20_000;

  function clearReveal(): void {
    revealed = null;
    if (revealTimer) {
      clearTimeout(revealTimer);
      revealTimer = null;
    }
  }

  onDestroy(clearReveal);

  async function reveal(field: VaultField): Promise<void> {
    clearReveal();
    try {
      const value = await revealField(steamId64, field);
      if (!value) {
        pushToast({ type: "info", message: $t("Toast_Vault_NothingStored"), duration: 4000 });
        return;
      }
      revealed = { field, value };
      revealTimer = setTimeout(clearReveal, REVEAL_MS);
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_RevealFailed"), e), duration: 8000 });
    }
  }

  async function copyRevealed(): Promise<void> {
    if (!revealed) {
      return;
    }
    try {
      await navigator.clipboard.writeText(revealed.value);
      pushToast({ type: "success", message: $t("Toast_Copied"), duration: 2000 });
    } catch {
      // Clipboard access can be refused; the value is on screen, so this is not fatal.
    }
  }

  function draft(): Record<string, unknown> {
    const d: Record<string, unknown> = { steamId64 };
    const put = (key: string, value: unknown): void => {
      if (touched.has(key)) {
        d[key] = value;
      }
    };
    put("accountName", name);
    put("label", label);
    put("standalone", standalone);
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

  async function save(): Promise<void> {
    busy = true;
    try {
      await saveEntry(draft());
      touched.clear();
      password = "";
      sharedSecret = "";
      identitySecret = "";
      imapPassword = "";
      mailboxToken = "";
      pushToast({ type: "success", message: $t("Toast_Vault_Saved"), duration: 3000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_SaveFailed"), e), duration: 8000 });
    } finally {
      busy = false;
    }
  }

  async function testBinding(): Promise<void> {
    busy = true;
    try {
      await testEmailBinding(steamId64);
      pushToast({ type: "success", message: $t("Toast_Vault_EmailOK"), duration: 4000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_EmailFailed"), e), duration: 8000 });
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
      const host = await discoverIMAP(emailAddress, imapPassword);
      imapHost = host;
      imapPort = 993;
      imapUser = emailAddress;
      touch("imapHost");
      touch("imapPort");
      touch("imapUser");
      pushToast({ type: "success", message: $t("Toast_Vault_AutodetectFound"), duration: 4000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_AutodetectFailed"), e), duration: 8000 });
    } finally {
      busy = false;
    }
  }

  async function removeEntry(): Promise<void> {
    busy = true;
    try {
      await deleteEntry(steamId64);
      pushToast({ type: "success", message: $t("Toast_Vault_Deleted"), duration: 3000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Vault_DeleteFailed"), e), duration: 8000 });
    } finally {
      busy = false;
    }
  }
</script>

<div class="vaultEditor">
  <p class="hint">{$t("Vault_Editor_Hint")}</p>

  <label class="row">
    <span>{$t("Vault_Field_Label")}</span>
    <input type="text" bind:value={label} on:input={() => touch("label")} />
  </label>

  <label class="row">
    <span>{$t("Vault_Field_AccountName")}</span>
    <input type="text" bind:value={name} on:input={() => touch("accountName")} autocomplete="off" />
  </label>

  <label class="check">
    <input type="checkbox" bind:checked={standalone} on:change={() => touch("standalone")} />
    <span>{$t("Vault_Field_Standalone")}</span>
  </label>
  <p class="hint small">{$t("Vault_Field_StandaloneHint")}</p>

  <h3>{$t("Vault_Section_Credentials")}</h3>

  <label class="row">
    <span>{$t("Vault_Field_Password")}</span>
    <span class="withAction">
      <input
        type="password"
        bind:value={password}
        on:input={() => touch("password")}
        placeholder={existing?.hasPassword ? $t("Vault_Placeholder_Stored") : ""}
        autocomplete="new-password"
      />
      {#if existing?.hasPassword}
        <button type="button" disabled={busy} on:click={() => reveal(VAULT_FIELDS.password)}>
          {$t("Vault_Action_Reveal")}
        </button>
      {/if}
    </span>
  </label>

  <label class="row">
    <span>{$t("Vault_Field_SharedSecret")}</span>
    <span class="withAction">
      <input
        type="password"
        bind:value={sharedSecret}
        on:input={() => touch("sharedSecret")}
        placeholder={existing?.hasSharedSecret ? $t("Vault_Placeholder_Stored") : ""}
        autocomplete="off"
      />
      {#if existing?.hasSharedSecret}
        <button type="button" disabled={busy} on:click={() => reveal(VAULT_FIELDS.sharedSecret)}>
          {$t("Vault_Action_Reveal")}
        </button>
      {/if}
    </span>
  </label>
  <p class="hint small">{$t("Vault_Field_SharedSecretHint")}</p>

  <label class="row">
    <span>{$t("Vault_Field_IdentitySecret")}</span>
    <input
      type="password"
      bind:value={identitySecret}
      on:input={() => touch("identitySecret")}
      placeholder={existing?.hasIdentitySecret ? $t("Vault_Placeholder_Stored") : ""}
      autocomplete="off"
    />
  </label>

  {#if revealed}
    <div class="revealed">
      <code>{revealed.value}</code>
      <button type="button" on:click={copyRevealed}>{$t("Vault_Action_Copy")}</button>
      <button type="button" on:click={clearReveal}>{$t("Vault_Action_Hide")}</button>
      <span class="small">{$t("Vault_Reveal_AutoHides")}</span>
    </div>
  {/if}

  <h3>{$t("Vault_Section_Email")}</h3>

  <label class="row">
    <span>{$t("Vault_Field_EmailAddress")}</span>
    <input type="text" bind:value={emailAddress} on:input={() => touch("emailAddress")} autocomplete="off" />
  </label>

  <label class="row">
    <span>{$t("Vault_Field_EmailSource")}</span>
    <select bind:value={emailSource} on:change={() => touch("emailSource")}>
      <option value="none">{$t("Vault_EmailSource_None")}</option>
      <option value="imap">{$t("Vault_EmailSource_IMAP")}</option>
      <option value="mailbox-api">{$t("Vault_EmailSource_Mailbox")}</option>
    </select>
  </label>

  {#if emailSource === "imap"}
    <label class="row">
      <span>{$t("Vault_Field_IMAPHost")}</span>
      <span class="withAction">
        <input type="text" bind:value={imapHost} on:input={() => touch("imapHost")} autocomplete="off" />
        <button type="button" disabled={busy} on:click={autodetect}>{$t("Vault_Action_Autodetect")}</button>
      </span>
    </label>
    <label class="row">
      <span>{$t("Vault_Field_IMAPPort")}</span>
      <input type="number" bind:value={imapPort} on:input={() => touch("imapPort")} min="1" max="65535" />
    </label>
    <label class="row">
      <span>{$t("Vault_Field_IMAPUser")}</span>
      <input type="text" bind:value={imapUser} on:input={() => touch("imapUser")} autocomplete="off" />
    </label>
    <label class="row">
      <span>{$t("Vault_Field_IMAPPassword")}</span>
      <span class="withAction">
        <input
          type="password"
          bind:value={imapPassword}
          on:input={() => touch("imapPassword")}
          placeholder={existing?.hasEmailAuth ? $t("Vault_Placeholder_Stored") : ""}
          autocomplete="new-password"
        />
        {#if existing?.hasEmailAuth}
          <button type="button" disabled={busy} on:click={() => reveal(VAULT_FIELDS.emailPassword)}>
            {$t("Vault_Action_Reveal")}
          </button>
        {/if}
      </span>
    </label>
  {:else if emailSource === "mailbox-api"}
    <p class="hint small">{$t("Vault_Field_MailboxHint")}</p>
    <label class="row">
      <span>{$t("Vault_Field_MailboxURL")}</span>
      <input type="text" bind:value={mailboxUrl} on:input={() => touch("mailboxUrl")} autocomplete="off" />
    </label>
    <label class="row">
      <span>{$t("Vault_Field_MailboxID")}</span>
      <input type="text" bind:value={mailboxId} on:input={() => touch("mailboxId")} autocomplete="off" />
    </label>
    <label class="row">
      <span>{$t("Vault_Field_MailboxToken")}</span>
      <input
        type="password"
        bind:value={mailboxToken}
        on:input={() => touch("mailboxToken")}
        placeholder={existing?.hasEmailAuth ? $t("Vault_Placeholder_Stored") : ""}
        autocomplete="new-password"
      />
    </label>
  {/if}

  {#if emailSource !== "none"}
    <button type="button" class="wide" disabled={busy} on:click={testBinding}>
      {$t("Vault_Action_TestEmail")}
    </button>
  {/if}

  <h3>{$t("Vault_Section_Provenance")}</h3>
  <label class="row">
    <span>{$t("Vault_Field_Source")}</span>
    <input type="text" bind:value={source} on:input={() => touch("source")} />
  </label>
  <label class="row">
    <span>{$t("Vault_Field_AcquiredAt")}</span>
    <input type="text" bind:value={acquiredAt} on:input={() => touch("acquiredAt")} placeholder="YYYY-MM-DD" />
  </label>
  <label class="row">
    <span>{$t("Vault_Field_SecretNote")}</span>
    <textarea rows="3" bind:value={secretNote} on:input={() => touch("secretNote")}></textarea>
  </label>

  <div class="actions">
    <button type="button" class="primary" disabled={busy} on:click={save}>{$t("Vault_Action_Save")}</button>
    {#if existing}
      <button type="button" class="danger" disabled={busy} on:click={removeEntry}>
        {$t("Vault_Action_Delete")}
      </button>
    {/if}
  </div>
</div>

<style lang="scss">
  .vaultEditor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-height: 60vh;
    overflow-y: auto;
    padding-right: 0.25rem;
  }

  h3 {
    margin: 0.75rem 0 0.25rem;
    font-size: 0.95rem;
    opacity: 0.9;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .row > span:first-child {
    font-size: 0.85rem;
    opacity: 0.85;
  }

  .withAction {
    display: flex;
    gap: 0.35rem;
    align-items: center;
  }

  .withAction input {
    flex: 1 1 auto;
    min-width: 0;
  }

  input,
  select,
  textarea {
    width: 100%;
    box-sizing: border-box;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .check input {
    width: auto;
  }

  .hint {
    margin: 0;
    font-size: 0.85rem;
    opacity: 0.8;
  }

  .small {
    font-size: 0.8rem;
  }

  .revealed {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    align-items: center;
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--border-bar-bg);
    border-radius: 4px;
  }

  .revealed code {
    font-family: monospace;
    word-break: break-all;
    flex: 1 1 12rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .wide {
    align-self: flex-start;
  }

  .danger {
    margin-left: auto;
  }
</style>

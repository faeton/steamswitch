<script lang="ts">
  /**
   * Handing an account to another person (VAULT.md §9).
   *
   * The whole screen exists to make one thing impossible to miss: **there is no revocation.**
   * With no server, nothing this app does reaches the recipient's copy of the bundle. The
   * owner's only real levers are changing the password and Steam's Sign Out Everywhere, both
   * of which happen outside SteamSwitch.
   *
   * Consequences that are load-bearing here, not stylistic:
   *
   *  - The word "lend" appears nowhere. It implies a recall that cannot be implemented, and a
   *    button that implies a lie is worse than no button.
   *  - Expiry and single-use are labelled as what they are: enforced by the recipient's copy
   *    of SteamSwitch, not by cryptography. They stop honest accidents. They are not a
   *    control, and this screen must never describe them as one.
   *  - Transfer takes a typed confirmation naming the account, because it can lock the owner
   *    out of their own account.
   */
  import { onDestroy } from "svelte";
  import { t } from "../../stores/i18n";
  import { pushToast } from "../../stores/toast";
  import { formatToastWithError } from "../../lib/formatWailsError";
  import { exportHandoff, handoffFolder, vaultBusy } from "../../stores/vault";

  export let steamId64: string;
  export let accountName = "";

  /** Matches vault.MinPassphraseLength. The Go side enforces it regardless. */
  const MIN_PASSPHRASE = 16;

  let mode: "grant" | "transfer" = "grant";
  let label = "";
  let passphrase = "";
  let passphraseAgain = "";
  let expiresInDays = 7;
  let singleUse = true;
  let confirm = "";
  let writtenPath = "";

  $: passphraseTooShort = passphrase.trim().length < MIN_PASSPHRASE;
  $: passphraseMismatch = passphraseAgain.length > 0 && passphrase !== passphraseAgain;
  $: confirmOk = mode !== "transfer" || confirm.trim().toLowerCase() === accountName.trim().toLowerCase();
  $: canExport =
    !$vaultBusy && !passphraseTooShort && !passphraseMismatch && passphrase === passphraseAgain && confirmOk;

  /** The passphrase is the only thing in front of full account access. It does not linger
   * in component state after the dialog closes. */
  function clearSecrets(): void {
    passphrase = "";
    passphraseAgain = "";
    confirm = "";
  }

  onDestroy(clearSecrets);

  async function doExport(): Promise<void> {
    try {
      const res = await exportHandoff({
        steamId64,
        mode,
        label: label.trim(),
        passphrase,
        expiresInDays: Number(expiresInDays) || 0,
        singleUse,
        confirm,
      });
      writtenPath = res.path;
      clearSecrets();
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Handoff_ExportFailed"), e), duration: 8000 });
    }
  }

  async function openFolder(): Promise<void> {
    try {
      const dir = await handoffFolder();
      pushToast({ type: "info", message: dir, duration: 8000 });
    } catch (e) {
      pushToast({ type: "error", message: formatToastWithError($t("Toast_Handoff_ExportFailed"), e), duration: 8000 });
    }
  }
</script>

<div class="handoff">
  <p class="warn">{$t("Handoff_NoRevoke")}</p>

  {#if writtenPath}
    <p class="small">{$t("Handoff_Written")}</p>
    <p class="path">{writtenPath}</p>
    <p class="small">{$t("Handoff_SendSeparately")}</p>
    <button type="button" on:click={openFolder}>{$t("Handoff_OpenFolder")}</button>
    <button type="button" on:click={() => (writtenPath = "")}>{$t("Handoff_ExportAnother")}</button>
  {:else}
    <fieldset>
      <legend>{$t("Handoff_Mode")}</legend>
      <label class="check">
        <input type="radio" bind:group={mode} value="grant" />
        <span>
          <strong>{$t("Handoff_Mode_Grant")}</strong>
          <span class="small">{$t("Handoff_Mode_GrantHint")}</span>
        </span>
      </label>
      <label class="check">
        <input type="radio" bind:group={mode} value="transfer" />
        <span>
          <strong>{$t("Handoff_Mode_Transfer")}</strong>
          <span class="small">{$t("Handoff_Mode_TransferHint")}</span>
        </span>
      </label>
    </fieldset>

    <label class="row">
      <span>{$t("Handoff_Label")}</span>
      <input type="text" bind:value={label} placeholder={$t("Handoff_LabelPlaceholder")} maxlength="60" />
    </label>

    <label class="row">
      <span>{$t("Handoff_Passphrase")}</span>
      <input type="password" bind:value={passphrase} autocomplete="new-password" spellcheck="false" />
    </label>
    <label class="row">
      <span>{$t("Handoff_PassphraseAgain")}</span>
      <input type="password" bind:value={passphraseAgain} autocomplete="new-password" spellcheck="false" />
    </label>
    {#if passphrase.length > 0 && passphraseTooShort}
      <p class="small err">{$t("Handoff_PassphraseTooShort", { min: String(MIN_PASSPHRASE) })}</p>
    {:else if passphraseMismatch}
      <p class="small err">{$t("Handoff_PassphraseMismatch")}</p>
    {/if}
    <p class="small">{$t("Handoff_PassphraseChannel")}</p>

    <label class="row">
      <span>{$t("Handoff_ExpiresInDays")}</span>
      <input type="number" min="0" step="1" bind:value={expiresInDays} />
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={singleUse} />
      <span>{$t("Handoff_SingleUse")}</span>
    </label>
    <p class="small">{$t("Handoff_AdvisoryOnly")}</p>

    {#if mode === "transfer"}
      <p class="warn">{$t("Handoff_TransferWarning")}</p>
      <label class="row">
        <span>{$t("Handoff_ConfirmPrompt", { account: accountName })}</span>
        <input type="text" bind:value={confirm} autocomplete="off" spellcheck="false" />
      </label>
    {/if}

    <button type="button" disabled={!canExport} on:click={doExport}>
      {mode === "transfer" ? $t("Handoff_ExportTransfer") : $t("Handoff_ExportGrant")}
    </button>
  {/if}
</div>

<style lang="scss">
  .handoff {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-width: 100%;
  }

  .small {
    margin: 0;
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .err {
    opacity: 1;
    color: var(--danger, #d66);
  }

  .warn {
    margin: 0;
    padding: 0.4rem 0.5rem;
    border-radius: 0.3rem;
    font-size: var(--fs-meta);
    background: rgba(200, 140, 0, 0.15);
  }

  .path {
    margin: 0;
    font-family: monospace;
    font-size: var(--fs-meta);
    overflow-wrap: anywhere;
  }

  fieldset {
    margin: 0.3rem 0 0;
    border: 1px solid rgba(128, 128, 128, 0.35);
    border-radius: 0.3rem;
    padding: 0.4rem 0.5rem;
  }

  legend {
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    margin-top: 0.3rem;
  }

  .row > span:first-child {
    font-size: var(--fs-meta);
    opacity: 0.85;
  }

  .check {
    display: flex;
    align-items: flex-start;
    gap: 0.4rem;
    margin-top: 0.3rem;
    font-size: var(--fs-secondary);
  }

  .check span {
    display: flex;
    flex-direction: column;
  }

  input[type="text"],
  input[type="password"],
  input[type="number"] {
    width: 100%;
    min-width: 0;
  }

  button {
    align-self: flex-start;
    margin-top: 0.4rem;
  }
</style>

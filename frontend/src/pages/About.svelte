<script lang="ts">
  /**
   * About, promoted from a Tools modal to a top-level destination (REDESIGN_BRIEF.md A6).
   *
   * It earns the space for one reason: it is where the app states what it does and does not
   * do with the network. The brief (A12) is explicit that this has to be said plainly
   * somewhere, so designers and users do not infer a product cloud that does not exist.
   *
   * Fork rule: the upstream TroubleChute attribution stays. No links to tcno.co, the wiki,
   * Discord or donations, and no update surface.
   */
  import { onMount } from "svelte";
  import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
  import { t } from "../stores/i18n";
  import { appBarTitle, previousPage } from "../stores/nav";
  import { offlineMode } from "../stores/offlineMode";
  import PageHeader from "../components/PageHeader.svelte";

  let currentVersion = "0.0.0";

  $: appBarTitle.set($t("Title_About"));

  onMount(() => {
    previousPage.set({ page: "home" });
    void PlatformService.GetAppVersion()
      .then((v) => {
        currentVersion = v || "0.0.0";
      })
      .catch(() => {
        currentVersion = "0.0.0";
      });
  });
</script>

<div class="about">
  <PageHeader title={$t("Nav_About")} />

  <div class="about__scroll">
    <section class="about__card">
      <h2 class="about__name">SteamSwitch</h2>
      <p class="about__version meta-mono">{$t("Modal_Info_Version")}: {currentVersion}</p>
      <p class="about__lead">{$t("About_WhatItIs")}</p>
    </section>

    <section class="about__card">
      <h3 class="about__heading">{$t("About_Network_Heading")}</h3>
      <p class="about__body">{$t("About_Network_None")}</p>
      <ul class="about__list">
        <li>{$t("About_Network_Valve")}</li>
        <li>{$t("About_Network_Mailbox")}</li>
      </ul>
      <p class="ss-help">
        {$offlineMode ? $t("About_Network_OfflineOn") : $t("About_Network_OfflineOff")}
      </p>
    </section>

    <section class="about__card">
      <h3 class="about__heading">{$t("About_Credits_Heading")}</h3>
      <p class="about__body">{$t("Modal_Info_ForkOf")}</p>
      <p class="ss-help">{$t("Modal_Info_Licence")}</p>
    </section>
  </div>
</div>

<style>
  .about {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .about__scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: 0 var(--space-7) var(--space-6);
    max-width: 720px;
  }

  .about__card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-5);
    border: 1px solid var(--hairline, var(--border-bar-bg));
    border-radius: var(--radius-lg);
    background: var(--surface-panel, var(--mainContentBackground));
  }

  .about__name {
    margin: 0;
    font-size: var(--fs-title);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .about__version {
    margin: 0;
    color: var(--fg-muted);
  }

  .about__heading {
    margin: 0;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    color: var(--fg-primary);
  }

  .about__lead,
  .about__body {
    margin: 0;
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-secondary);
  }

  .about__list {
    margin: 0;
    padding-left: 1.2em;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--fg-secondary);
  }
</style>

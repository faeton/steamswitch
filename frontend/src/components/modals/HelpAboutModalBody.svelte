<script lang="ts">
  import { onMount } from "svelte";
  import * as PlatformService from "../../../bindings/steamswitch/internal/platform/platformservice.js";
  import { t } from "../../stores/i18n";

  let currentVersion = "0.0.0";

  onMount(() => {
    void PlatformService.GetAppVersion()
      .then((v) => {
        currentVersion = v || "0.0.0";
      })
      .catch(() => {
        currentVersion = "0.0.0";
      });
  });
</script>

<div class="about-modal">
  <div class="rightContent">
    <h2>SteamSwitch</h2>
    <p>{$t("Modal_Info_ForkOf")}</p>
    <p class="licence">{$t("Modal_Info_Licence")}</p>
  </div>
</div>
<div class="versionIdentifier">
  <span>{$t("Modal_Info_Version")}: {currentVersion}</span>
</div>

<style lang="scss">
  .about-modal {
    display: flex;
    flex-direction: row;
    flex-wrap: wrap;
    gap: 1rem;
    align-items: flex-start;
  }

  .rightContent {
    flex: 1 1 200px;
    min-width: 0;
  }

  .rightContent h2 {
    margin: 0 0 0.5rem;
    font-size: 1.25rem;
  }

  .rightContent p {
    margin: 0 0 0.75rem;
  }

  .licence {
    font-size: 0.9rem;
    opacity: 0.85;
  }

  .versionIdentifier {
    margin-top: 0.75rem;
    padding-top: 0.5rem;
    border-top: 1px solid var(--border-bar-bg);
    font-size: 0.85rem;
    opacity: 0.85;
  }
</style>

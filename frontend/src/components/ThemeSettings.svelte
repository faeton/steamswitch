<script lang="ts">
  import SettingsCard from "./settings/SettingsCard.svelte";
  /** The Appearance section (REDESIGN.md §6): System/Light/Dark plus one accent. */
  import { t } from "../stores/i18n";
  import { appBgInfo } from "../stores/backgroundImage";
  import AppearanceModeControl from "./AppearanceModeControl.svelte";
  import ThemePickerControls from "./ThemePickerControls.svelte";
  import BackgroundSettings from "./BackgroundSettings.svelte";
</script>

<SettingsCard title={$t("Appearance_Section")}>
  <AppearanceModeControl />
  <ThemePickerControls />
  <!-- Only an image set by an earlier build gets here; this is how to remove it. -->
  {#if $appBgInfo.hasImage}
    <BackgroundSettings target="app" />
  {/if}
</SettingsCard>

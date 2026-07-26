<script lang="ts">
  /**
   * The Appearance section (REDESIGN.md §6).
   *
   * Primary block: System/Light/Dark plus one accent. The ~20 inherited theme packs and the
   * background-image controls are demoted into a collapsed "Classic themes" section, so a
   * first run never sees the theme zoo.
   */
  import { get } from "svelte/store";
  import { route } from "../stores/nav";
  import { t } from "../stores/i18n";
  import { pushToast } from "../stores/toast";
  import { formatToastWithError } from "../lib/formatWailsError";
  import * as PlatformService from "../../bindings/steamswitch/internal/platform/platformservice.js";
  import { appBgInfo, userOverriddenAppBg, setUserOverride } from "../stores/backgroundImage";
  import { clearUserTheme, currentThemeBgUrl, currentThemeId, isBaseThemeId } from "../lib/themes";
  import AppearanceModeControl from "./AppearanceModeControl.svelte";
  import ThemePickerControls from "./ThemePickerControls.svelte";
  import BackgroundSettings from "./BackgroundSettings.svelte";

  $: showResetToThemeBg = !!$currentThemeBgUrl && ($appBgInfo.hasImage || $userOverriddenAppBg);
  $: usingClassicTheme = !isBaseThemeId($currentThemeId);

  async function resetToThemeBg(): Promise<void> {
    try {
      if ($appBgInfo.hasImage) {
        await PlatformService.ClearAppBackground();
      }
      await setUserOverride(false);
      const info = await PlatformService.GetAppBackground();
      appBgInfo.set(info);
    } catch (e) {
      pushToast({
        type: "error",
        message: formatToastWithError(get(t)("Toast_SaveFailed"), e),
        duration: 8000,
      });
    }
  }
</script>

<h2 class="SettingsHeader">{$t("Appearance_Section")}</h2>

<AppearanceModeControl />
<ThemePickerControls section="accent" />

<details class="classic" open={usingClassicTheme}>
  <summary>{$t("Appearance_ClassicThemes")}</summary>
  <p class="hint">{$t("Appearance_ClassicThemes_Hint")}</p>

  <ThemePickerControls section="theme">
    <div slot="after-controls" class="classic__actions">
      {#if usingClassicTheme}
        <button type="button" class="btnicontext" on:click={() => void clearUserTheme()}>
          {$t("Appearance_ClassicThemes_None")}
        </button>
      {/if}
      <button type="button" class="btnicontext" on:click={() => route.set({ page: "preview-css" })}>
        {$t("PreviewCss")}
      </button>
    </div>
  </ThemePickerControls>

  {#if $appBgInfo.hasImage || showResetToThemeBg}
    <div class="bg-settings-row">
      {#if showResetToThemeBg}
        <button type="button" class="btnicontext" on:click={() => void resetToThemeBg()}>
          {$t("Settings_ResetToThemeBackground")}
        </button>
      {/if}
      {#if $appBgInfo.hasImage}
        <BackgroundSettings target="app" />
      {/if}
    </div>
  {/if}
</details>

<style lang="scss">
  .classic {
    margin-top: 0.75rem;
    border-top: 1px solid var(--hairline, var(--border-bar-bg));
    padding-top: 0.5rem;

    summary {
      cursor: pointer;
      user-select: none;
    }
  }
  .classic__actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .hint {
    color: var(--role-text-muted, var(--text-subtle-gray));
    font-size: 11px;
    margin: 4px 0 8px;
  }
  button {
    position: relative;
    height: 38px;
  }
</style>

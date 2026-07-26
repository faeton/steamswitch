<script lang="ts">
  /**
   * The whole of Settings, in one column (REDESIGN.md §4).
   *
   * Order is by how often it is touched, not by which Go package owns it: appearance first
   * because it is the one thing people change on day one, then the Steam options a switch
   * actually depends on, then the game modules, then the machine-level plumbing, and language
   * last because it is set once.
   */
  import "../styles/Settings.scss";
  import ThemeSettings from "./ThemeSettings.svelte";
  import SteamSettings from "./SteamSettings.svelte";
  import GameModuleSettings from "./GameModuleSettings.svelte";
  import VaultSettings from "./VaultSettings.svelte";
  import SystemSettings from "./SystemSettings.svelte";
  import LanguageSettings from "./LanguageSettings.svelte";
</script>

<ThemeSettings />
<SteamSettings />
<GameModuleSettings />
<VaultSettings />
<SystemSettings />
<LanguageSettings />

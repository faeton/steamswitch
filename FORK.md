# SteamSwitch — fork notes

A Steam-only fork of [TcNo Account Switcher](https://github.com/TCNOco/TcNo-Acc-Switcher)
with Dota 2 configuration management added.

Copyright and licence obligations are in `NOTICE.md` — read that before distributing.

Upstream remains the multi-platform project. This fork exists to do three things well:
switch Steam accounts, clean/refresh Steam, and move Dota 2 configs around.

## What changed from upstream

### Identity and isolation

The fork must be able to run side by side with the original app without either one
clobbering the other's state. Everything below was renamed for that reason:

| Thing | Upstream | This fork |
|---|---|---|
| Go module | `TcNo-Acc-Switcher` | `steamswitch` |
| Product / binary | `TcNo-Acc-Switcher.exe` | `SteamSwitch.exe` |
| User data folder | `%AppData%\TcNo Account Switcher` | `%AppData%\SteamSwitch` |
| Settings file | `TcNo-Acc-Switcher.settings.json` | `SteamSwitch.settings.json` |
| URL scheme | `tcno://` | `steamswitch://` |
| Single-instance ID | `co.tcno.acc-switcher` | `io.faeton.steamswitch` |
| Singleton mutex | `TcNo-Acc-Switcher-Singleton` | `SteamSwitch-Singleton` |
| Named pipe | `\\.\pipe\TcNo-Acc-Switcher` | `\\.\pipe\SteamSwitch` |
| Autostart registry value | `TcNoAccSwitcher` | `SteamSwitch` |
| Product identifier | `co.tcno.acc-switcher` | `io.faeton.steamswitch` |
| Version | tracks upstream | reset to `1.0.0` |

### Steam-only

`Platforms.json` was trimmed from 24 platforms to just the `Steam` entry. That single
change is what makes the app Steam-only: the frontend already routes straight past the
platform grid when exactly one platform is enabled
(`applySinglePlatformStartupRoute`, `frontend/src/stores/routeCodec.ts`).

Consequently removed:

- `pages/ManagePlatforms.svelte` and the `manage-platforms` route — nothing to manage.
- `pages/Platform.svelte` — the generic non-Steam account page.
- The Action Bar's "Manage Platforms" button, replaced by a "Dota configs" button that
  is shown on the Steam page.

**`internal/basic` was deliberately kept.** It is not just the generic switching engine —
Steam depends on it for account tags, notes, renaming, ordering and game stats
(`internal/steam/accounts_list.go`, `internal/steam/service.go`). Deleting it would mean
reimplementing all of that. The generic *switching* paths in it are now unreachable from
the UI but still compile.

### Auto-update is off

`internal/app/gui.go` no longer initialises the Wails updater. Upstream's release feed
publishes the multi-platform app; leaving the updater on would let it overwrite this
build with upstream's. `runLaunchPlatformsJSONCheck` is likewise disabled — it would
re-download the 24-platform `Platforms.json` and undo the strip. The now-dead
"pre-release updates" checkbox was removed from settings.

The `internal/updatecheck` package and `runLaunchPlatformsJSONCheckUpstream` are still
present, so re-enabling either is a small, obvious change if you point the fork at your
own release feed.

### De-branded and offline

The fork is not a TcNo product and does not talk to TcNo's infrastructure:

- **All outbound reporting is removed.** `internal/api` returns empty URLs for anonymous
  statistics, crash reports, stability ratings, feedback and version checks, and every
  caller treats an empty URL as "disabled" and returns early. Statistics and crash dumps
  are still written locally; they are simply never uploaded.
- The Steam **app-name list** now comes from Valve's own `ISteamApps/GetAppList/v2`
  instead of TcNo's mirror. `parseAppNameMapJSON` accepts both that response shape and the
  flat `{"appid":"name"}` shape used by the on-disk cache.
- Upstream **links removed**: wiki, Discord, Patreon, Ko-fi, Crowdin, tcno.co, the public
  stats page and the releases page, plus the settings toggles that fed them (share stats,
  auto-submit crash reports) and the now-dead "check for updates" / "suggest a feature"
  buttons. Local statistics collection and the stats viewer stay — they never leave the PC.
- **Artwork replaced** with neutral placeholder icons (`build/appicon.png`,
  `build/trayicon.png`, regenerated `.ico`/`.icns`) and a plain `SteamSwitch` wordmark
  (`frontend/public/img/SteamSwitch_Logo.svg`). TroubleChute's logo files are deleted.
  These are placeholders — drop in real artwork and re-run
  `wails3 task common:generate:icons`.
- Discord Rich Presence is **kept**: it talks to the local Discord client, not TcNo.

## New features

### Dota 2 config management (`internal/steam/dota.go`, `pages/DotaConfigs.svelte`)

Reachable from the "Dota configs" button on the Steam page, or `#/dota-configs`.

- **Account → account copy.** `CopyDotaConfigBetween(source, dest, parts)` copies
  `userdata/<id32>/570/…` directly between any two accounts. This is the main thing
  upstream could not do: upstream's `CopySteamGameSettingsFrom` always writes into
  whichever account is *currently signed in*, so copying A → B meant switching to B first.
- **Snapshot library.** Save any account's Dota config under a name — "Ceb's config",
  "my 2026 setup" — then apply it to any account later. Snapshots live in
  `<DataRoot>/Backups/Dota/<uuid>/` with a `snapshot.json` holding the label, note,
  source account, parts and timestamp.
- **Automatic revert points.** Every copy or apply first snapshots the destination
  account, tagged `Auto: true` and shown as a "revert point" in the UI. Renaming one
  promotes it to a normal curated config, so a revert point can be re-tagged as
  somebody else's config after you import it.

Three config parts can be selected independently:

| Part | Path | Notes |
|---|---|---|
| `local` | `userdata/<id32>/570/local` | Video settings and other local options. Not cloud-synced, so copies stick. |
| `remote` | `userdata/<id32>/570/remote` | Hero grids and cloud-synced prefs. **Steam Cloud will overwrite this on next launch if Steam is running** — the backend refuses the copy and the UI warns. |
| `globalcfg` | `<library>/steamapps/common/dota 2 beta/game/dota/cfg` | `autoexec.cfg` and friends. Resolved across every library in `libraryfolders.vdf`, since Dota is often on a second drive. Machine-wide and shared by every account, so it is captured in snapshots but never copied account-to-account (it would be a no-op). Applying a snapshot *can* write it, behind an explicit warning. |

### One-click refresh presets (`internal/steam/refresh_preset.go`)

Two bundles on the Advanced Cleaning page that run several clearing actions in order
instead of clicking each one:

- **Refresh caches** — close Steam, then clear htmlcache, appcache, httpcache, depotcache.
- **Deep refresh** — the above plus logs, UI logs and crash dumps.

Both close Steam first and are login-safe: a test asserts neither preset references an
action in the `login` category, so they cannot sign accounts out. A failing step is
logged and the run continues rather than aborting.

### Refresh cadence settings (`internal/steam/refresh_schedule.go`)

In Steam platform settings:

- **Refresh accounts shortly after the app starts** (default on) — runs 5s after launch.
- **Also refresh every N minutes** (default off) — clamped up to a 15-minute floor, since
  each refresh scrapes community pages for every account.
- **Re-download cached avatars after N days** — the pre-existing `Steam_ImageExpiryTime`,
  now exposed in the UI.

Changing either setting re-applies the schedule immediately; no restart needed. The
scheduler skips runs while the app is locked.

### A switch no longer signs other accounts out

Upstream's swap sets `RememberPassword` to `0` on every account in `loginusers.vdf` that it is
*not* switching to. This fork sets it only on the target.

The evidence is a real Steam install that SteamSwitch has never touched: five remembered
accounts, `RememberPassword` `1` on all five. The client does not clear it on a switch. That
same install has only four `ConnectCache` entries in `local.vdf`, so one account would land on
a password prompt — with its flag still at `1`, which rules the flag out as the cause but also
shows how close the failure mode is.

Clearing it therefore bought nothing and pushed the file away from the state Steam maintains,
in the direction of the worst outcome here: a switch that looks like a logout. Turning off
**Settings → Steam → Stay signed in after switching** still works, and is still scoped to the
account being switched to.

### macOS support (`internal/steam/os_backend*.go`)

Upstream is Windows-only, and reasonably so: it drives 24 platforms, most of which do not
exist on macOS. A Steam-only fork does not have that problem — Steam keeps the same layout
under `~/Library/Application Support/Steam` that it does under `C:\Program Files (x86)\Steam`,
including `config/loginusers.vdf` and `userdata/<id32>/570/{local,remote}`.

What differs is small enough to sit behind one interface, `osBackend`:

- the Windows registry values (`AutoLoginUser`, `RememberPassword`) live in `registry.vdf`
  under `Registry/HKCU/Software/Valve/Steam`, edited losslessly by `registryvdf.go`;
- processes are `steam_osx` / `steamwebhelper` / `dota2`, found with `pgrep -x` and stopped
  with SIGTERM followed by SIGKILL after a 20-second grace period;
- the application bundle is separate from the data directory, so `steamRoot` throughout this
  package means the *data* root and only `Launch` looks for `/Applications/Steam.app`.

The seam is also the fail-closed gate. `unsupportedBackend` answers every method with
`Toast_Steam_SwitchingUnsupportedOnThisOS`, and `ProcessInspectionSupported()` reporting false
is what makes the write guards refuse rather than proceed — because "no Steam process found"
and "I cannot see processes" are the same answer from a stub, and one of them means it is safe
to write.

Linux is not enabled. It is closer than it looks — same `registry.vdf`, process names `steam`
and `steamwebhelper` — but Flatpak and Snap installs relocate the data directory into a sandbox
and hide the process from name lookup outside it. A backend that handled only the native
install would report Steam as closed, with confidence, for a large share of Linux users; a
cloud-synced Dota write made in that state is reverted silently by Steam Cloud with no error
anywhere.

## Development

Same as upstream, but note:

```bash
go install github.com/wailsapp/wails3/cmd/wails3@v3.0.0-alpha2.117  # if not installed
wails3 task common:generate:bindings   # REQUIRED before frontend check/test/build
cd frontend && pnpm install && pnpm run check && pnpm run test
go test ./...
```

`frontend/bindings/` is generated and gitignored. Frontend tests and `svelte-check` fail
to resolve imports until bindings are generated at least once.

### Test baseline on non-Windows

`go test ./...` shows **15 failures on macOS/Linux**, all inherited from upstream and all
caused by Windows path/registry/file-locking assumptions:

- `internal/basic` — 13 (`TestFlow_*`, `TestResolveDescriptorVariables_WithTokens`)
- `internal/platform` — 1 (`TestResolveSafeDeletePatternRejectsGlobAtPlaceholderBase`)
- `internal/steam` — 1 (`TestWriteFileAtomic_LockedFile`)

These are all in the `basic` engine and in file-locking helpers, none of which the Steam
engine uses. The Steam package itself is green on macOS.

## Review notes

An independent review (Grok) of the Dota feature found several real defects, since fixed:
the machine-wide cfg path ignoring secondary Steam libraries; an empty `parts` argument
defaulting to include that machine-wide folder; no lock around concurrent writes;
`window.prompt` being unreliable inside the embedded WebView; unbounded growth of auto
revert points; and over-confident "copied" messaging for cloud-synced files. Remaining
known gaps are listed in `FORK.md` history and the code comments — most notably that a
Steam Cloud conflict on next launch is possible and is surfaced to the user rather than
prevented.

Compare against that baseline rather than expecting green. Everything added by this fork
passes on macOS: 13 new tests in `internal/steam/dota_test.go` and
`internal/steam/refresh_preset_test.go`.

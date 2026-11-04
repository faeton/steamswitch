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
  `build/trayicon.png`, regenerated `.ico`/`.icns`, `other/**/icon.ico` and the installer banners)
  and a plain `SteamSwitch` wordmark
  (`frontend/public/img/SteamSwitch_Logo.svg`). TroubleChute's logo files are deleted.
  These are placeholders — drop in real artwork and re-run
  `wails3 task common:generate:icons`.
- Discord Rich Presence is **kept**: it talks to the local Discord client, not TcNo.

## New features

### Game config copy (`internal/steam/gameconfig.go`, `pages/GameConfigs.svelte`)

Reachable from Tools, or `#/tools/game-configs`. The old `#/dota-configs` link still works.

- **Account → account copy for any game, either way.** `CopyGameConfigBetween(appID, source, dest,
  parts, allowCloudRisk)` copies `userdata/<id32>/<appid>/…` directly between any two accounts. This
  is the main thing upstream could not do: its `CopySteamGameSettingsFrom` wrote into whichever
  account was *currently signed in*. That method now calls this one. The `*Dota*` service methods
  are this path with 570 filled in.
- **The same rules for every game.** The destination is snapshotted first, including what it does
  *not* have, so an undo also removes what the copy created. Each part is replaced, never merged.
  Cloud-synced parts are refused while Steam runs unless the risk is accepted. Nothing is written
  while the game runs, with no override: known process names, Steam's own Running flag while the
  client is up, and any process started from the install folder. A write that fails part
  way puts the destination straight back.
- **What each game's copy holds is read from disk**, and the page names what it leaves behind.
  `local/` counts as cloud-synced for every game except Dota, where it is known not to be: public
  guides describe CS2's `local/cfg` as synced. CS2's layout comes from those guides and has not been
  checked against a real install. For other games `remote/` can hold save games, and the page says so.
  Steam's own folders (7, 760) are refused.
- **Not the Session Kit.** "Moving your game settings" journals a kit onto a shared account during
  a switch. Only Dota has a writing module, and `cs2Enabled` stays off until TESTING.md §C has been
  run. The two never overlap: this tool refuses while a kit is live.
- **Snapshot library, per game.** Save any account's config under a name and apply it to any account
  later. Dota's stay in `<DataRoot>/Backups/Dota/<uuid>/`; others are in
  `<DataRoot>/Backups/Games/<appid>/<uuid>/`. Each has a `snapshot.json` with the label, note, source
  account, parts and timestamp.
- **Automatic revert points.** Every copy or apply first snapshots the destination account, tagged
  `Auto: true` and shown as a "revert point" in the UI. Renaming one promotes it to a normal curated
  config. The newest 10 are kept per game.

For Dota, three config parts can be selected independently:

| Part | Path | Notes |
|---|---|---|
| `local` | `userdata/<id32>/570/local` | Video settings and other local options. Not cloud-synced, so copies stick. |
| `remote` | `userdata/<id32>/570/remote` | Keybinds, hero grids, item builds and other cloud-synced prefs. **Steam Cloud may overwrite this on next launch if Steam is running.** The backend refuses by default; `allowCloudRisk` is the user having been shown that and said yes, and the UI asks before setting it. Closing Steam first is still the reliable route, and a `cloudFollowUp` toast explains how to answer the conflict prompt when there is one. |
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

### The leave rule is enforced in Go, not in the UI (`internal/steam/sessionkit_guard.go`)

The Session Kit's central promise is that borrowing someone's account cannot silently
overwrite their settings. That rests on one rule: switching *away* from an account carrying a
kit has to ask whether to put their setup back.

For a while that rule existed only in `Accounts.svelte`. `SessionKitService.SwitchTo` is the
kit-aware entry point, and the account tiles call it — but the tray menu, desktop shortcuts,
`steamswitch://` URLs and `--run-appid=` all reach `steam.SwapToAccount` directly and never
touch the engine. A tray switch would therefore leave the other person's account under this
machine's kit with an open journal, which is the state the crash-recovery modal exists to
clean up, reached without a crash.

`requireNoActiveKit` now runs at the head of `SwapToAccount`, before the already-signed-in
short circuit. The engine drives the individual `Lifecycle` steps rather than calling
`SwapToAccount`, so the guard constrains only the bypass paths.

It refuses rather than guessing. "Restore theirs" and "keep mine" is a decision about somebody
else's files, and there is no defensible default for a tray click — so the error names the
window as where to finish. It fails *open* when it cannot tell (no engine, unreadable journal),
because a guard that blocks every switch on a transient read failure would be worse than the
hole it closes.

### Double-click to switch

A switch closes Steam, so one stray click must not start one (`AccountRow.svelte`, `tileKeys.ts`).
A pointer's click does nothing, a double-click switches, and Shift-click still opens the detail
panel. Number keys, Enter in search and Enter on a focused row are deliberate and switch at once, as
does a click with no pointer behind it (screen reader, controller). Space on a focused row shows
details instead, because a click leaves the row focused and a stray Space should not close Steam.
A second switch is refused by `beginSwitch`, so a double-click cannot fire two.

### "Signed in" means Steam said so (`internal/steam/signin_watch.go`)

Writing the login and starting Steam does not sign anybody in. A rejected token ("27 (License
expired)", measured) or an account with no stored login ends at a password prompt, and the app used
to report success for both. Every launch now starts a watch whose verdict drives the dock and the
top bar: `confirmed`, `needsSignIn` (with Steam's reason), `unconfirmed` or `steamExited`. If the
person then types their password, it becomes `confirmed`, however long that takes: the watch keeps
going while Steam runs. `steamExited` needs 15 s of no client, because Steam restarts itself to update.

Evidence is `logs/connection_log.txt`, the client's `ActiveUser` changing to this account (a killed
client leaves the old value behind, so its value alone proves nothing) and the `ConnectCache` entry
disappearing. The `loginusers.vdf` `Timestamp` is deliberately not used: nobody has checked whether
Steam rewrites it on a failed attempt. Only the rejection lines have been measured on real Steam.
Anything that does not match gives `unconfirmed`, never a false "signed in". Known gap: after restarting the app there is no watch, so the bar goes back to
reading the login files.

`stored_login.go` reads which accounts have a `ConnectCache` key (CRC32(name) + "1") for the "Needs
sign-in" badge. Only the keys are used, never the token values. Unreadable, or nothing matching at
all, means no badge rather than a badge on everyone.

### The rating prompt is gone

Upstream's "Did the switch work?" prompt and its feedback dialog posted to an endpoint this fork
removed, so the answer went nowhere and nothing left the machine. It still thanked people for it.
Both were removed. The endpoints in `internal/api` stay empty.

### Blank window diagnostics

The page reports how far it got and any script errors to the normal log, over the app's local asset
scheme (`internal/app/frontend_report.go`), and the log says so if it hears nothing for 20 s.
`--framed` uses the OS frame, `--diag-page` opens a plain test page, and `--webview-arg=<flag>` passes
a WebView2 flag.

### macOS support (`internal/steam/os_backend*.go`)

Upstream is Windows-only, and reasonably so: it drives 24 platforms, most of which do not
exist on macOS. A Steam-only fork does not have that problem — Steam keeps the same layout
under `~/Library/Application Support/Steam` that it does under `C:\Program Files (x86)\Steam`,
including `config/loginusers.vdf` and `userdata/<id32>/570/{local,remote}`.

What differs is small enough to sit behind one interface, `osBackend`:

- the Windows registry values (`AutoLoginUser`, `RememberPassword`) live in `registry.vdf`
  under `Registry/HKCU/Software/Valve/Steam`, edited losslessly by `registryvdf.go`;
- processes are `steam_osx` / `Steam Helper` / `dota2`, found with `pgrep -x`. Steam is closed by
  asking it (`steam://exit`), because a signed-in Mac Steam ignores SIGTERM, then with SIGTERM after
  10 s, or at once if the request could not be made, and SIGKILL 20 s from the start either way.
  `close_sequence.go` holds the order; Windows has its own close path;
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
caused by Windows path/registry/file-locking assumptions. Linux has a 16th,
`TestWriteLoginUsersAndRegistry_FieldSwapping`, because switching is not enabled there:

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

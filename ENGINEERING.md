# SteamSwitch engineering notes

How SteamSwitch works and the rules it keeps. It does three things: switch Steam accounts, clean
and refresh Steam, and move game configs between accounts.

Copyright and licence obligations are in `NOTICE.md`. Read that before distributing.

## Project rules

### Identity

| Thing | Value |
|---|---|
| Go module | `steamswitch` |
| Product and binary | `SteamSwitch` |
| User data folder | `%AppData%\SteamSwitch`, `~/Library/Application Support/SteamSwitch` |
| Settings file | `SteamSwitch.settings.json` |
| URL scheme | `steamswitch://` |
| Single-instance ID and product identifier | `io.faeton.steamswitch` |
| Windows singleton, pipe and autostart value | `SteamSwitch-Singleton`, `\\.\pipe\SteamSwitch`, `SteamSwitch` |

The encryption labels in `internal/security` start with `tcno-`. They are bound into every sealed
blob, so changing them makes existing data undecryptable. They stay.

### Steam only

`Platforms.json` holds only the `Steam` entry, and the frontend routes straight past the platform
grid when one platform is enabled (`applySinglePlatformStartupRoute`, `stores/routeCodec.ts`).

The generic multi-platform engine and the game-stats scraper are gone. `internal/basic` is now only
the account metadata Steam uses: tags, notes, names and images (`internal/steam/accounts_list.go`,
`internal/steam/service.go`). Steam is the only platform: a command or link naming anything else is
an error.

The `tcno-` labels aside, the one piece that still mentions other platforms is the migration walker
in `internal/security/vault.go`, which skips Steam.

### No outbound reporting, no auto-update

- There is no code that uploads statistics, crash reports, ratings or feedback, or checks for
  updates. Statistics and crash dumps are written locally and stay there.
- Game names come from Steam's own `appmanifest_*.acf` files, without the network. Valve removed
  the public `ISteamApps/GetAppList/v2`, and its replacement needs a Web API key.
- There is no updater, and nothing downloads a replacement `Platforms.json`.

### Appearance

One built-in palette (`styles/appearance.scss`) with System, Light and Dark, plus one accent colour.

## How it behaves

### Account names

The row, the tray and every dialog show the name from **Rename in SteamSwitch** when there is one
(`AccountLabels` in the Steam settings), then Steam's profile name. It is local: Steam is not changed,
and an empty name goes back to Steam's.

### A switch only touches the target account

`RememberPassword` is set only on the account being switched to. A real Steam install that
SteamSwitch had never touched had five remembered accounts, all at `1`, so the client does not clear
it on a switch, and clearing it would push the file towards the worst outcome here: a switch that
looks like a logout. Turning off **Settings > Steam > Stay signed in after switching** is scoped to
that account too.

### Switching from the tray, links and the CLI

They have no window to ask in, so they refuse rather than close a running game. The account rows go
through the Session Kit engine and ask first. With Steam closed, asking for the account already
selected starts Steam.

### The Session Kit leave rule is enforced in Go (`internal/steam/sessionkit_guard.go`)

The Session Kit's central promise is that borrowing someone's account cannot silently
overwrite their settings. That rests on one rule: switching *away* from an account carrying a
kit has to ask whether to put their setup back.

`SessionKitService.SwitchTo` is the
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

### Game configs (`internal/steam/gameconfig.go`)

Copying is **Copy game config** on the Switcher (`GameConfigCopyForm.svelte`). Saved configs are Tools > Game configs (`pages/GameConfigs.svelte`, `#/tools/game-configs`): save an account's config under a name, and press **Apply to…** on a row to pick the account.

- **Account → account copy for any game, either way.** `CopyGameConfigBetween(appID, source, dest,
  parts, allowCloudRisk)` copies `userdata/<id32>/<appid>/…` directly between any two accounts. `CopySteamGameSettingsFrom` and the
  `*Dota*` service methods are this path with the destination or 570 filled in.
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

For Dota, a config has three parts:

| Part | Path | Notes |
|---|---|---|
| `local` | `userdata/<id32>/570/local` | Video settings and other local options. Not cloud-synced, so copies stick. |
| `remote` | `userdata/<id32>/570/remote` | Keybinds, hero grids, item builds and other cloud-synced prefs. **Steam Cloud may overwrite this on next launch if Steam is running.** The backend refuses by default; `allowCloudRisk` is the user having been shown that and said yes, and the UI asks before setting it. Closing Steam first is still the reliable route, and a `cloudFollowUp` toast explains how to answer the conflict prompt when there is one. |
| `globalcfg` | `<library>/steamapps/common/dota 2 beta/game/dota/cfg` | `autoexec.cfg` and friends. Resolved across every library in `libraryfolders.vdf`, since Dota is often on a second drive. Machine-wide and shared by every account, so it is captured in snapshots but never copied account-to-account (it would be a no-op). Applying a named config *can* write it, behind an explicit warning; a revert point leaves it alone, because copies never change it. |

### One-click refresh presets (`internal/steam/refresh_preset.go`)

Two bundles on the Advanced Cleaning page that run several clearing actions in order
instead of clicking each one:

- **Refresh caches** — close Steam, then clear htmlcache, appcache, httpcache, depotcache.
- **Deep refresh** — the above plus logs, UI logs and crash dumps.

Both close Steam first and are login-safe: a test asserts neither preset references an
action in the `login` category, so they cannot sign accounts out. A failing step is
logged and the run continues rather than aborting.

### Refresh cadence settings (`internal/steam/refresh_schedule.go`)

In Settings > Steam:

- **Refresh accounts shortly after the app starts** (default on) — runs 5s after launch.
- **Also refresh every N minutes** (default off) — clamped up to a 15-minute floor, since
  each refresh scrapes community pages for every account.
- **Re-download cached avatars after N days** — the pre-existing `Steam_ImageExpiryTime`,
  now exposed in the UI.

Changing either setting re-applies the schedule immediately; no restart needed. The
scheduler skips runs while the app is locked.

### Blank window diagnostics

The page reports how far it got and any script errors to the normal log, over the app's local asset
scheme (`internal/app/frontend_report.go`), and the log says so if it hears nothing for 20 s.
`--framed` uses the OS frame, `--diag-page` opens a plain test page, and `--webview-arg=<flag>` passes
a WebView2 flag.

### The OS seam (`internal/steam/os_backend*.go`)

Steam keeps the same layout
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

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117  # if not installed
wails3 task common:generate:bindings   # required before frontend check, test or build
cd frontend && pnpm install && pnpm run check && pnpm run test
go test ./...
```

`frontend/bindings/` is generated and gitignored. Frontend tests and `svelte-check` cannot resolve
imports until bindings have been generated once.

### Test baseline

`go test ./...` shows **2 failures on macOS and Linux**, caused by Windows path and file-locking
assumptions. Linux has a 3rd, `TestWriteLoginUsersAndRegistry_FieldSwapping`, because switching is
not enabled there:

- `internal/platform`: `TestResolveSafeDeletePatternRejectsGlobAtPlaceholderBase`
- `internal/steam`: `TestWriteFileAtomic_LockedFile`

Both pass on Windows. Under Wine, run them in a throwaway bottle rather than one holding a
real Steam, and expect `TestForwardArgsRoundTrip` to time out until that is settled on real Windows.
The Steam tests write the live `HKCU\Software\Valve\Steam` values there and put them back afterwards.

Compare against this baseline rather than expecting green.

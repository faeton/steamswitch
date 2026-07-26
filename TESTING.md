# Manual test plan

This is the by-hand pass to run on a real machine before a release. It is not CI — it covers
the things automated tests structurally cannot: the Windows registry (or macOS `registry.vdf`),
real Steam process behaviour, Steam Cloud, and what the UI actually looks like at 420×520.

**Run it on Windows and on macOS.** Both are supported targets and the OS-specific half of the
engine is a separate implementation on each (`internal/steam/os_backend_windows.go` and
`os_backend_darwin.go`), so a pass on one says nothing about the other. Where a step names a
Windows path, the macOS equivalent is:

| Windows | macOS |
|---|---|
| `<Steam>\` (contains `steam.exe`) | `~/Library/Application Support/Steam` |
| `HKCU\Software\Valve\Steam` | `<Steam>/registry.vdf`, under `Registry/HKCU/Software/Valve/Steam` |
| `steam.exe`, `steamwebhelper.exe`, `dota2.exe` | `steam_osx`, `steamwebhelper`, `dota2` |
| `%AppData%\SteamSwitch\` | `~/Library/Application Support/SteamSwitch/` |

**On Linux the app builds and renders but refuses to switch.** There is no backend for it, so
every write path returns `Toast_Steam_SwitchingUnsupportedOnThisOS`. Sections B–F cannot be
exercised; the one thing worth checking there is that the refusal is what happens, rather than
a silent no-op.

## Before you start

You need:

- Two Steam accounts you can log into, both having launched Dota 2 at least once (so
  `userdata/<id32>/570/local` and `.../remote` exist). A third is useful for §C4.
- Recognisably different Dota settings on each — change the crosshair or a keybind on one and
  a hero grid name on the other, so "whose config is this?" is answerable at a glance.
- A file manager open at `%AppData%\SteamSwitch\` and at `<Steam>\userdata\`.

Take a copy of `<Steam>\userdata\` before starting. If a test goes wrong you want a way back
that does not depend on the thing you are testing.

> Where a step says **must**, a failure is a release blocker. Where it says *should*, note it
> and move on.

---

## A. First run and basics

| # | Steps | Expected |
|---|---|---|
| A1 | Launch with no `%AppData%\SteamSwitch\` | Window opens ~420×520. Accounts already in Steam are listed. No theme picker, no platform grid. |
| A2 | Delete/rename the data folder, relaunch with **no** Steam accounts saved | Empty state shows an **Add New** button, not just "no accounts". |
| A3 | Click **Add New** | Steam opens at its own login screen. No SteamSwitch crash, no half-written state. |
| A4 | Close Steam, look at the footer | **Launch Steam** appears only while Steam is closed, and disappears once it is running. |
| A5 | Settings → Appearance, switch System / Light / Dark | Applies instantly. **System** follows the OS setting — flip Windows to dark and confirm the app follows without a restart. |
| A6 | Appearance → expand **Classic themes**, pick one, then set it back to None | The pack applies, and returning to None restores the built-in light/dark palette rather than leaving a half-themed window. |
| A7 | Resize the window to its minimum | Nothing clips or overlaps; the status strip text truncates rather than pushing buttons off-screen. |

## B. Plain switching (no kit)

Do this with **no** account marked Shared.

| # | Steps | Expected |
|---|---|---|
| B1 | Click a non-current account tile | Steam closes, the strip narrates each step, Steam relaunches signed in as that account. No password prompt, no 2FA prompt. |
| B2 | Watch the strip during B1 | It narrates. It **must not** show a success toast at the end — the strip is the channel now. |
| B3 | Press `1`, `2`, `3`, `4` | Switches to the first four tiles in listed order. |
| B4 | Click a tile twice, fast | Only **one** switch runs. The second click is ignored, not queued. |
| B5 | Right-click → Advanced → **Log in as ▸ Invisible** | Switches and Steam comes up Invisible. Critically, this **must** go through the same gate as B4 — try clicking a tile immediately after and confirm it is refused. |
| B6 | During a switch, try to click another tile | Tiles are disabled while switching. |
| B7 | Right-click → **Set as Home** on your main account | A Home badge appears; the tile sorts first. |
| B8 | Try to mark the Home account as Shared | Refused with an explanation. Home and Shared are mutually exclusive. |
| B9 | Right-click → Copy IDs → each entry | Each copies the right value; paste and check. |
| B10 | Right-click → Advanced → **Note…**, save some text, reopen | The note persists. |

### B-remember. Stay signed in after switching

The public-computer option. Default is **on**; these check that turning it off actually
leaves nothing behind.

| # | Steps | Expected |
|---|---|---|
| B11 | Settings → Steam, hover **Stay signed in after switching** | Checked by default. Tooltip explains the shared-computer case and admits it cannot remove logins Steam already saved. |
| B12 | Leave it **on**, switch to another account | Steam comes up already signed in, as in B1. |
| B13 | Turn it **off**, switch to another account | Steam comes up at the **password prompt** for that account rather than signed in. The switch itself still worked — the right account is preselected. |
| B14 | With it off, open `<Steam>/config/loginusers.vdf` | The account just switched to reads `"RememberPassword" "0"`. **Every other account keeps whatever it had.** Opting out of staying signed in is about the account you are switching to, not a machine-wide sign-out of everybody else's saved sessions. |
| B14a | Record every account's `RememberPassword` before a switch, then switch with the setting **on** | Only the target's changed. A switch that flips other accounts to `0` is the "switch looks like a logout" bug — the Steam client itself leaves them alone. |
| B15 | With it off, check `HKCU\Software\Valve\Steam` (Windows) or `registry.vdf` (macOS) | `RememberPassword` is `0`, matching the file. The two must never disagree. |
| B16 | Turn it back **on**, switch again | Signed in automatically once more. |
| B17 | Compare `loginusers.vdf` before and after any switch | Only `RememberPassword` and the active-account marker change. **No other key is added, removed or reordered** — this is the lossless-write guarantee. |

## C. The Session Kit — the part that matters

Mark your second account **Shared**. Home must be set.

| # | Steps | Expected |
|---|---|---|
| C1 | Note the Dota settings on **both** accounts first | Write down which is which. Every later check depends on this. |
| C2 | Switch to the Shared account | Strip narrates *Closing Steam → Saving their setup → Applying my kit → Launching*. It then settles on **"Your kit is active on X"**, in amber, mentioning Dota 2. |
| C3 | Launch Dota on the shared account | Your Home settings are in effect, not theirs. |
| C4 | Switch to a third (non-shared) account | The **Restore X's setup?** prompt appears *before* anything happens. |
| C5 | In that prompt, press **Esc** | The switch is cancelled. You stay where you are, kit still active. Esc **must not** silently pick an answer. |
| C6 | Reopen the prompt, expand **What changed?** | Shows the games and scope inline, not on a second page. Nothing is clipped at 420px. |
| C7 | Choose **Restore theirs** | Their original Dota settings are back — verify in-game or by file. Kit line disappears from the strip. |
| C8 | Repeat C2, then leave with **Keep mine on it** | Your config stays on their account. The strip still shows the kit as active, and it survives an app restart. |
| C9 | While a kit is active, open Tools → Dota config library and try a manual copy | Refused, citing an active kit. This is `guardManualConfigWrite`. |
| C10 | With Steam **running**, try to switch to the shared account | Steam is closed first automatically. It should not write anything while Steam is up. |
| C11 | Start Dota, leave it running, and try to switch | Refused with "Steam or a game is still running", naming the process. Not a silent failure, not a partial write. |

### C-bypass. The ways to switch that are not a tile

Every one of these skipped the leave prompt entirely before the guard existed, so a kit could
be abandoned without ever being asked about. These are the cases that matter most in §C.

| # | Steps | Expected |
|---|---|---|
| CB1 | With a kit active on the shared account, use **tray → quick switch** to a different account | Refused, with a message saying to open the window and switch from the account list. The kit stays active and nothing on disk moves. |
| CB2 | Open the window and do the same switch from a tile | The **Restore X's setup?** prompt appears as in C4 and the switch proceeds. |
| CB3 | With a kit active, run a **desktop shortcut** for a different account | Same refusal as CB1. Check that `<Steam>\userdata\<shared id32>\570\` is untouched afterwards. |
| CB4 | With a kit active, run a desktop shortcut for **the account the kit is on** | Allowed — re-selecting the account you are already on is not leaving it. |
| CB5 | Leave an interrupted transaction unresolved (as in D1), then try a tray switch | Refused, pointing at the window. Resolving the recovery there must then let the tray work again. |
| CB6 | With **no** kit active and nothing outstanding, tray switch | Works exactly as before. The guard must be invisible in the ordinary case. |

### C-cloud. Steam Cloud honesty

| # | Steps | Expected |
|---|---|---|
| CC1 | After C2, check the strip wording | It says cloud settings **may** be overridden. It must not claim the kit is durable. |
| CC2 | Let Steam sit signed in for a minute, then check hero grids | If Cloud reverted them, that is expected behaviour, and the app should say so rather than insisting the kit is applied. |
| CC3 | Disable Steam Cloud for Dota, repeat C2 | Grids now stick. This is the documented reliable mode. |

## D. Crash recovery — the reason the journal exists

This is the most important section and the easiest to skip. Don't.

| # | Steps | Expected |
|---|---|---|
| D1 | Start a switch to the Shared account. The moment the strip says *"Applying my kit"*, kill `SteamSwitch.exe` from Task Manager | — |
| D2 | Relaunch SteamSwitch | A **blocking** modal: "Last switch didn't finish". It cannot be dismissed with Esc, a backdrop click, or a close button. |
| D3 | Try to switch accounts with the modal up | Impossible. Tiles are unreachable. |
| D4 | Expand **Inspect** | Shows phase, transaction id, start time, games. Read-only. |
| D5 | Choose **Restore their setup** | The shared account's original config is back and intact. Compare against your C1 notes. |
| D5a | Immediately after D5, check which account Steam will sign in as | It is the account you started from, **not** the shared one. A restore that puts the files back but leaves the login pointing at someone else's account is a half-undo. |
| D5b | Confirm Steam did **not** start on its own during D5 | Answering a repair prompt is not a request to launch Steam. |
| D5c | Repeat D1 but kill the app *before* the strip reaches "Applying my kit", then relaunch and expand **Inspect** | "Signed in as" shows the original account and there is no login-mismatch notice — nothing was swapped, so there is nothing to reconcile. |
| D6 | Repeat D1, then choose **Keep current** instead | The app unblocks; whatever was on disk stays. |
| D7 | Repeat D1 twice in a row without resolving between them | The second launch still shows exactly one prompt and still recovers cleanly. |
| D8 | Inspect `%AppData%\SteamSwitch\SessionKit\` after a clean switch | `transactions/` is empty; `snapshots/` and `archive/` hold history. No leftovers. |
| D9 | Inspect `<Steam>\userdata\<id32>\570\` after a clean switch | **No `.steamswitch-tx` folder is left behind.** |

### D-external. Somebody else played

| # | Steps | Expected |
|---|---|---|
| DE1 | With a kit active on the shared account, edit a file under `<Steam>\userdata\<shared id32>\570\local\` by hand | Simulates the other person playing. |
| DE2 | Try to leave with **Restore theirs** | Blocked: "Files changed outside SteamSwitch". **Nothing is overwritten.** |
| DE3 | In the prompt, the default is **Keep current** | Confirm the safe option is the default, not the destructive one. |
| DE4 | Choose **Restore saved setup anyway** | Now it overwrites, because you asked explicitly. |

## E. Multi-drive install — the one that used to break

Only meaningful if you can arrange it, but please do arrange it.

| # | Steps | Expected |
|---|---|---|
| E1 | Steam installed on a **different drive** from `%AppData%` (e.g. Steam on `D:`, Windows on `C:`) | — |
| E2 | Run C2 (switch to shared with a kit) | The kit applies. Earlier builds failed here because staging lived under the data root and `rename` cannot cross volumes. |
| E3 | Run D1–D5 on that machine | Crash recovery still finds the displaced files and restores them. |
| E4 | Dota installed in a **secondary Steam library** on yet another drive | Detection still finds it; the kit still applies. |

## F. Tools and settings

| # | Steps | Expected |
|---|---|---|
| F1 | Tools hub | Grouped under Session / Steam maintenance / Diagnostics. Rows that open a page show a chevron; one-shot actions don't. |
| F2 | Tools → Dota config library | Opens; snapshots list; a manual copy between two accounts works with no kit active. |
| F3 | Tools → Advanced cleaning | Opens; individual options behave; does not sign you out. |
| F4 | Tools → Refresh avatars / VAC status check | Each runs and reports; rows disable while one is in flight. |
| F5 | Tools → Open data folder | Explorer opens at the data folder. |
| F5a | Tools → **Back up Steam config** | Writes a zip under `<data>\Backups\Steam\`; the toast names the file and a file count. |
| F5b | Tools → **Back up everything** on the same install | A noticeably larger archive than F5a — it drops the extension filter, so screenshots and caches are included. |
| F5c | With Steam **running**, Tools → **Restore latest backup** | Refused before the confirmation appears, saying to close Steam. Steam rewrites `config/` as it exits and would undo the restore silently. |
| F5d | Close Steam, Restore latest backup, and decline the confirmation | Nothing is written. Check the modification time on `<Steam>\config\loginusers.vdf`. |
| F5e | Accept it | `config/` and `userdata/` are restored from the newest archive and the toast names it. Then launch Steam and confirm the account list is intact. |
| F5f | Tools → **Open backup folder** on a machine that has never backed up | The folder is created and opened rather than erroring. |
| F5g | Tools → **About SteamSwitch** | Shows the version and the TroubleChute attribution. |
| F6 | Settings | Everything is on **one** page: Appearance, Steam, Game modules, System, Language. No duplicated global block, no per-platform settings page. |
| F6a | Settings → Steam → **Pick Steam folder**, choose the real install | The path shown updates and a switch still works. Point it somewhere wrong and confirm the switch fails with an error naming the folder, rather than silently doing nothing. |
| F6b | Settings → Steam → uncheck **Automatically start Steam on account switch**, then switch | `loginusers.vdf` is updated and Steam is **not** launched. |
| F6c | Settings → Steam → uncheck each of SteamID / last login / account username, return to the list | The tile's monospaced meta line loses exactly that part. With all three off the line disappears rather than leaving an empty row or a stray `·`. |
| F6d | Turn on **Note preview under username**, add a note with two paragraphs via the account menu | The preview shows on one line with the newlines collapsed; the tile does not grow to fit it. |
| F6e | Settings → Steam → **Set method to close Steam** | The dropdown reads in your language, including "TaskKill". It used to be English in every locale. |
| F6f | Change any Steam setting, quit and relaunch | It stuck. Then open `Settings\SteamSettings.json` and confirm `SteamWebApiKey`, `ForgetAccountEnabled` and `AlwaysSwapOnShortcut` are gone — they had no reader. |
| F7 | Tray → quick switch | Switches without opening the window. |
| F8 | Tray while a kit is active | The tooltip reflects it. |

### F-tags. Tags

| # | Steps | Expected |
|---|---|---|
| FT1 | Account menu → Advanced → **Tags ▸ Add**, type a new name, press Enter | The tag is created and shows as a bubble on that tile. |
| FT2 | Open the same menu on a second account | The tag created in FT1 is offered in the Add list rather than needing retyping. |
| FT3 | **Tags ▸ Modify ▸ <tag> ▸ Add expiry**, set it a minute out | The bubble shows a live countdown. After it lapses, the tag is gone on the next list refresh. |
| FT4 | **Tags ▸ Remove** | Removes them all from that account; other accounts keep theirs. |
| FT5 | A tile with three or more tags | The bubbles wrap onto their own row; the Home/Shared and kit badges stay in their own cluster and are not pushed off. |

## F-modules. Game modules

| # | Steps | Expected |
|---|---|---|
| FM1 | Settings → **Game modules** | Two cards: Dota 2 and Counter-Strike 2. Each states installed / not installed and active / not running. |
| FM2 | With CS2 installed | It shows as installed but **not running**, with a reason saying support is unfinished. It must not claim CS2 is missing. |
| FM3 | Switch to a Shared account with CS2 installed | Only Dota's settings travel. Nothing under `userdata/<id32>/730/` is touched — check the modification times. |
| FM4 | Dota card → **Run self-test** | Passes, records a layout signature and a timestamp. Nothing on disk changes; compare `userdata/<id32>/570/` before and after. |
| FM5 | Dota card → **Pause**, then switch to a Shared account | The switch happens as a plain switch; no kit is applied and no journal is left in `SessionKit/transactions/`. |
| FM6 | **Resume**, switch again | The kit travels as before. |
| FM7 | After FM4, edit `<Steam>/steamapps/appmanifest_570.acf` and change `buildid`, then reopen Settings | The Dota card is **paused automatically** and says the game's files have moved. This simulates a Dota patch. |
| FM8 | Run the self-test on that paused card | It passes, records the new layout and resumes the module by itself. |
| FM9 | Restore the original `buildid`, reopen Settings | Paused again — the layout moved a second time. Self-test to settle it. |
| FM10 | Pause Dota by hand, then run the self-test | It passes, but the module **stays paused**. A self-test confirms the layout; it does not overrule a decision the user made. |

## F-macos. macOS specifics

Run the whole plan on macOS too, then these. They cover the parts where the macOS backend is a
separate implementation rather than shared code.

| # | Steps | Expected |
|---|---|---|
| M1 | With Steam **running**, try a switch | Refused with "Steam or a game is still running", or Steam is closed first — never a write while `steam_osx` is alive. Check with `pgrep -x steam_osx`. |
| M2 | Start a switch while Steam is running and watch `pgrep -x steam_osx` | Steam gets a SIGTERM and is given up to 20s to exit cleanly before SIGKILL. It must not be killed instantly: Steam flushes `loginusers.vdf` and `registry.vdf` on exit. |
| M3 | After a switch, open `~/Library/Application Support/Steam/registry.vdf` | `AutoLoginUser` under `Registry/HKCU/Software/Valve/Steam` is the new account. |
| M4 | Diff that file against a copy taken before the switch | **Only** `AutoLoginUser` and `RememberPassword` changed. Every other key — `language`, `SourceModInstallPath`, the `steamglobal` subtree, the whole `HKLM` hive — is byte-identical. |
| M5 | With Dota **running** but Steam closed, try a Dota config copy touching cloud settings | Refused. `dota2` on macOS has no `.exe` suffix; if this is *allowed*, the process-name list is wrong and every cloud guard is dead. |
| M6 | Switch with auto-start on | Steam launches via LaunchServices, and quitting SteamSwitch afterwards leaves Steam running — it must not be a child process. |
| M7 | Launch a game from a tile | The `steam://rungameid/` URL opens in Steam. |
| M8 | Settings → Steam → uncheck "Stay signed in after switching", switch | `RememberPassword` is `0` in **both** `loginusers.vdf` and `registry.vdf`. |
| M9 | Rename `/Applications/Steam.app` away, then switch with auto-start on | The login is still written; only the launch fails, with an error naming `Steam.app`. |
| M10 | Tools → Advanced Cleaning → the registry rows | They are enabled on macOS and edit `registry.vdf`. Confirm afterwards that the named key is gone and the rest of the file is intact. |

## G. Regression sweep

| # | Steps | Expected |
|---|---|---|
| G1 | Portable install (exe in its own folder) | Data goes to `<exe folder>\SteamSwitch\`, not `%AppData%`. |
| G2 | Set an app-lock password, restart | Unlock is required before any account data is reachable. |
| G3 | Turn on Offline mode | No outbound requests. Avatars stop refreshing; nothing hangs waiting on the network. |
| G4 | Create a desktop shortcut for an account, run it with the app closed | Switches correctly and exits. |
| G5 | `steamswitch://` protocol link | Handled by the running instance rather than starting a second one. |
| G6 | Launch a second copy while one is running | The second forwards its arguments and exits; only one window. |
| G7 | Switch the app language | Every visible string changes; no raw keys like `Kit_Leave_Title` leak into the UI. |
| G8 | Tab through the main window and both kit dialogs | Focus is trapped inside a dialog while it's open, and lands on the primary button first. |

---

## Reporting a failure

Include: the section number, what you expected, what happened, and — for anything in §C or §D —
the contents of `%AppData%\SteamSwitch\SessionKit\active.json` plus the matching file under
`transactions/`. Those two identify the exact point the transaction reached.

`internal/actionlog` keeps a rolling record of user actions that is attached to crash reports,
and `internal/logsanitize.Redact` replaces account identifiers with `accountN` aliases first,
so a log is safe to share.

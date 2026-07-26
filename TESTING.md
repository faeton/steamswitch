# Manual test plan

This is the by-hand pass to run on a real Windows machine before a release. It is not CI — it
covers the things automated tests structurally cannot: the Windows registry, real Steam
process behaviour, Steam Cloud, and what the UI actually looks like at 420×520.

**Everything below assumes Windows.** On macOS or Linux the app builds and renders but cannot
switch: `internal/winutil`'s non-Windows stubs return `ErrUnsupported` for registry writes,
process termination and process launch, and `IsExeRunning` always returns false. Nothing in
sections B–F can be exercised there.

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
| B14 | With it off, open `<Steam>/config/loginusers.vdf` | Every account reads `"RememberPassword" "0"`, including the one just switched to. |
| B15 | Windows only, with it off: check `HKCU\Software\Valve\Steam` | `RememberPassword` is `0`, matching the file. The two must never disagree. |
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
| F6 | Settings | Everything is on **one** page. No duplicated global block, no per-platform settings page. |
| F7 | Tray → quick switch | Switches without opening the window. |
| F8 | Tray while a kit is active | The tooltip reflects it. |

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

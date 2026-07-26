# Manual test plan

This is the by-hand pass to run on a real machine before a release. It is not CI — it covers
the things automated tests structurally cannot: the Windows registry (or macOS `registry.vdf`),
real Steam process behaviour, Steam Cloud, and what the UI actually looks like at 420×520.

**Read `FEATURES.md` first.** It lists what is built feature by feature, says how confident
each piece is, and orders the sections below into six blocks you can run one at a time. The
distinction it draws matters here: most of this plan re-checks things that already work, but a
handful of steps — §V4 above all — are the first time that code has ever run for real. A
failure in the first kind is a bug; a failure in the second is expected-until-proven, and the
useful report is *how far it got*.

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

§V (the account vault) needs a little more — a test account you do not mind locking out, a
free Steam Web API key, and optionally an inbox you can reach over IMAP. Its own preamble
lists them, and the section can be skipped entirely if you are only checking switching.

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
| CB7 | Read the tray refusal message in CB1 | It is a **sentence**, in your language. If it reads `Toast_Kit_LeaveRequiredOutsideWindow`, the key is reaching the notification untranslated. |
| CB8 | Repeat CB1 as a `steamswitch://` link instead of the tray | Same sentence in a toast, again not a raw key. |
| CB9 | With a kit active, Tools → **Restore latest backup** | Refused before the confirmation. A restore overwrites the whole of `config/` and `userdata/`, which is what the kit is applied to. |
| CB10 | With a kit active on account X, use a shortcut for **X** that launches a game, then repeat it with an interrupted transaction outstanding | Allowed in the first case, refused in the second — even though the login already names X. The point is that nothing launches on top of files the engine has not resolved. |

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
| M11 | Run §V with the data root at `~/Library/Application Support/SteamSwitch/` | The vault blob lands under `Vault/secrets.ssvault` there, and `ls -l` shows `-rw-------`. |
| M12 | Store a vault entry on macOS, copy the whole data folder to Windows, and open it with the same app password | It opens. The blob format is not OS-specific and must not become so — the same file is what a future handoff bundle builds on. |

## V. Account vault

New in this build, and the least proven part of it. Sections V1–V3 can be run with nothing
but a throwaway account; **V4 is the only place the Steam login code has ever run against
Valve's servers**, so treat a failure there as expected-until-proven rather than as a
surprise.

### Before this section

- Somewhere to put a **test account you do not mind locking out**. V4 attempts real logins
  and a wrong password repeated enough times will get the account rate-limited by Steam.
- A **Steam Web API key** from `steamcommunity.com/dev/apikey`. Free, takes a minute.
  V2 covers what happens without one, so get it after V2a.
- Optionally, an email account you can reach over IMAP. If you have none, V3 can be skipped
  and V4 run with an authenticator seed instead.

> **Nothing in this section should ever put a readable secret on disk.** V1c is the check
> that matters most; if it fails, stop and report it before running anything else.

### V1. The store

| # | Steps | Expected |
|---|---|---|
| V1a | With **no app password set**, Settings → Account vault | It says the vault needs an app password and offers to set one. There is no toggle that pretends to enable it without one. |
| V1b | Set an app password, then right-click an account → Advanced → **Vault…** | The editor opens. Every secret field is empty with no "stored" marker yet. |
| V1c | Enter a password, an authenticator seed and an email address, Save. Then open `%AppData%\SteamSwitch\Vault\secrets.ssvault` in a text editor | **A JSON envelope and nothing else.** Search it for the password, the account name, the email address and the SteamID64 — none may appear. If any does, this is a release blocker. |
| V1d | Check the file's permissions | Owner-only. macOS: `ls -l` shows `-rw-------`. Windows: Properties → Security lists only your account. |
| V1e | Reopen the editor | Password and seed show `••• stored`, not the value, and not a masked field containing the value. |
| V1f | Press **Save** without touching anything, then Reveal the password | Still the original. Saving a form that never displayed a secret must not erase it. |
| V1g | Click **Reveal** on the password | It appears once, with a Copy button, and **hides itself after about 20 seconds** without being touched. |
| V1h | Reveal, then close the dialog immediately | Nothing is left on screen anywhere, and reopening does not show it again. |
| V1i | Edit the entry, tick **Store and check only**, Save. Look at the account list | The tile is unchanged (it is still a real Steam account); the entry is marked *store only* in Tools → Stored accounts. |
| V1j | Delete the entry, then look at the blob again | The file still exists but is smaller. Reopening the vault shows the entry gone. |
| V1k | Corrupt the blob: change one character inside `"ciphertext"` and relaunch | The vault reports the file could not be read. It must **not** silently start empty — that would look like your data vanished. |
| V1l | Restore the good file, restart, and unlock | Entries are back. |

### V2. Locking

| # | Steps | Expected |
|---|---|---|
| V2a | With the vault holding an entry, quit and relaunch the app | Unlock is required. Before unlocking, Settings → Account vault says it is locked, and no tile shows a health dot. |
| V2b | Before unlocking, open an account menu | Vault, health, Guard code and login details are all disabled, and opening the menu does **not** trigger an unlock prompt. |
| V2c | Unlock, then check Settings → Account vault | The entry count appears. |

### V3. Guard codes

| # | Steps | Expected |
|---|---|---|
| V3a | Store an authenticator seed for an account, then account menu → **Get Steam Guard code** | A 5-character code appears within a second, marked "From your authenticator seed", with a countdown, and it is already on the clipboard. |
| V3b | Compare it against the code your phone shows for the same account, at the same moment | **Identical.** If not, the seed or the code generation is wrong and V4 will fail too. |
| V3c | Watch the countdown to zero, then press **Get another** | A different code. |
| V3d | Paste a code into a real Steam login | Steam accepts it. |
| V3e | Set the code source to **an inbox (IMAP)**, enter the address and password, press **Detect** | It finds the IMAP host without you knowing its name. If your provider is unusual it may fail — enter the host by hand and continue. |
| V3e2 | Detect against a **vanity domain** whose mail is served by another host (e.g. a bought-account provider) | It still finds it, and the host it fills in is the one the certificate names, not `imap.<your-domain>`. This is the case detection exists for; if it only ever works on Gmail-shaped addresses, it is not doing its job. |
| V3e3 | Detect with the **right address and a wrong password** | It reports that the mailbox rejected the credentials, not "no server found". Finding the host and failing to log in are different answers. |
| V3f | Press **Test the connection** | Confirms it connected. With a deliberately wrong password it says the mailbox rejected the credentials, distinct from "could not connect". |
| V3g | Remove the authenticator seed so the inbox is the only source. Trigger a real Steam login that sends a code, then **Get Steam Guard code** | The code from that mail, marked "From your inbox". |
| V3h | Check that mail in your normal mail client | It is **still unread**. Reading a code must never mark mail as read. |
| V3i | Ask for a code when no new mail is coming | It polls with a visible wait and then gives up cleanly, telling you that you can still type the code yourself. It must not hang forever. |
| V3j | Ask for a code immediately after an *older* login's mail | It does **not** hand you the old code. Steam codes are single-use, and a dead one is worse than none because you cannot tell it is dead. |
| V3k | If your inbox receives codes for several accounts: ask for account A's code while account B's is the newest mail | You get A's, or none. Never B's. |
| V3l | Turn on Offline mode, then ask for a code from the inbox | Refused because of offline mode. A TOTP code still works — it needs no network. |
| V3m | Set the source to **a mailbox service I run** and enter a plain `http://` URL | Refused. The request carries a bearer token and the reply carries a Guard code; neither belongs on plain HTTP. |
| V3n | Start a switch to an account whose code comes from an inbox, and watch the timing | The fetch starts as the switch starts, not when you ask. By the time Steam shows its prompt the code is usually already available. |
| V3o | Start such a switch and make it fail (e.g. rename Steam's exe first) | No mail connection is left running afterwards. |

### V4. Verification

`internal/vault/steamauth` now follows the request shape that has been running against
Valve's servers in `ggcr-bot-fleet` for a long time and across many accounts: form-encoded
parameters, JSON replies, no protobuf. That removes the main reason this section used to
carry a warning.

What is still unproven is **this** implementation of that shape — the parameter set, the
`,string` decoding of 64-bit ids and the guard-type handling are checked by unit tests but
have not themselves completed a real login. Treat V4a as the first real run.

| # | Steps | Expected |
|---|---|---|
| V4a | Store the correct login name and password for a test account, then account menu → Advanced → **Account health…** → **Verify password** | It reports the password works. If Steam sends a Guard email, the app answers it automatically when a code source is configured. |
| V4b | Change the stored password to something wrong, verify again | "Steam rejected the stored password", marked as a blocker. Not "could not be confirmed" — the two mean different things. |
| V4b2 | Change the stored **login name** to one that does not exist, verify | "Steam has no account with that login name", pointing at the name rather than the password. These arrive as different Steam result codes and must not be collapsed. |
| V4c | Verify twice in a row | The second run does **not** send a second Guard email: the first stored a trusted-device token. |
| V4d | Verify a third and fourth time in quick succession | If Steam rate-limits, the app says so, disables Verify for the session, and does **not** retry. Retrying is what turns a warning into a block. |
| V4e | Start a verify on one account, then immediately on another | The second is refused with "a verification is already running". They are serialised on purpose. |
| V4f | After a successful verify, open Advanced → **Login details…** | Audience, issue and expiry dates, token ID and IP claims are shown. If the audience includes `client`, the panel warns that anyone holding the token can sign in from any machine. |
| V4g | Press **Reveal the raw token** | The token appears and hides itself after about 20 seconds. |
| V4h | Confirm the panel is read-only | Nothing on it writes. Compare `loginusers.vdf` before and after opening it — byte-identical. |
| V4i | Remove the stored password, open the health screen | **Verify password** is disabled. There is nothing to verify. |

### V5. Health checks

| # | Steps | Expected |
|---|---|---|
| V5a | With **no** Web API key set, account menu → Advanced → **Account health…** | Bans and profile say they need an API key. The other results still work. The feature is degraded, not broken. |
| V5b | Add the key in Settings → Account vault, run **Check** again | Bans and profile now report real values. |
| V5c | Check a healthy account | Verdict "Looks fine"; **no dot appears on its tile**. |
| V5d | Check a VAC-banned account if you have one | Verdict "Something is wrong", the ban row is marked as a blocker, and a red dot appears on the tile. |
| V5e | Check an account created under 30 days ago | Warned as probably still limited, and the wording says it is **inferred from the creation date** rather than reported by Steam. |
| V5f | Check an account not signed into for months | Warned as idle, with the day count. |
| V5g | Hover the tile dot | The tooltip says which state it is. Colour is never the only signal. |
| V5h | Tools → Account vault → **Check every account** | Every stored account is checked, one after another, and the summary names how many. It must not fan out. |
| V5i | Confirm no Guard email arrives from V5h | The cheap tier never logs in. If an email arrives, the tiers have been confused and that is a release blocker. |
| V5j | Tools → **Stored accounts** | Every entry including store-only ones, each with its verdict. No secret values anywhere on this screen. |

### V6. Vault against the rest of the app

| # | Steps | Expected |
|---|---|---|
| V6a | With a vault entry for a shared account, switch into it with a Session Kit involved | Both work. The vault takes no part in the kit transaction; the switch behaves exactly as in §C. |
| V6b | Trigger a leave prompt while a code pre-warm is running | The prompt behaves normally. The pre-warm must not block, delay or fail the switch. |
| V6c | Switch from the tray while the vault is locked | The switch works. A locked vault must never block switching. |
| V6d | Remove the app password entirely | The vault becomes unreadable, and says so plainly rather than reporting corruption or starting empty. |
| V6e | Turn on Offline mode and run a **Check** | Reported as unavailable because of offline mode, not as a timeout. |
| V6f | Look at `%AppData%\SteamSwitch\` logs after all of the above | No password, seed, Guard code, token or email password appears anywhere in them. |
| V6g | Trigger a crash report (Tools → Diagnostics) and read what it would send | Same: account identifiers are aliased to `accountN`, and no vault value is present. |

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
| G9 | With the vault in use, switch the app language | Vault strings change too. No raw keys like `Vault_Signal_NoBans` or `Toast_Vault_Locked` leak into the UI — the Go side returns those keys as its error messages, so an untranslated one shows up as gibberish rather than as a missing string. |
| G10 | Run the whole of §V with **Offline mode on** from the start | Everything that needs the network says so. Nothing hangs, and the vault's local operations — storing, revealing, TOTP codes — all still work. |

---

## Reporting a failure

Include: the section number, what you expected, what happened, and — for anything in §C or §D —
the contents of `%AppData%\SteamSwitch\SessionKit\active.json` plus the matching file under
`transactions/`. Those two identify the exact point the transaction reached.

For §V, include the section number and the message shown, and **never** the vault blob or
anything you revealed from it. If §V4 fails, the useful detail is which step it reached —
whether Steam was asked for the RSA key at all, whether it asked for a Guard code, and
whether the failure names a rejected password or an unrecognised response. Those three
distinguish "the account is fine and this code is wrong" from the opposite.

`internal/actionlog` keeps a rolling record of user actions that is attached to crash reports,
and `internal/logsanitize.Redact` replaces account identifiers with `accountN` aliases first,
so a log is safe to share.

# SteamSwitch

**A Steam account switcher that protects your game settings.**

Switch between your Steam accounts in one click — no passwords, no 2FA codes. And when you log
into an account you share with someone else, SteamSwitch can carry *your* game config with you
and put *theirs* back when you leave.

Windows only. GPL-3.0. No telemetry, no update check, no account of any kind.

> **Status: early alpha — v0.1.0. Not yet run on Windows.**
>
> This is a source snapshot, not a release. The switching engine, the Session Kit and the
> Dota config handling were written and unit-tested on macOS, where the Windows primitives
> they depend on are compile-only stubs — so the actual switch path has **never been executed
> end to end on the one OS it supports**. There are no binaries and no releases yet.
>
> If you want to try it, build from source, read [`TESTING.md`](TESTING.md), and don't point
> it at an account you couldn't log back into by hand.

> SteamSwitch started life as a Steam-only fork of
> [TcNo Account Switcher](https://github.com/TCNOco/TcNo-Acc-Switcher) by TroubleChute —
> the inspiration and the original codebase for this project. It is **not affiliated with
> or supported by** that project, so please don't send SteamSwitch problems upstream.
> See [Credits](#credits) for attribution and [`FORK.md`](FORK.md) for everything that
> differs.

---

## ⚠️ Read this before using Shared accounts

The account switching itself is boring and safe. The **Session Kit** — the part that overwrites
game config folders — is the part that can lose data if you don't know what it does.

- **Close Steam and the game first.** SteamSwitch refuses to write while either is running.
  That refusal is protecting you, not being awkward.
- **Steam Cloud can undo a kit.** Dota's `remote/` folder (hero grids, item builds) is
  cloud-synced. SteamSwitch writes it, then Steam may pull the old copy back after the next
  login. The status line says *"cloud may override grids"* rather than claiming success. The
  only fully reliable mode is disabling Steam Cloud for that game on this PC.
- **"Keep mine on it" leaves your settings on someone else's account.** They'll play with your
  keybinds until you restore. That's a social failure mode, not a technical one.
- **Restoring gives back this PC's last saved setup** — not everything the other person has
  ever had, and nothing from their other machines.
- **If someone else played in the meantime, SteamSwitch stops.** Live files that no longer
  match what it wrote mean somebody's work is at stake, so the default is *no write* and you
  get asked.
- **Don't hand-delete the data folder to "fix" a stuck switch.** Use the recovery prompt — it's
  the only thing that knows which files were moved where.
- **Antivirus and Controlled Folder Access can block it.** SteamSwitch needs write access to
  Steam's `userdata` folder and to its own data folder.

---

## How switching works

Steam remembers your last login in `loginusers.vdf`, a couple of registry values under
`HKCU\Software\Valve\Steam`, and `config.vdf`. SteamSwitch saves those per account while Steam
is closed, and swaps them back in when you pick a different account. It never sees or stores a
password, and 2FA is untouched because Steam still considers the machine already authorised —
the machine-auth token is one of the files being swapped.

**What it isn't:** not a cheat, not a VAC bypass, not a credential manager, not a bot, not a
multi-platform launcher, and not a backup product.

---

## The Session Kit

Three roles, set from an account's right-click menu:

| Role | Meaning |
|---|---|
| **Home** | Your main account, one per PC. Your kit is taken from here. |
| **Shared** | An account someone else also uses. Your kit travels here. |
| *(neither)* | An ordinary alt. Plain switch, nothing is copied. |

### Switching to a Shared account

1. Close Steam.
2. **Save their setup** — snapshot the shared account's Dota config, hashed file by file.
3. **Apply my kit** — copy Home's config onto it, staged and verified.
4. Swap the login and relaunch Steam.

The status strip narrates each step, then sits on **"Your kit is active on *X*"** until you
leave.

### Leaving a Shared account

You're asked every time:

> **Restore *X*'s setup?**
> [ **Restore theirs** ] [ Keep mine on it ]

"Restore theirs" is the default. "Keep mine" leaves your config in place and keeps tracking it,
so you can still restore later.

### If something goes wrong

Every mutation is written to a transaction journal on disk *before* it happens. If SteamSwitch
is killed mid-switch — crash, power cut, Task Manager — the next launch **blocks switching** and
shows what was interrupted, offering to restore, keep what's on disk, or discard.

Files are replaced by staging a copy on the same drive and renaming it into place, keeping the
displaced copy until the transaction commits. There is no point at which the destination is
simply deleted.

---

## Features

### Account switching
- One-click switching between saved Steam accounts; `1`–`4` for the first four.
- Log in as Invisible / Offline and other persona states, copy SteamID formats and profile links.
- Per-account notes and custom images; desktop shortcuts for quick switching.
- Tray menu for switching without opening the window.

### Session Kit (shared accounts)
- Home / Shared roles with per-account kit indicators.
- Dota 2 module covering local settings and cloud settings (hero grids, builds).
- Journalled, hash-verified and crash-recoverable, with external-change detection.
- The machine-wide Dota `cfg` folder is **never** part of a kit.

### Dota 2 configs (Tools)
- **Copy between accounts** — move `userdata/<id>/570` settings from one account to another.
  Neither account has to be the one currently signed in.
- **Named config library** — save a config as "my setup" or a friend's config, then apply it to
  any account later.
- **Automatic revert points** — every overwrite first snapshots the destination, so an Undo is
  always one click away.
- Local settings, cloud settings and the shared game `cfg` folder are selected independently,
  with warnings where Steam Cloud can undo a change.

### Steam maintenance (Tools)
- Advanced Cleaning for individual caches, logs, dumps and login files.
- Avatar refresh and VAC status check.
- Configurable account refresh: on launch, on a timer, and avatar cache expiry.

### Appearance
- System / Light / Dark, following the OS by default. One accent colour.
- The inherited theme packs are still there, collapsed under "Classic themes".

---

## Quick start

1. Launch SteamSwitch. Accounts you've already logged into appear automatically.
2. Right-click your main account → **Set as Home**.
3. Right-click an account you share → **Mark as shared account**.
4. Click a tile to switch.

To use the Dota kit, Dota 2 must be installed and both accounts must have played it at least
once, so the config folders exist.

---

## Where your data lives

```
%AppData%\SteamSwitch\            (or <exe folder>\SteamSwitch\ when portable)
  steamswitch.settings.json       app settings
  Settings/                       per-platform settings
  LoginCache/Steam/<account>/     saved login blocks
  SessionKit/                     transaction journals and config snapshots
  Backups/Dota/                   the config library and revert points
  wwwroot/                        your own images and themes
```

**Uninstalling does not remove this folder.** Delete it by hand if you want the saved account
data gone; doing so does not sign you out of Steam.

---

## Privacy

SteamSwitch sends **nothing** anywhere. There is no telemetry, no crash reporting, no
feedback channel and no update check. Statistics and crash dumps are written to your own
data folder and never uploaded. The only outbound requests are to Valve, to fetch public
profile data (avatars, VAC status, game names) — and those stop entirely if you turn on
Offline mode in settings.

---

## Troubleshooting

**"Steam or a game is still running."** Something is holding the config files. Close Steam and
Dota fully — check Task Manager for `steam.exe`, `steamwebhelper.exe` and `dota2.exe`.

**"Files changed outside SteamSwitch."** The live files no longer match what SteamSwitch wrote.
Usually the other person played, or Steam Cloud pulled a different copy. Nothing has been
overwritten; choose whether to keep what's there or restore the saved setup.

**My hero grids came back wrong after a switch.** Steam Cloud won. Re-apply with Steam closed,
and consider disabling Steam Cloud for Dota on this PC.

**"Last switch didn't finish."** SteamSwitch was killed mid-write. Answer the recovery prompt;
it knows which files were moved and where they went.

**Switching does nothing, or asks for admin.** Steam's registry values or `userdata` folder
aren't writable by your user. Try running SteamSwitch as administrator once.

---

## Building

Requires Go, Node.js with pnpm, and the Wails v3 CLI.

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117

wails3 task common:generate:bindings   # required before any frontend command
cd frontend && pnpm install && cd ..

wails3 task dev       # run with live reload
wails3 task build     # build into bin/
wails3 task package   # Windows installer
wails3 task test      # Go + frontend tests
```

`frontend/bindings/` is generated and gitignored; frontend typechecking and tests cannot
resolve imports until it exists.

**On macOS or Linux the app compiles but cannot switch accounts.** The `!windows` builds of
`internal/winutil` are stubs: registry access, process termination and process launching all
return `ErrUnsupported`, and `IsExeRunning` always reports false. `go test ./...` reports 16
failures there, inherited from upstream, that assume Windows path, registry and file-locking
behaviour. See `FORK.md` for the exact list — that is the expected baseline, not a regression.

[`TESTING.md`](TESTING.md) is the manual test plan for a real Windows machine.

## Credits

SteamSwitch exists because of
[TcNo Account Switcher](https://github.com/TCNOco/TcNo-Acc-Switcher) by
**TroubleChute (Wesley Pyburn)** — it was both the inspiration and the codebase this fork
began from. If you need to switch accounts on platforms other than Steam, go use the
original: it supports two dozen of them and is actively maintained.

This fork narrows the scope to Steam, drops all outbound reporting, and adds Dota 2 config
management. [`FORK.md`](FORK.md) lists every divergence.

## Licence

GPL-3.0. Original work © TroubleChute (Wesley Pyburn); fork modifications © 2026
Ivan Faeton Danishevskyi. See [`LICENSE`](LICENSE) and [`NOTICE.md`](NOTICE.md).

Steam and Dota 2 are trademarks of Valve Corporation. This project is not affiliated with
Valve. It only moves files that are already on your computer.

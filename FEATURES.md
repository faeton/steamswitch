# What is built

An inventory of everything implemented, feature by feature, with how confident each
one is and where to test it. `TESTING.md` is the by-hand plan; this file says what
that plan is *for* and which parts of it are checking new ground rather than
re-checking something that already works.

`FORK.md` covers what diverges from upstream TcNo Account Switcher. This file covers
what exists now, regardless of origin.

## How confident each thing is

| | Meaning |
|---|---|
| **Proven** | Shipped, unit-tested, and exercised by hand on a real install. |
| **Built** | Shipped and unit-tested, but the by-hand pass has not run against it yet. |
| **Live-unproven** | The code path talks to something outside the app (Valve, an IMAP host) and has never completed that exchange for real. Unit tests cover the shapes, not the exchange. |
| **Partial** | Deliberately half-done; the missing half is named. |
| **Not started** | Designed, nothing written. |

A **Built** failure is a bug. A **Live-unproven** failure is expected-until-proven, and
the useful report is *how far it got*, not that it failed.

---

## 1. Account switching

The original job, and the only thing that has to be right every single time.

| Feature | Status | Where | Test |
|---|---|---|---|
| Switch Steam accounts by swapping `loginusers.vdf` + registry while Steam is closed | Proven | `internal/steam/switcher.go` | §B |
| Lossless writes — only `AutoLoginUser` / `RememberPassword` change, nothing else in the file moves | Proven | `internal/steam/registryvdf.go`, `loginusers.go` | B17, M4 |
| A switch no longer signs *other* accounts out | Built | `internal/steam/switcher.go` | B14a |
| **Stay signed in after switching** opt-out, scoped to the target account only | Built | Steam settings | B11–B16 |
| One switch at a time — a second click is ignored, not queued | Proven | swap gate | B4, B6 |
| Keyboard `1`–`4` for the first four tiles | Built | `pages/Accounts.svelte` | B3 |
| Log in as Invisible / Offline / Away (persona state on switch) | Built | account menu → Advanced | B5 |
| Home and Shared roles, mutually exclusive | Built | `internal/steam` account metadata | B7, B8 |
| Per-account notes, tags, tag expiry, custom order | Built | `internal/basic` metadata + Steam list | B10, §F-tags |
| Copy SteamID64 / ID32 / profile URL / login name | Proven | account menu | B9 |
| Tray quick-switch, desktop shortcuts, `steamswitch://` links | Built | `internal/tray`, `internal/shortcuts`, `internal/cli` | F7, G4, G5 |
| Single instance — a second launch forwards its arguments and exits | Proven | `internal/ipc`, singleton mutex | G6 |

## 2. The Session Kit

Borrow someone's account without overwriting their settings, and put their setup back
when you leave. The whole feature rests on one rule: **leaving a kitted account must
ask.**

| Feature | Status | Where | Test |
|---|---|---|---|
| Apply your Home config to a Shared account on switch | Built | `internal/sessionkit` | C2, C3 |
| Save their setup first, restore it on leave | Built | snapshot + restore | C4, C7 |
| Leave prompt — restore theirs / keep mine, Esc cancels rather than choosing | Built | `Accounts.svelte` + engine | C5, C6, C8 |
| The leave rule enforced in **Go**, so tray / shortcuts / URLs cannot bypass it | Built | `internal/steam/sessionkit_guard.go` | §C-bypass |
| Crash recovery — a blocking, undismissable repair prompt after an interrupted switch | Built | journal in `SessionKit/transactions/` | §D |
| Restore also puts the *login* back, not just the files | Built | recovery path | D5a |
| Outside-change detection — refuses to overwrite files someone else edited | Built | layout hash | §D-external |
| Manual Dota copies refused while a kit is active | Built | `guardManualConfigWrite` | C9 |
| Steam Cloud honesty — the UI says "may be overridden", never claims durability | Built | strip wording | §C-cloud |
| Multi-drive installs — staging next to the target, not under the data root | Built | staging path | §E |

## 3. Game modules

The kit is pluggable per game. v1 ships the interface with Dota 2 real and CS2 declared.

| Feature | Status | Where | Test |
|---|---|---|---|
| Dota 2 module — `local` + `remote` travel with the kit | Built | `internal/steam/dota.go` | FM3–FM6 |
| CS2 module — declared, detected, honestly reported as unfinished | Built | module registry | FM2 |
| Self-test that records a layout signature | Built | module self-test | FM4 |
| Auto-pause when the game's file layout moves (a patch) | Built | `appmanifest` buildid watch | FM7–FM9 |
| A manual pause outranks a passing self-test | Built | module state | FM10 |

## 4. Dota 2 config library

Fork-only. The thing upstream structurally could not do: copy account → account.

| Feature | Status | Where | Test |
|---|---|---|---|
| Account → account copy of `userdata/<id32>/570/…` | Proven | `CopyDotaConfigBetween` | F2 |
| Named snapshot library with labels and notes | Built | `<DataRoot>/Backups/Dota/` | F2 |
| Automatic revert point before every write | Built | `applyDotaParts` | F2 |
| `remote/` refused while Steam is running (Cloud would revert it) | Built | `dotaSteamRunningGuard` | M5 |
| `globalcfg` resolved across every Steam library, skipped account-to-account | Built | `libraryfolders.vdf` walk | — |

## 5. Tools and maintenance

| Feature | Status | Where | Test |
|---|---|---|---|
| Advanced cleaning — individual clearing actions | Built | `pages/SteamAdvancedClearing.svelte` | F3 |
| **Refresh caches** / **Deep refresh** presets, both login-safe | Built | `internal/steam/refresh_preset.go` | F3 |
| Back up Steam config / back up everything, to a zip | Built | `<data>/Backups/Steam/` | F5a, F5b |
| Restore latest backup, refused while Steam is running | Built | restore path | F5c–F5e |
| Refresh avatars, VAC status check | Built | Tools hub | F4 |
| Refresh cadence — on start, every N minutes, avatar expiry | Built | `internal/steam/refresh_schedule.go` | — |
| Local statistics and crash logging, never uploaded | Built | `internal/stats`, `internal/crashlog` | V6g |

## 6. Account vault

Newest and least proven. An encrypted store for what an account *is* — its password,
authenticator seed, email binding and health — sealed under the app-lock password.

**Phases 1–4 are built. Phase 5 (handoff) is not started.**

| Feature | Status | Where | Test |
|---|---|---|---|
| Sealed single-blob store, AES-256-GCM under an HKDF subkey of the app-lock master key | Built | `internal/vault/store.go`, `internal/security/subkey.go` | V1c, V1d |
| No plaintext metadata — no account name, email or SteamID64 outside the ciphertext | Built | one blob, no per-entry framing | V1c |
| Presence flags, never values, everywhere except an explicit Reveal | Built | `Summary`, `Reveal` | V1e, V5j |
| Reveal one field at a time, self-hiding after ~20s | Built | vault modals | V1g, V1h |
| Saving a form that never displayed a secret does not erase it | Built | pointer-field `Draft` | V1f |
| Locked by default; a locked vault never blocks switching | Built | `RequireUnlocked` + `DropCache` | V2a–V2c, V6c |
| Corruption reported as corruption, never as "empty" | Built | `ErrCorrupt` | V1k |
| **Steam Guard codes — TOTP** from a stored seed | Built | `internal/vault/totp` | V3a–V3d |
| **Guard codes — IMAP inbox**, read-only, never marks mail read | Live-unproven | `internal/vault/mail/imap.go` | V3g, V3h |
| IMAP host detection, including vanity domains served by another host | Live-unproven | cert-SAN probe | V3e, V3e2 |
| Code extraction that rejects stale, misaddressed and lookalike-sender mail | Built | `internal/vault/mail/code.go` | V3j, V3k, `FromSteam` tests |
| Mailbox-service source, HTTPS-only | Built | `internal/vault/mail/mailbox.go` | V3m |
| Pre-warm — the code fetch starts when the switch starts, not when you ask | Built | `vault.Prewarm` in the swap gate | V3n, V3o |
| **Cheap health check** — bans, profile, account age, idle days. Never logs in. | Built | `internal/vault/health.go`, `probe/` | V5a–V5g |
| Check every stored account, serialised | Built | Tools → Account vault | V5h, V5i |
| **Deep check — a real Steam login** to prove the password | Live-unproven | `internal/vault/steamauth` | §V4 |
| Distinguishing wrong password / no such account / Steam unreachable | Live-unproven | `classifyLoginError` | V4b, V4b2 |
| Rate-limit latch — says so, disables Verify, does not retry | Built | `deepMu` + latch | V4d, V4e |
| Login-details panel — audience, expiry, IP claims, read-only | Built | `TokenDetails` | V4f–V4h |
| Health dot on the tile, warn/fail only, never colour alone | Built | `AccountTile.svelte` | V5c, V5d, V5g |
| Vault values kept out of `actionlog` and `slog` | Built | call sites | V6f, V6g |

### Vault: what is deliberately missing

| | Why |
|---|---|
| **Handoff (phase 5)** — giving an account to another person | Not started. It is the only part that can hurt someone other than the user running it, so it is last. Design is settled in `VAULT.md` §9: two modes, never a "lend" button, a file the user moves themselves, no server or relay. The honest premise is that a client-audience refresh token *is* full account access with no revocation, so the UI must not imply one exists. |
| **Scheduled deep checks** | Partial. The persisted `nextEligibleAt`, the backoff and the rate-limit latch exist and are tested. Nothing *fires* on that schedule — deep checks are user-initiated only. |
| **Token injection** ("log in for me") | Research-only, unproven, and `VAULT.md` §6.2 says so. Verification proves a password works; it does not sign you in. |
| **Decoding Steam's `ConnectCache` / `MachineAuth`** | The panel reports presence, size and mtime and stops. On macOS the Keychain-derived key has not been reversed at all. |

## 7. Platform support

| | Status | Notes |
|---|---|---|
| **Windows** | Proven | The original target. Registry, `steam.exe`, WebView2. |
| **macOS** | Built | A real backend, not a stub: `registry.vdf` instead of the registry, `pgrep -x` + SIGTERM→SIGKILL, `open -a Steam.app`. Tier 1 — run the whole plan here too. §F-macos covers what is genuinely different. |
| **Linux** | Refuses, on purpose | Builds and renders; every write path returns `Toast_Steam_SwitchingUnsupportedOnThisOS`. Flatpak/Snap installs hide Steam from process lookup, so a partial backend would confidently report "Steam is closed" and let Cloud silently revert the write. The only thing worth testing is that the refusal happens. |

## 8. Cross-cutting

| Feature | Status | Test |
|---|---|---|
| App-lock password (Argon2id) + encrypted account caches | Built | G2 |
| Offline mode enforced at the transport layer, not per-caller | Built | G3, V3l, V6e, G10 |
| Portable install — data next to the exe | Built | G1 |
| 30+ locales; all user-visible strings translated, including tray and native surfaces | Built | G7, G9, CB7 |
| Themes: system/light/dark plus classic theme packs | Built | A5, A6 |
| No outbound reporting of any kind | By construction | `internal/api` returns empty URLs; every caller early-returns |
| Auto-update disabled | By construction | Upstream's feed would replace this build with the 24-platform app |

---

# How to test it

Run `TESTING.md`, but run it in this order. Each block is self-contained: finish one,
report, then start the next. Stopping after any block leaves you with a useful result.

Do the **whole sequence on Windows and again on macOS.** The OS-specific half of the
engine is a separate implementation on each, so a pass on one says nothing about the
other.

### Block 1 — does it still switch? (~30 min)

The regression floor. If this fails, nothing else matters.

1. §A — first run, empty state, themes, window at minimum size.
2. §B — plain switching, B1 through B10.
3. §B-remember — B11 through B17. **B14a and B17 are the important ones**: they check
   that a switch does not look like a logout and that no other key in `loginusers.vdf`
   moves.
4. §G1–G8 — the regression sweep.

Stop here and report if anything failed. Everything below builds on switching working.

### Block 2 — the Session Kit (~1 h)

The feature with the most ways to go wrong, because it writes to somebody else's files.

5. §C — C1 first (write down whose settings are whose; every later check depends on it),
   then C2 through C11.
6. §C-bypass — CB1 through CB10. This is the block that matters most: every one of
   these paths skipped the leave prompt entirely before the guard existed.
7. §C-cloud — CC1 through CC3.
8. §D — crash recovery. **Do not skip it.** It is the most important section and the
   easiest to skip because it means deliberately killing the app mid-write.
9. §D-external — somebody else played.
10. §E — multi-drive, if you can arrange it. Please arrange it; it is the case that
    used to break.

### Block 3 — modules, tools, settings (~40 min)

11. §F-modules — FM1 through FM10.
12. §F — the Tools hub, backups, settings. F5c–F5e (restore) touch real data; take the
    `userdata/` copy the preamble asks for before running them.
13. §F-tags — tags and expiry.

### Block 4 — the vault, local half (~40 min)

Nothing here talks to Valve. It can be run on a throwaway account with no API key.

14. §V1 — the store. **V1c is the single most important step in §V**: open the blob in
    a text editor and search it for the password, the account name, the email and the
    SteamID64. If any of them appears, stop and report it before running anything else.
15. §V1d — file permissions.
16. §V2 — locking.
17. §V3a–V3d — TOTP Guard codes. V3b is the proof: the code must be **identical** to
    what your phone shows at the same moment. If it is not, stop — §V4 will fail too,
    for this reason rather than its own.

### Block 5 — the vault, network half (~40 min)

This is the new ground. Get a free Steam Web API key from `steamcommunity.com/dev/apikey`
first, and use a test account you do not mind locking out.

18. §V5a–V5g — cheap health checks. These never log in.
19. **§V5i** — confirm no Guard email arrived from V5h. If one did, the two check tiers
    have been confused, and that is a release blocker.
20. §V3e–V3o — IMAP. Skip if you have no reachable inbox; §V4 can run on a TOTP seed
    instead.
21. §V4 — **the first real Steam login this code has ever attempted.** V4a is the
    moment of truth. Report how far it got, not just that it failed: whether Steam was
    asked for the RSA key at all, whether it asked for a Guard code, and whether the
    failure names a rejected password or an unrecognised response. Those three
    distinguish "the account is fine and this code is wrong" from the opposite.
22. §V6 — the vault against the rest of the app, plus G9 and G10.

### Block 6 — macOS specifics

23. §F-macos, M1 through M12. Run after the full plan, not instead of it.

---

## What a failure report needs

Section number, what you expected, what happened.

For §C and §D, add `SessionKit/active.json` and the matching file under `transactions/` —
those two identify the exact point the transaction reached.

For §V, add the section number and the message shown, and **never** the vault blob or
anything you revealed from it.

# SteamSwitch — Redesign Brief (v2)

**Audience:** UI/UX designers taking on a visual + interaction overhaul of SteamSwitch.
**How to read this:** Part A is the design brief — the jobs, flows, states, constraints and
acceptance criteria you design against. Part B is the current-state audit (what's broken today
and why). Part C is the engineering appendix (data model, APIs, storage, file references). Work
from Part A; consult B and C when you need to know what exists. Where B/C name a `.svelte` file
or a Go method, that is context, not a constraint on your design.

> This v2 was rewritten after an independent review pass (two reviewers) of v1. The biggest
> changes: the bulk-import **security model is corrected** (encrypted interchange, not
> "plaintext file then secure-delete"); the **grid-vs-vault architecture is decided** (two
> surfaces, not one); implementation/CSS detail moved out of the design body into Part C; and
> **measurable acceptance criteria** were added. See Part B for the fixed/known defects.

Companion engineering docs: `VAULT.md` (vault design + threat model), `FORK.md` (divergence
from upstream TcNo), `FEATURES.md` (what's built). This brief is the **design source of truth**;
where it needs an engineering fact it states it inline (Part C) rather than sending you to those.

---

# PART A — DESIGN BRIEF

## A1. The product in one page

SteamSwitch is a **small Windows desktop utility** (single resizable window, embedded WebView2)
that lets a person keep several Steam accounts and jump between them in one click. It works by
swapping the login files/registry values Steam uses to remember its last sign-in, while Steam is
closed — so a normal switch **handles no password at all**. That is the product's trust story.

On top of that sits an **optional, opt-in "vault"**: if the user chooses to store credentials
(password, Guard/TOTP seed, email access), the app can run **health checks** on accounts (banned?
token still alive? saved password still valid?), fetch a **Steam Guard code** during a login, and
**hand an account to another machine** as an encrypted file. The vault is encrypted at rest and
gated by an app-lock password.

There are therefore **two modes, and the design must keep them distinct:**

- **Switcher (default, no secrets):** the home surface. Who am I signed in as? Switch to another
  account. Add another Steam login. A two-account user lives entirely here and should never be
  shown vault machinery.
- **Vault (optional layer):** manage stored credentials, see health, import a roster, hand off an
  account. A power user opts into this. It attaches to the *same* accounts by SteamID.

The redesign's job, in priority order (A3), is to make the switcher instantly clear, finish the
half-done visual migration into one coherent language, make "add an account" honest, turn the
vault into a real managed place, and add safe **bulk import** for large rosters.

## A2. Jobs to be done (use these instead of personas)

Design against concrete, timed jobs — each with how often it happens and what it costs when it
fails. These, not demographic personas, decide what earns space.

| Job | Frequency | Failure cost | Drives |
|---|---|---|---|
| **J1 — Switch to another account before I play** | Many times/day | High annoyance; wrong account joins a game | The switcher: one-click, unmistakable current state, fast visible progress |
| **J2 — Know at a glance who I'm signed in as now** | Every time the window opens | Confusion, mis-switch | A strong, truthful "current account" state (A4) |
| **J3 — Add a brand-new Steam login** | Occasional | User thinks the app is broken (today's bug #7) | "Add another Steam login" flow: honest label, real feedback, lands at Steam login |
| **J4 — Check which of my N accounts are healthy/banned** | Weekly (roster keepers) | Wasted time, surprise bans | Vault health at a glance; a health visual language (A9) |
| **J5 — Onboard many accounts from a list I already have** | Monthly / on acquisition | The dangerous moment: dozens of plaintext credentials in motion | Safe bulk import (A7) |
| **J6 — Add / edit one account's stored credentials by hand** | Occasional | Can't manage the vault at all (today's bug #8) | The account editor + an obvious "Add vault entry" (A6) |
| **J7 — Move an account to another machine / a friend** | Rare, deliberate | Credential exposure if done wrong | Encrypted handoff (already exists; align import with it) |

**Design primarily for J1–J3** (the 90%). J4–J7 must be reachable and excellent for the people
who need them, but must **not tax J1–J3**: vault power-user chrome (roster columns, provenance,
bulk actions) lives behind "vault enabled," never on the home switcher by default.

**A note on framing:** several jobs (J4–J7) serve users running many bought/farmed accounts.
Treat this as **authorized multi-account management** — the operator owns or is authorized for the
accounts. If account resale/transfer is an intended, promoted use case, that needs explicit legal
and Steam-ToS review before it's designed as a feature; this brief does not assume it is. Design
the honesty that follows from real credential handling (rate-limit awareness, clear warnings),
regardless.

## A3. Priorities and the v1 cut line

Everything below is wanted; not everything is v1. Proposed ordering — challenge it, but the brief
must ship with *an* order, not twelve equal "must-fixes":

- **P0 (v1 core):** (1) Switch clarity — current-account truth + switch progress/result (J1/J2).
  (2) Coherent list/tile/chrome (finish the migration; fix avatar, menu, hover, back-arrow).
  (3) "Add another Steam login" made honest (J3). (4) Legibility pass — type minima + contrast (A8).
- **P1:** (5) Vault as a real, managed place + the account editor reachable/blank-openable (J6).
  (6) App-lock / unlock UX (A10). (7) Health visual language (A9).
- **P2:** (8) Safe bulk import (J5, A7). (9) Settings IA overhaul (A11). (10) Handoff UI polish (J7).

**Explicitly out of redesign v1** (state so, so designers don't half-touch them): Dota 2 config
copy, the game-shortcut bar, tray menu styling, CLI/`steamswitch://` deep-links, theme *editor*
(the theme *catalog* stays; a full editor is not established as valuable and is out).

## A4. The "current account" truth model (design against real states)

"Show the live account unmistakably" is only safe if the design accounts for the fact that the app
does not always know Steam's ground truth. Specify each state:

- **Signed in as X (Steam closed):** the app's cache says X is current. Show X as current, calmly.
- **Signed in as X (Steam running):** X is live and in use. Strongest "current" treatment;
  switching will need to close Steam (warn).
- **Unknown / no current:** after "Add another Steam login," a failed switch, or a fresh install —
  no account is marked. Show "not signed in" honestly, not a stale last account.
- **Mismatch:** the cache says X but Steam was last used as someone else (external login). The app
  may detect this on refresh. Design a "this may be out of date — refresh" affordance rather than
  asserting a possibly-wrong current.
- **Mid-switch:** transient; see A5.

## A5. Core flows (design these as flows, with their failure branches)

Numbered end-to-end scenarios. Each needs happy path **and** the listed failure branches.

1. **First run, zero accounts.** Explain in one breath what the app does and the no-password
   switch story; one clear primary action to get the first account (detect existing Steam login /
   "Add a Steam login"). No empty grid staring back.
2. **Switch A→B.** Steps: confirm intent → close Steam → save A's login → clear → restore B →
   (optionally) launch Steam as B → "Now signed in as B." Branches: **Steam won't close**;
   **needs admin** (offer elevated retry — backend supports it); **relaunch fails**; **user cancels
   mid-switch**; **B's cached login is missing/corrupt**. Never leave the window unchanged and
   silent.
3. **Add another Steam login (J3).** This does *not* add a stored account — it puts Steam into a
   logged-out state so the user can sign in fresh. Copy must say so *before* the click. Result:
   Steam open at the login screen, with a toast confirming what happened. Branch: if Steam isn't
   relaunched, say "Steam closed — [Launch login]" as an explicit action, don't silently no-op.
4. **Unlock / lock / forgot app-lock (A10).** Entering the vault when locked; locking; and the
   dead-end of a forgotten app-lock (what's recoverable — nothing, by design — and how that's
   communicated before it happens).
5. **Add a vault entry by hand (J6).** Open the editor blank → paste a SteamID64 (or resolve from
   a profile URL/vanity — nice-to-have) → fill what's known, leave the rest → save. Decide whether
   it appears on the switcher ("Show on switcher" toggle; default on for a manual add, off for bulk
   credential import).
6. **Bulk import (J5, A7).** Choose source → parse → **review table** (per-row validity, create vs
   update, grid vs standalone, conflicts) → confirm → progress → summary ("18 added, 2 updated, 1
   skipped: bad SteamID64"). Branches: partial parse failure; conflicts with existing entries;
   interrupted/aborted import.
7. **Health check + what to do next (J4, A9).** Run a check (one / all); show results as states
   (A9); each bad state offers the next step (re-enter password, refresh token via login, etc.).
8. **Guard-code retrieval during a login.** When the vault supplies Guard codes (TOTP seed) or
   reads them from a bound inbox, the login flow surfaces the code / a "enter code" prompt. Design
   the in-login code moment (auto-filled vs manual entry vs "waiting for email").

## A6. Surface requirements (outcomes + states, not pixels)

For each surface: what it must let the user do, and the states it must cover. Visual language is
yours; these are the requirements.

**The account list & tile (Switcher home).**
- Must make **the current account unmistakable** (A4) and every other account **one click to
  switch**. States per tile: default, hover, current (Steam closed), current (Steam running),
  switching (in progress), error/needs-attention (e.g. health flag if vault enabled).
- The avatar must render correctly and **self-contained** at the list's size, clipped, with a
  fallback for missing images and identical clipping for animated avatars. Decorative Steam frames
  are optional and, if kept, must fit the same box (consider frames as detail-view only).
- Scale from **2 to ~500 accounts**: search + tag filter appear when the roster is large or tags
  exist (not a manual density tax); the list stays performant and scannable at N=500.
- Per-account actions (switch, edit vault, tags, export handoff, delete) reachable without clutter
  — consider 1–2 inline actions on hover plus an overflow menu (A6 "context actions").

**Switch experience (the product's core — give it the most attention).**
- A first-class **progress + result** experience for a multi-second operation that closes and
  relaunches Steam: which account you're leaving/joining, the steps as they happen, cancellation
  where safe, "don't close the window" guidance, and a clear success or an error-with-recovery.
  Decide the vehicle (inline on the tile vs action bar vs modal) — but it must be visible and
  truthful, replacing today's invisible button-disable.

**Account detail / preview.**
- Replace today's ugly hover card (Part B #4). Prefer **click/select → detail** (a panel or route)
  over hover cards, which are painful on a dense Windows list; hover, if used at all, is a small
  themed tooltip, never a second window. Detail shows persona, current/last game, health (if vault
  on), tags, and the per-account actions.

**Context actions menu.**
- The single-account action hub. Coherent with the app's language (not the legacy card in Part B
  #5): content-fit width, clear hover, grouped items, destructive actions (Delete) separated and
  confirmed, predictable submenu placement that never runs off-window.

**Keyboard shortcuts.**
- The 1–4 quick-switch (Part B #6) must read as a **shortcut**, not a mystery badge: keycap
  affordance + tooltip ("Press 1 to switch to this account") + a discoverable shortcuts overlay.
  Resolve the tension with reorder/filter/pin: define whether a number binds to *visible order* or
  a *sticky pin*, or drop numeric hints until ordering is stable. Must be visually distinct from
  any count/health badge.

**Vault page (the managed place — J4/J6).**
- A **searchable, filterable, actionable** view of stored accounts (not today's read-only dead-end,
  Part B #8). Default columns: identity, health, **secret presence** (password / TOTP / token /
  email as clear icons), tags. Provenance (source / acquired-at / note) lives in the detail/editor,
  not the default table, until proven needed. Per-row actions: open, edit, run check, export,
  delete. "Check all" and "Import" are buttons on this page. An **"Add vault entry"** button opens
  the editor blank (see below). A **locked** vault shows an unlock gate, and a **first-run** vault
  explains what it stores, that it's encrypted and optional, and how the lock works — never a
  silent empty list.

**Account editor (shared by manual-add, tile-edit, import-review).**
- One security-sensitive form, opened from three doors: Vault "Add entry" (blank), a tile's "Edit
  vault," and bulk-import "edit row." Requirements: **field matrix** — required (SteamID64 only) vs
  optional vs advanced; identity first, then expandable sections for Credentials, Email binding,
  Provenance; **reveal-on-demand** for every stored secret (never shown by default; auto-re-hide,
  A10); clear "stored / not stored" states; validation (SteamID64 format, IMAP host/port); honest
  microcopy about what each field *enables* ("TOTP seed lets the app generate Guard codes"; "email
  binding lets it read the code from your inbox"); and a clear meaning for "save with mostly empty"
  (identity + password is already useful). Include a **"Show on switcher"** toggle (the plain-language
  version of the internal "standalone" flag).

**Settings (A11).**

**App chrome & global nav.**
- The persistent frame on every screen: where the app title / current-location sits, where global
  actions live (lock vault, settings, add account), and **back/forward driven by real nav history**
  (Part B #3 — the back control is absent or truly disabled at the root, never an inert spinner).
  Top-level destinations are a small fixed set: Switcher / Vault / Tools / Settings / About.

## A7. Import & handoff security model (the highest-risk area — design this first)

The headline new capability (J5) is loading many accounts at once, including from a list an
automation/agent produces. Those accounts carry **plaintext passwords, Guard/TOTP seeds, and email
credentials**. The naive model — "drop a `accounts.json`/SQLite of secrets in a folder, import it,
then securely delete the file" — is **unsafe on Windows and must not be the recommended path.**
Why it fails: SSDs don't reliably overwrite (wear-leveling/TRIM/controller remapping); Volume
Shadow Copies, OneDrive/Dropbox/Drive version history, the Search index, and Defender history all
retain copies; and the agent's own transcript/workspace holds another copy. A UI that says
"securely deleted ✓" teaches a false sense of safety.

**The model to design instead (in priority of preference):**

- **A. Canonical — encrypted multi-account interchange (agents + machine moves).** Reuse the same
  crypto and threat model as the existing single-account **handoff**: a passphrase-sealed bundle.
  The agent/script populates a documented **plaintext payload in memory**, the tooling **encrypts**
  it, and the user imports **ciphertext**. Plaintext credentials never exist on disk as a supported
  format. The schema/template we publish for agents describes the **pre-encryption payload plus the
  required encrypt step** — not a file of passwords to leave next to the exe.
- **B. Human on-ramp — in-memory entry.** Paste / CSV-paste / typed form into a **review buffer
  that is never written to disk as a plaintext file**. Optional "save as locked incomplete entries"
  writes into the encrypted vault, not to `Downloads`.
- **C. Agent without disk — CLI/stdin.** For automation, accept the payload over **stdin / a pipe**
  so secrets never touch an OS-indexed path. The in-app import is the UI twin of this.
- **D. Plaintext file — legacy escape hatch only, never promoted.** If a user *must* import a
  plaintext file they already have: unlock vault → open under exclusive access → parse → encrypt
  into the vault in one batch → **best-effort** overwrite+unlink, with **honest copy**: "Removal is
  best-effort. We can't guarantee erasure on SSDs or cloud-synced folders — delete any other copies
  yourself." Never "securely deleted ✓." **No watched-folder auto-import** (that's how secrets get
  re-indexed and re-synced): explicit pick + one-shot only.

**Integrity / atomicity requirements** (the import writes into one re-encrypted vault blob):
- **All-or-summary, never partial-silent.** Define behavior on partial parse failure, an
  interrupted/crashing write, and abort. The vault must never be left half-written or corrupt; a
  failed import leaves the prior state intact.
- **Per-field conflict policy, specified before the UI.** For a SteamID already in the vault: does
  import overwrite a present password, fill only empty fields, or skip? Decide per field (password,
  TOTP, email, token) and per row (create vs update vs skip), and surface it in the review table.
- **Requires the vault unlocked** (it must be, to write) and gated by the app-lock.

**Least privilege for stored email creds:** guide users toward **mailbox-specific app passwords /
restricted tokens**, never their primary mailbox password. The editor/import copy should say so.

**Trust boundary for "hand an agent your roster":** the brief mandates **local, trusted,
non-retaining automation only.** Secrets go agent-memory → encrypted bundle → vault; the design
must never imply a hosted service receives them. (See A12 — nothing here is a product cloud.)

## A8. Typography & legibility

Today's root is 12px and help text renders at ~8–10px in low contrast (Part B #10). The fix is a
**semantic type system**, not a global root bump.

- Respect the document/user default at the root and **rebuild semantic type tokens** with
  **role minima**: body/UI text **≥13–14px effective**, secondary/help **≥12px**, and **nothing
  <12px**. Don't globally rewrite 12px→16px and hope the rem tree rescales — that reflows every
  inherited surface and gets "fixed" with more fractional rems.
- **Contrast is half the problem.** Define text-color roles (primary/secondary/muted/disabled) that
  meet **WCAG AA (≥4.5:1 body)** on every theme, and **clamp themes** so a user theme can't set a
  text role below its contrast target against its own surface. Small + low-contrast is the current
  failure; fix both.
- **Question the typeface.** Montserrat is a display face; audit it as dense UI/body text at small
  sizes under Windows ClearType and switch if it hurts legibility. The brief should not assume
  Montserrat survives.
- **Must hold at Windows scaling 125% / 150% / 200%** and on a small laptop — verify by resizing
  and text-zoom, not by inspecting a nominal px. Pair larger minima with **shorter copy / fewer
  always-visible hints / progressive "learn more"** so bigger help text doesn't wrap into noise.
- Keep the scale short for a ~1000px-wide utility (e.g. title / body / secondary / meta / mono +
  keycap); no marketing "display" tier.

## A9. Health & trust visual language

Roster keepers need "health at a glance," which needs designed states. Specify a consistent set of
**account health states** with icon + color + severity + a plain-language label and a next action:
- **OK / live**, **Never checked**, **Checking…**, **Token expired** (needs a login to refresh),
  **Password wrong** (saved password rejected), **Guard/email unreachable** (can't fetch a code),
  **Limited** (rate-limited / restricted), **Banned/VAC** (terminal, loud).
Severity ordering and how a tile/row summarizes multiple signals into one badge must be defined.
This badge must be visually distinct from the keyboard-shortcut keycap (A6).

## A10. App-lock & vault encryption UX

The vault is encrypted and gated by an app-lock password; today this is a slogan, not a designed
flow. Specify: whether the app-lock is **mandatory or optional** to enable the vault; **set /
change / forgot** password flows (and communicate, *before* they commit, that a forgotten app-lock
means unrecoverable vault data — that's the encryption working as intended); **auto-lock** rules
(on minimize? after N minutes idle? on sleep?); **reveal** behavior for stored secrets (momentary,
auto-re-hide after a few seconds or on blur); and **clipboard policy** (if "copy password" exists,
clear the clipboard after a short timeout and say so). None of the vault is usable without these.

## A11. Settings

Today: one flat scrolling column of borrowed-styled sections — the "still looks like TcNo" tell
(Part B #11). Requirements:
- **Give settings navigation and grouping** (two-pane categories, or tabs): Appearance, Steam,
  Game Modules, Vault & Security, System, Language. Each is a designed page, not a stacked section.
- **Surface trust/security settings prominently** (app-lock, vault encryption, offline mode) — not
  buried among refresh intervals.
- **A consistent control vocabulary** (toggles, selects, sliders, inputs, "danger zone") designed
  once and applied; retire the inherited look.
- Keep the genuinely Steam-specific controls (account display, tray behavior, process management,
  run-as-admin) — redesign them, don't drop them. Appearance keeps the **theme catalog**; a theme
  *editor* is out of v1 scope.

## A12. Networking honesty (so designers don't invent the wrong "cloud" UI)

State plainly in any related UI: SteamSwitch makes **no product-owned network calls** — no
telemetry, no update check, no feedback/crash upload, no account server. The only outbound traffic
is (a) Valve's own APIs for health/login and (b) the **user's own** IMAP server or mailbox-API that
*they* configured for reading Guard codes. **Offline mode** disables (a) and (b) at the transport
layer, and the UI must reflect that state (health/login unavailable), not error. A configured
mailbox-API is the user's integration, never "brokered by SteamSwitch."

## A13. Measurable acceptance criteria (definition of done)

"Feels coherent" isn't testable. Each redesigned area needs criteria like these — expand per
surface:
- **Legibility:** at 125% Windows scaling and the minimum supported window size, all help text is
  readable without clipping; no UI text renders <12px; body/UI text meets ≥4.5:1 contrast on every
  shipped theme.
- **Switch (J1):** from window open, a first-time user completes a switch **without documentation**;
  progress is visible within 200ms of the click; success/error state is unambiguous.
- **Add-login (J3):** after the action, the user can tell what happened and reach the Steam login
  in one step; no silent no-op state exists.
- **Vault manageability (J6):** a user can add, edit, reveal, and delete a vault entry entirely from
  the Vault page, and open a blank editor to add an account that has no home tile.
- **Import (J5):** importing 20 accounts leaves **zero plaintext credential file on disk as a
  supported outcome**; conflicts are shown per-row before commit; a partial failure never corrupts
  the vault; the summary accounts for every input row.
- **States:** every surface defines empty / loading / locked / error / offline.
- **A11y:** keyboard nav order and focus states defined; no interactive control is `aria-hidden`;
  shortcuts discoverable via an overlay.

Also fix, as hard boundaries designers can rely on: **minimum window size** and resize behavior;
**expected roster sizes** (2 / 10 / 50 / 500) each surface must handle; **performance targets** for
unlock, search, preview, import, and switch feedback; **localization expansion tolerance** (strings
grow ~30% in other locales — this app is heavily localized).

## A14. Genuinely open questions (decide with the designers)

These are actually open (unlike grid-vs-vault, which A1/A6 decide: two surfaces):
- Hover-tooltip vs click-to-detail for account preview — recommendation is click-to-detail; confirm.
- Keyboard shortcut binding model (visible order vs sticky pins) and whether to extend past 1–4.
- Whether decorative avatar frames earn space in the compact list or are detail-view only.
- The exact conflict-resolution defaults for import (overwrite vs fill-empty vs skip, per field).
- Whether the app-lock is mandatory to enable the vault, or optional with a clear warning.

---

# PART B — CURRENT-STATE AUDIT (defects and their causes)

Real defects found running the Windows build. The two "problems" that are **not** design problems —
the startup GitHub-update toast (an engineering regression, now fixed) and "we need a brief" — are
excluded from the design scope; the fix note is kept here for the record.

| Area | Symptom | Verified cause |
|---|---|---|
| Avatar | Doesn't fit the row | `SteamAccountAvatar.svelte` ships no CSS of its own; the width/height/`object-fit` rules exist only under the old `label.acc` selector the new `.tile` never uses → the `<img>` renders at intrinsic 64px+ inside a 36px box with no clip. A frame overlay adds `scale(1.22)`. |
| Back-arrow | Spins, does nothing on home | `TitleBar.svelte backClick()` runs a random-axis 360° easter-egg spin on the `home` route and never navigates; the button is only disabled while a modal is open (`backDisabled = !!$activeModal`), never based on nav history, so it looks active-but-inert at the root. |
| Avatar hover | Ugly mismatched-size popup | Hover injects the full legacy Steam **miniprofile** HTML (`miniProfileHover.ts`): a ~328px card with an 80px avatar + 96px frame, styled by `miniprofile.scss`. A carried-over Steam blob, not a compact preview. |
| "..." menu | Looks bad, clashes | Trigger is a light hairline button; the menu is rendered by `ContextMenu.svelte` but styled entirely by legacy `context_menu.scss` — a fixed 15em dark card with a `10px 10px` offset drop-shadow and accent slide-in bars. |
| "1" badge | Unexplained | `kbd.tile__hint` = the tile's 1-based index (first 4 tiles); pressing 1–4 switches to that account. `aria-hidden`, no affordance explaining it. |
| "Add new" | Seems to do nothing | `SteamAddNew()` → `SwapToAccount("")` clears the active login (the logout) but **only relaunches Steam if `AutoStart` is on**, with no toast either way → button disables briefly, then nothing visible. |
| Vault in Tools | Can't open/manage; no manual add | Tools shows three loose actions; "View all" is a **read-only** dead-end modal; the editor (`VaultEntryModalBody`) opens only from a home-tile right-click, always seeded from that tile's SteamID64 — **nothing opens it blank**. |
| Fonts | Help text too small | Root `font-size: 12px`; help text at `0.7–0.85rem` ≈ 8.4–10.2px, scattered per-component with no shared token, paired with low-contrast muted colors. |
| Settings | Looks like TcNo | Single flat stacked column of sections; `styles/Settings.scss` still carries upstream selectors (`.acc_list_actionbar`, `.btn_app_login`, `div.form-text`). |
| *(fixed)* Update toast | "Couldn't reach GitHub" on launch | The disabled-update sentinel was handled as a connectivity failure. **Fixed** in `internal/updatecheck/updatecheck.go` — returns silently. There is **no** update surface in this product; do not design one. |

**Key enabling fact for the vault work:** the backend already supports creating an entry from just
a SteamID64 (`VaultService.SaveEntry → Put()` creates when the ID isn't found). So "add manually"
is a missing **button + blank-open**, not a missing feature. **But** "so it's mostly UI" understates
bulk import: that is primarily a data-integrity, cryptographic-boundary, and failure-recovery
feature (A7) with a UI on top.

---

# PART C — ENGINEERING APPENDIX (data, APIs, storage)

Context for feasibility. File references are current at writing; verify against the tree.

**Main list surfaces:** `pages/Accounts.svelte`, `components/AccountTile.svelte`,
`components/SteamAccountAvatar.svelte`, `components/AccountTagBubbles.svelte`,
`components/TitleBar.svelte`, `components/ContextMenu.svelte` + `styles/context_menu.scss`,
`lib/steam/accountMenu.ts`. Switch backend: `SteamService.SteamAddNew` / `SwapToAccount`
(`internal/steam/switcher.go`); "Launch Steam" uses `LaunchSteamOnly` (always launches).

**Vault surfaces:** `pages/Tools.svelte`, `components/modals/VaultOverviewModalBody.svelte`
(read-only), `VaultEntryModalBody.svelte` (editor), `VaultImportModalBody.svelte` (handoff),
`stores/vault.ts`.

**Vault data model (`internal/vault`).** Persisted `Entry` fields (json tags):
`steamId64` (required), `accountName`, `label`, `standalone` (record-only / off-grid),
`password`, `sharedSecret` (TOTP seed), `identitySecret`, `refreshToken`, `guardData`,
`tokenExpiresAt`, `email` (an `EmailBinding`), `source`, `acquiredAt`, `secretNote`,
`health` (a `HealthReport`), `nextEligibleAt`, `checkFailures`, `sessionSkips`, `egressId`,
`updatedAt`. `EmailBinding` = `address` + `source` (`none`|`imap`|`mailbox-api`|`manual`) + optional
`IMAPCreds` (`host`, `port` [default 993], `user`, `password`, `useTls` [default true],
`purgeConsumed`) or `MailboxRef` (`baseUrl`, `token`, `mailboxId`).

**Writable input = `Draft`** (pointer fields = "leave alone" vs "set"): `steamId64` plus optional
`accountName`, `label`, `standalone`, `password`, `sharedSecret`, `identitySecret`,
`emailAddress`/`emailSource`/IMAP fields/mailbox fields, `source`, `acquiredAt`, `secretNote`, and
`clearPassword`/`clearTotp`/`clearToken` flags. **`Draft` has no session-material inputs**
(`refreshToken`/`guardData`/`tokenExpiresAt` are written only by the app's own login/check paths).
So an import populates identity + credentials + email + provenance; session/health fill in later.

**Persistence:** one AES-256-GCM encrypted document at `<DataRoot>/Vault/secrets.ssvault`, sealed
under a subkey of the app-lock master key; only `updatedAt` is cleartext. Every write goes through
`mutate` under a write lock, deep-copies the whole doc, and re-encrypts the entire blob (so a batch
import should wrap repeated `Put` in **one** `mutate` — one re-encrypt for N accounts). Delete
rewrites the whole blob (no tombstone). **`ids.json`** (unencrypted, `LoginCache/<platform>/`) is
**separate** — it carries per-account tags and last-used for the basic engine.

> **Correction (was wrong in v2).** This section previously said an imported account reaches the
> switcher "only if `ids.json` is also written". That is not how the Steam grid works.
> `buildSteamListContext` builds the roster from `ParseLoginUsers(<root>/config/loginusers.vdf)`
> merged with `order.json` (`internal/steam/accounts_list.go`, `order.go`); `ids.json` never enters
> it. The Steam switcher is therefore **exactly the set of accounts Steam itself has signed into on
> this machine**, and nothing this app writes can add a row for an account that has not.
>
> Consequences, all of which the import design now follows: every bulk-imported entry is
> necessarily `standalone`; there is **no "Show on switcher" toggle** to offer on an import (it
> would be a promise the app cannot keep); and the only honest route from a vault-only entry onto
> the grid is a real sign-in, which the Vault row's **"Sign in on this machine"** action starts by
> running the existing add-login flow (`SteamService.SteamAddNew`).

**VaultService methods (Wails-bound, most require app-lock unlocked):** `GetStatus`, `ListEntries`,
`GetEntry`, `SaveEntry(Draft)` (create/update), `DeleteEntry`, `RevealField`, `HasEntry`,
`GetGuardCode`, `SubmitManualGuardCode`, `TestEmailBinding`, `RunQuickCheck(All)`, `RunDeepCheck`,
`RunSessionCheck`, `GetTokenDetails`, `DiscoverIMAP`, `IsRateLimited`, and the handoff set
(`ExportHandoff`, `ListHandoffBundles`, `InspectHandoffBundle`, `AcceptHandoffBundle`,
`GetHandoffLog`, `GetHandoffFolder`).

**Bulk import (A7) — built.** `internal/vault/roster.go` and `roster_import.go` add the
`.ssroster` interchange: the same Argon2id + AES-GCM envelope as handoff under its own AAD, so
the two formats can never be opened as one another. Four intakes converge on one review-commit
path — `PrepareRosterFromBundle` (sealed file), `PrepareRosterFromText` (paste, JSON or CSV),
`PrepareRosterFromPlaintextFile` (legacy escape hatch), and the CLI's `--seal-roster`, which reads
a plaintext payload **and its passphrase** on stdin and writes ciphertext, so an automation never
puts plaintext credentials on disk. Then `RepriceRosterImport` / `CommitRosterImport`.

Three properties worth keeping if this is ever reworked:
- **The parsed roster never crosses the bindings.** It lives in a Go-side session buffer; the UI
  receives per-field *presence* and outcome, never a value. The buffer is dropped on commit, on
  cancel, after 15 minutes, and — via `DropCache` — whenever the app locks.
- **One `mutate` for the whole batch**, so N accounts cost one re-encryption and a failure leaves
  the prior blob intact rather than half-written.
- **Fill-empty-only is the default**, with per-row `overwrite` / `skip`. The email binding is
  kept or replaced whole, never merged field by field.

**Fork guardrails (do not regress):** Steam-only; Windows is the only OS where switching works;
no telemetry/update/feedback/crash endpoints; no links to tcno.co/wiki/Discord/donations; handoff &
import are **local user-moved files**, never a network transfer; keep the TroubleChute/upstream
attribution in About/License.

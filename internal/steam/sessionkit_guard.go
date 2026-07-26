package steam

import (
	"errors"

	"steamswitch/internal/sessionkit"
)

// The Session Kit's leave rule, enforced where the swap happens rather than in the UI.
//
// `SessionKitService.SwitchTo` is documented as "the single entry point the account tiles
// call", and that was accurate — but it was never the only way to switch. The tray menu, a
// `steamswitch://` URL, a desktop shortcut and `--run-appid=` all reach `SwapToAccount`
// directly, and none of them passes through the engine.
//
// That mattered because the "a kit is live, leaving it is your decision" rule lived entirely
// in `Accounts.svelte`. Switching away from a shared account from the tray therefore left the
// other person's files under this machine's kit, with a journal still claiming the transaction
// was in progress — precisely the state the recovery modal exists to clean up, reached as a
// matter of routine rather than after a crash.
//
// The gate is the engine's, not this package's, and it holds the transaction lock across the
// whole swap. Checking the status and then releasing the lock would have left a window in
// which a bare swap and a tile switch could both be closing Steam and rewriting
// `loginusers.vdf`: an unjournaled login change interleaved with a journaled one. The engine
// does not call `SwapToAccount` (see `Lifecycle`, which drives the individual steps), so
// running the swap under its lock cannot deadlock against the engine's own use.
//
// It refuses rather than trying to do the right thing silently. There is no correct default:
// "put their setup back" and "keep mine" is a decision about someone else's files, and a tray
// click is not the place to guess at it. The error names the window as where to finish.

// ErrKitLeaveRequired is returned when a swap would abandon a live Session Kit.
//
// The message is an i18n key, following the convention in this package.
var ErrKitLeaveRequired = sessionkit.ErrLeaveRequired

// ErrKitRecoveryRequired is returned when an interrupted transaction has not been resolved.
var ErrKitRecoveryRequired = sessionkit.ErrRecoveryRequired

// ErrKitEngineUnavailable is returned when the engine could not be built at startup.
//
// This fails *closed*. `main` constructs the engine before both the headless dispatch and the
// GUI, and treats a construction failure as non-fatal so the app still opens — but a failure
// means the journal could not be read, and an unreadable journal is exactly the case where a
// bare swap could strand a live transaction. Switching stops until it is available.
var ErrKitEngineUnavailable = errors.New("Toast_Kit_NotReady")

// guardedSwap runs `swap` only if the Session Kit's state permits a bare swap to `target`,
// with the engine's transaction lock held throughout.
func guardedSwap(targetSteamID64 string, swap func() error) error {
	e := activeKitEngine()
	if e == nil {
		return ErrKitEngineUnavailable
	}
	return e.RunUnjournaledSwap(sessionkit.AccountRef{SteamID64: targetSteamID64}, swap)
}

// swapPermitted is the read-only form, for callers that decide *not* to swap because the
// target is already signed in and would otherwise skip the gate entirely.
//
// `LoginAndLaunchGame` and `shortcuts.RunShortcut` both short-circuit when `loginusers.vdf`
// already names the requested account, and then launch. For a legitimately active kit that is
// correct and intended. For an interrupted or externally-changed transaction it is not: the
// files are in a state the engine has not resolved, and launching Steam on top of them is how
// a recoverable situation stops being recoverable.
func swapPermitted(targetSteamID64 string) error {
	return guardedSwap(targetSteamID64, func() error { return nil })
}

// SwapPermitted is `swapPermitted` for callers outside this package — `internal/shortcuts`
// makes the same already-signed-in decision and needs the same gate.
func SwapPermitted(targetSteamID64 string) error { return swapPermitted(targetSteamID64) }

// RestorePermitted reports whether restoring a Steam backup over the live install is safe.
//
// Stricter than a swap: there is no "same account" exemption, because a restore overwrites
// `config/` and `userdata/` wholesale. Those are the trees a kit is applied to and the ones
// its snapshot hashes describe, so restoring over a live kit silently invalidates both — even
// for the account the kit is on, which is precisely the account most likely to be affected.
func RestorePermitted() error {
	e := activeKitEngine()
	if e == nil {
		return ErrKitEngineUnavailable
	}
	st, err := e.Status()
	if err != nil {
		return ErrKitRecoveryRequired
	}
	if st.Kind != sessionkit.RecoveryNone {
		return errors.New("Toast_Kit_RestoreBlockedByKit")
	}
	return nil
}

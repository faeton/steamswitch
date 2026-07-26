package steam

import (
	"errors"
	"log/slog"

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
// whole operation. Checking the status and then releasing the lock would leave a window in
// which a bare swap and a tile switch could both be closing Steam and rewriting
// `loginusers.vdf`: an unjournaled login change interleaved with a journaled one. The engine
// does not call `SwapToAccount` (see `Lifecycle`, which drives the individual steps), so
// running the swap under its lock cannot deadlock against the engine's own use.
//
// It refuses rather than trying to do the right thing silently. There is no correct default:
// "put their setup back" and "keep mine" is a decision about someone else's files, and a tray
// click is not the place to guess at it. The errors below name the window as where to finish.

// These are the outside-window wordings.
//
// The engine's own sentinels say "finish the recovery" and assume the reader is looking at the
// window that offers it. These paths — tray, shortcut, protocol handler, CLI — have no such
// window in front of them, so the message has to say where to go. They wrap the engine errors
// so `errors.Is` still identifies the condition, and `Error()` returns the key the tray and
// CLI will translate.
var (
	// ErrKitLeaveRequired is returned when a swap would abandon a live Session Kit.
	ErrKitLeaveRequired = outsideWindowError{
		key:  "Toast_Kit_LeaveRequiredOutsideWindow",
		wrap: sessionkit.ErrLeaveRequired,
	}
	// ErrKitRecoveryRequired is returned when an interrupted transaction stands.
	ErrKitRecoveryRequired = outsideWindowError{
		key:  "Toast_Kit_RecoveryRequiredOutsideWindow",
		wrap: sessionkit.ErrRecoveryRequired,
	}
	// ErrKitRestoreBlocked is returned when any kit state forbids overwriting the live tree.
	ErrKitRestoreBlocked = outsideWindowError{
		key:  "Toast_Kit_RestoreBlockedByKit",
		wrap: sessionkit.ErrNotSettled,
	}
)

// outsideWindowError carries an i18n key of its own while remaining `errors.Is`-comparable
// with the engine sentinel it stands for.
type outsideWindowError struct {
	key  string
	wrap error
}

func (e outsideWindowError) Error() string { return e.key }
func (e outsideWindowError) Unwrap() error { return e.wrap }

// ErrKitEngineUnavailable is returned when the engine could not be built *and* something was
// left on disk, so whether a transaction is live cannot be determined.
var ErrKitEngineUnavailable = errors.New("Toast_Kit_NotReady")

// engineOrSafeWithout returns the engine, or reports whether proceeding without one is safe.
//
// `sessionkit.New` only resolves the data root and creates a directory — it never reads the
// journal — so a construction failure means the store is unavailable, not that a transaction
// is in progress. Refusing every switch on that basis would disable the entire product for
// someone who has never marked an account shared, because a subdirectory could not be made.
//
// So the fallback is a stat, not a guess: if no transaction pointer exists on disk, no kit can
// be live and a bare swap is safe. If one exists, or the answer cannot be read, refuse —
// that is the case where a swap could strand a real transaction.
func engineOrSafeWithout() (*sessionkit.Engine, error) {
	if e := activeKitEngine(); e != nil {
		return e, nil
	}
	active, err := sessionkit.ActivePointerExists()
	if err != nil {
		steamLog.Warn("no session kit engine and the transaction pointer is unreadable", slog.Any("err", err))
		return nil, ErrKitEngineUnavailable
	}
	if active {
		steamLog.Warn("no session kit engine but a transaction pointer exists; refusing")
		return nil, ErrKitEngineUnavailable
	}
	return nil, nil
}

// guardedSwap runs `swap` only if the Session Kit's state permits a bare swap to `target`,
// with the engine's transaction lock held throughout.
func guardedSwap(targetSteamID64 string, swap func() error) error {
	e, err := engineOrSafeWithout()
	if err != nil {
		return err
	}
	if e == nil {
		return swap()
	}
	err = e.RunUnjournaledSwap(sessionkit.AccountRef{SteamID64: targetSteamID64}, swap)
	return asOutsideWindow(err)
}

// guardedWhileSettled runs `fn` under the transaction lock, and only when no kit state at all
// stands. For operations that rewrite the files a transaction tracks rather than just the
// login — see `Engine.RunWhileSettled`.
func guardedWhileSettled(fn func() error) error {
	e, err := engineOrSafeWithout()
	if err != nil {
		return err
	}
	if e == nil {
		return fn()
	}
	return asOutsideWindow(e.RunWhileSettled(fn))
}

// asOutsideWindow re-labels an engine refusal with the wording for a surface that has no
// window in front of it. Anything else — including the wrapped operation's own error — passes
// through untouched.
func asOutsideWindow(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sessionkit.ErrLeaveRequired):
		return ErrKitLeaveRequired
	case errors.Is(err, sessionkit.ErrNotSettled):
		return ErrKitRestoreBlocked
	case errors.Is(err, sessionkit.ErrRecoveryRequired), errors.Is(err, sessionkit.ErrExternalChange):
		return ErrKitRecoveryRequired
	default:
		return err
	}
}

// SwapPermitted runs `run` only if a bare swap to `targetSteamID64` would have been allowed,
// with the lock held for the duration.
//
// `LoginAndLaunchGame` and `shortcuts.RunShortcut` both short-circuit when `loginusers.vdf`
// already names the requested account, and then launch. For a legitimately active kit that is
// correct and intended. For an interrupted or externally-changed transaction it is not: the
// files are in a state the engine has not resolved, and launching Steam on top of them is how
// a recoverable situation stops being recoverable.
//
// The launch runs *inside* the gate rather than after a check, so a transaction cannot start
// between the two.
func SwapPermitted(targetSteamID64 string, run func() error) error {
	return guardedSwap(targetSteamID64, run)
}

// RestorePermitted runs `restore` only while nothing is outstanding.
//
// Stricter than a swap, with no same-account exemption, because a restore overwrites
// `config/` and `userdata/` wholesale. Those are the trees a kit is applied to and the ones
// its snapshot hashes describe, so restoring over a live kit silently invalidates both — even
// for the account the kit is on, which is the account most likely to be affected.
//
// Refusing during an interrupted transaction is the uncomfortable case, because a restore is
// plausibly what someone reaches for when things are broken. Resolving the transaction is
// still the right first step: it is what puts the other person's files back, and it is the one
// operation that knows which files those are. Restoring first would bury that under a
// wholesale overwrite and leave a journal describing a tree that no longer exists.
func RestorePermitted(restore func() error) error {
	return guardedWhileSettled(restore)
}

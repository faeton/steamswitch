package steam

import (
	"errors"
	"log/slog"
	"strings"

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
// So the guard sits at the head of `SwapToAccount`. The engine does not call that function
// (see `Lifecycle`, which drives the individual steps instead), so guarding it constrains only
// the bypass paths and cannot deadlock the engine's own use.
//
// It refuses rather than trying to do the right thing silently. There is no correct default:
// "put their setup back" and "keep mine" are a decision about someone else's files, and a tray
// click is not the place to guess at it. The error names the window as where to finish.

// ErrKitLeaveRequired is returned when a swap would abandon a live Session Kit.
//
// The message is an i18n key, following the convention in this package.
var ErrKitLeaveRequired = errors.New("Toast_Kit_LeaveRequiredOutsideWindow")

// ErrKitRecoveryRequired is returned when an interrupted transaction has not been resolved.
var ErrKitRecoveryRequired = errors.New("Toast_Kit_RecoveryRequiredOutsideWindow")

// requireNoActiveKit refuses a bare swap that would strand a Session Kit.
//
// Fails *open* on every "I cannot tell" case — no engine built yet, an unreadable journal —
// because this runs on the path that switching depends on, and a guard that blocks every
// switch whenever the journal is momentarily unreadable would be worse than the hole it
// closes. The engine itself is the authority once it exists; this is a second gate, not the
// first one.
func requireNoActiveKit(targetSteamID64 string) error {
	e := activeKitEngine()
	if e == nil {
		// Headless dispatch builds no engine. Nothing has applied a kit in this process, and
		// a kit left over from a previous run is caught by the status check below only when
		// the GUI is running — headless swaps are documented as kit-unaware.
		return nil
	}
	st, err := e.Status()
	if err != nil {
		steamLog.Warn("session kit status unavailable; allowing swap", slog.Any("err", err))
		return nil
	}
	return swapAllowedBy(st, targetSteamID64)
}

// swapAllowedBy is the rule itself, split out from the engine plumbing so it can be tested
// against every state without building a transaction.
func swapAllowedBy(st sessionkit.RecoveryState, targetSteamID64 string) error {
	switch st.Kind {
	case sessionkit.RecoveryInterrupted, sessionkit.RecoveryExternalChange:
		return ErrKitRecoveryRequired
	case sessionkit.RecoveryKitActive:
		// Re-selecting the account the kit is already on is not leaving it. Shortcuts do this
		// routinely — "log in as X and launch the game" while already on X.
		if strings.EqualFold(strings.TrimSpace(st.TargetSteamID64), strings.TrimSpace(targetSteamID64)) {
			return nil
		}
		return ErrKitLeaveRequired
	default:
		return nil
	}
}

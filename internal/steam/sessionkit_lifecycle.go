package steam

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"steamswitch/internal/platform"
	"steamswitch/internal/sessionkit"
	"steamswitch/internal/stability"
	"steamswitch/internal/stats"
	"steamswitch/internal/tray"
)

// Lifecycle is the Steam half of a session-kit transaction (sessionkit.Lifecycle).
//
// It exists because `SwapToAccount` is one indivisible call: it closes Steam, writes the
// login, records the switch and launches, and it short-circuits when the target is already
// signed in. A kit has to be applied *between* "Steam is closed" and "the login is written",
// and the short circuit would skip the close entirely — so the steps are re-exposed here
// individually rather than the engine calling the monolith.
//
// The methods deliberately do not take `SteamService.mu`. The engine holds its own
// transaction-wide lock for the whole sequence, and reaching back for the service lock from
// inside it would invert the ordering that `SteamService` methods use when they call *into*
// the engine. See the lock hierarchy note on `SessionKitService`.
type Lifecycle struct{}

var _ sessionkit.Lifecycle = Lifecycle{}

// resolveRoot repeats what SwapToAccount does at its head. It is cheap (three small JSON/VDF
// reads) and re-resolving per step keeps the adapter stateless, so a settings change between
// two steps of a long transaction cannot leave it pointing at a stale install.
func (Lifecycle) resolveRoot() (string, Settings, error) {
	st, err := LoadSettings()
	if err != nil {
		return "", Settings{}, err
	}
	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		return "", Settings{}, err
	}
	app, err := platform.LoadAppSettings(exeDir)
	if err != nil {
		return "", Settings{}, err
	}
	raw, err := platform.LoadPlatformsJSON(exeDir)
	if err != nil {
		return "", Settings{}, err
	}
	root, err := ResolveInstallFolder(exeDir, st, app, raw)
	if err != nil {
		return "", Settings{}, err
	}
	if root == "" {
		return "", Settings{}, fmt.Errorf("steam install folder not found")
	}
	return root, st, nil
}

func (l Lifecycle) SteamRoot() (string, error) {
	root, _, err := l.resolveRoot()
	return root, err
}

// CurrentAccount reads the account Steam would log in as right now.
//
// A missing or unparseable loginusers.vdf is reported as "no current account" rather than an
// error: a first run with no saved session is normal, and the engine only uses this value to
// label the transaction's origin.
func (l Lifecycle) CurrentAccount() (sessionkit.AccountRef, error) {
	root, _, err := l.resolveRoot()
	if err != nil {
		return sessionkit.AccountRef{}, err
	}
	users, err := ParseLoginUsers(LoginUsersPath(root))
	if err != nil {
		return sessionkit.AccountRef{}, nil
	}
	return sessionkit.AccountRef{SteamID64: strings.TrimSpace(ActiveSessionSteamID64(users))}, nil
}

// CloseSteam terminates Steam and its helpers.
//
// `ErrIfCannotKill` runs first so a permission problem surfaces as a "needs admin" error
// before anything has been killed, which is what the elevated-restart prompt keys off.
func (l Lifecycle) CloseSteam(ctx context.Context) error {
	// RunningProcesses cannot express "I don't know" — it returns a bare []string, and the
	// engine reads empty as "nothing is holding the files". Refuse here, where an error can
	// still be returned, rather than letting the engine act on a false all-clear.
	if err := requireProcessInspection(); err != nil {
		return err
	}
	_, st, err := l.resolveRoot()
	if err != nil {
		return err
	}
	names := backend.ProcessNames()
	if err := backend.CanClose(names, st.ClosingMethod); err != nil {
		return err
	}
	if err := backend.Close(names, st.ClosingMethod); err != nil {
		// Matches SwapToAccount: a kill that reports an error may still have worked, so the
		// authority is RunningProcesses below, not this return value.
		steamLog.Warn("kill steam processes", slog.Any("err", err))
	}
	return nil
}

// RunningProcesses names anything still holding the files. The engine treats a non-empty
// result as a hard stop, so this must only report processes that actually lock config trees.
func (Lifecycle) RunningProcesses() []string { return runningProcessNames() }

// WriteLogin points Steam at an account without launching it.
//
// personaState follows the SwapToAccount convention: -1 means "use the configured override",
// anything below that skips the localconfig edit entirely.
func (l Lifecycle) WriteLogin(ctx context.Context, account sessionkit.AccountRef, personaState int) error {
	root, st, err := l.resolveRoot()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(account.SteamID64)
	if err := writeLoginUsersAndRegistry(root, id, st.SteamRememberPassword); err != nil {
		return err
	}
	if err := setShowSteamSwitcher(root, st.ShowSteamSwitcher); err != nil {
		steamLog.Warn("config.vdf AlwaysShowUserChooser", slog.Any("err", err))
	}

	pS := personaState
	if pS == -1 {
		pS = st.SteamOverrideState
	}
	if pS >= 0 && id != "" {
		platform.EmitActionBarStatusI18n("Status_ActionBar_UpdatingSteamPersona")
		if err := setPersonaStateLocalConfig(root, id, pS); err != nil {
			steamLog.Warn("localconfig ePersonaState", slog.Any("err", err))
		}
	}
	return nil
}

// LaunchSteam starts Steam, honouring the same AutoStart opt-out as the classic swap: a user
// who has turned auto-start off wants the login swapped and nothing else launched.
func (l Lifecycle) LaunchSteam(ctx context.Context) error {
	root, st, err := l.resolveRoot()
	if err != nil {
		return err
	}
	if !st.AutoStart {
		return nil
	}
	return backend.Launch(root, buildSteamArgs(st, nil), launchOpts(st, st.RunAsAdmin))
}

// OnSwitchSucceeded runs the bookkeeping from the tail of SwapToAccount.
//
// It is deliberately the last thing the engine calls, and only on a committed transaction:
// counting a switch that later failed would inflate the statistics and, worse, feed
// `stability.OnSuccessfulSwitch` a success it did not have.
func (Lifecycle) OnSwitchSucceeded(account sessionkit.AccountRef) {
	id := strings.TrimSpace(account.SteamID64)
	RecordTrayRecentAfterSwap(id)
	stability.OnSuccessfulSwitch("Steam")
	if err := stats.IncrementSwitches("Steam"); err != nil {
		steamLog.Warn("increment switch statistics", slog.Any("err", err))
	}
	platform.TriggerDiscordPresenceRefresh()
	tray.MaybeHideMainWindow()
}

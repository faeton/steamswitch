package steam

import (
	"errors"
	"strings"
)

// The OS seam.
//
// Everything the Steam engine needs from the operating system goes through `osBackend`. The
// rest of the package — VDF parsing, account metadata, the Session Kit, Dota config handling —
// is plain file work and is identical everywhere.
//
// This exists because `internal/winutil` is not merely "the Windows implementation" of these
// operations, it is Windows *shaped*: it takes registry paths like `HKCU\Software\Valve\Steam`
// and process names like `steam.exe`, neither of which has a meaning on macOS. A build tag
// over winutil could only ever produce stubs. A backend can produce an answer:
//
//	Windows                                  macOS
//	HKCU\Software\Valve\Steam\AutoLoginUser   registry.vdf → Registry/HKCU/Software/Valve/Steam
//	steam.exe                                 steam_osx
//	TerminateProcess                          SIGTERM, then SIGKILL
//	C:\Program Files (x86)\Steam              ~/Library/Application Support/Steam
//
// What a swap actually does is unchanged by the platform: point Steam at an account it has
// already remembered. It cannot create a session that does not exist on the machine, on any
// OS.
//
// Adding a platform means adding one `os_backend_<goos>.go` and nothing else. Leaving one out
// is also fine — `unsupportedBackend` fails closed, which is why `ProcessInspectionSupported`
// is part of this interface rather than a winutil detail.

// ErrSwitchingUnsupported is returned by every mutating entry point on a build with no
// backend for the host OS.
var ErrSwitchingUnsupported = errors.New("Toast_Steam_SwitchingUnsupportedOnThisOS")

// osBackend is the OS-specific half of the Steam engine.
type osBackend interface {
	// ProcessInspectionSupported reports whether IsRunning can be trusted.
	//
	// This is the single most important method here. A backend that cannot see processes
	// must say so, because "no Steam process found" is indistinguishable from "Steam is
	// closed" and every write guard in this package reads the latter as permission to
	// proceed. Answering false makes those guards refuse instead.
	ProcessInspectionSupported() bool

	// ProcessNames lists the processes that hold Steam's configuration files, in the form
	// the other methods accept. A `SERVICE:` prefix marks a Windows service.
	ProcessNames() []string

	// GameProcessNames lists game processes that keep their own handles on `userdata` and
	// outlive Steam — Dota being the one this fork cares about.
	GameProcessNames() []string

	// IsRunning reports whether the named process is alive. Only meaningful when
	// ProcessInspectionSupported is true.
	IsRunning(name string) bool

	// CanClose reports a permission problem *before* anything is killed, so the caller can
	// offer an elevated restart rather than discovering the problem half way through.
	CanClose(names []string, closingMethod string) error

	// Close terminates the named processes and returns once they are gone. A returned error
	// is advisory: the authority on whether it worked is IsRunning, since a kill can report
	// failure for a process that did exit.
	Close(names []string, closingMethod string) error

	// SetAutoLogin records the account Steam should sign in as, in whatever store the client
	// reads outside loginusers.vdf. `accountName` is the login name, not the SteamID64, and
	// an empty one means "no account selected" — the account-chooser state.
	SetAutoLogin(steamRoot, accountName string, rememberPassword bool) error

	// AutoLoginUser reads that selection back.
	AutoLoginUser(steamRoot string) (string, error)

	// DeleteRegistryValue removes one value from the Steam key, for Advanced Cleaning.
	// Reports whether it existed.
	DeleteRegistryValue(steamRoot, valueName string) (existed bool, err error)

	// DefaultRoot is where Steam keeps `config/loginusers.vdf` and `userdata/` on this OS
	// when the user has not configured a path. Empty when there is no sensible default.
	DefaultRoot() string

	// Launch starts Steam. `steamRoot` is the data root; a backend whose executable lives
	// elsewhere is responsible for finding it.
	Launch(steamRoot string, args []string, opts LaunchOpts) error

	// OpenURL hands a `steam://` URL to the desktop's protocol handler, which is how a game
	// is launched once the right account is signed in.
	OpenURL(url string) error
}

// LaunchOpts is the OS-neutral subset of the launch settings.
type LaunchOpts struct {
	// Admin requests an elevated launch. Windows-only in practice; a backend that has no
	// equivalent ignores it rather than failing, since the user's intent — "start Steam" —
	// is still satisfiable.
	Admin bool
	// StartingMethod mirrors Settings.StartingMethod ("Default", "Direct").
	StartingMethod string
}

// backend is resolved once at init by the build-tagged newOSBackend for this GOOS.
var backend = newOSBackend()

// SwitchingSupported reports whether this build can actually swap accounts. The UI uses it to
// explain itself instead of presenting controls that will refuse.
func SwitchingSupported() bool { return backend.ProcessInspectionSupported() }

// requireProcessInspection is the guard every mutating path calls first.
//
// Gating the UI is not enough: the tray, the CLI (`internal/app/dispatch.go`) and the bound
// services all reach these functions directly. The case that matters most is
// `dotaSteamRunningGuard`, which has no backstop — Steam Cloud silently reverts a `remote/`
// write made while the client is up, so a wrong "Steam is closed" answer costs the user their
// hero grids with no error anywhere.
func requireProcessInspection() error {
	if !backend.ProcessInspectionSupported() {
		return ErrSwitchingUnsupported
	}
	return nil
}

// unsupportedBackend is used on any OS with no implementation. Every method fails closed.
type unsupportedBackend struct{}

func (unsupportedBackend) ProcessInspectionSupported() bool { return false }
func (unsupportedBackend) ProcessNames() []string           { return nil }
func (unsupportedBackend) GameProcessNames() []string       { return nil }

// IsRunning reports false, which is why ProcessInspectionSupported reports false too: on its
// own this answer reads as "safe to write" at exactly the wrong moment.
func (unsupportedBackend) IsRunning(string) bool { return false }

func (unsupportedBackend) CanClose([]string, string) error { return ErrSwitchingUnsupported }
func (unsupportedBackend) Close([]string, string) error    { return ErrSwitchingUnsupported }
func (unsupportedBackend) SetAutoLogin(string, string, bool) error {
	return ErrSwitchingUnsupported
}
func (unsupportedBackend) AutoLoginUser(string) (string, error) { return "", ErrSwitchingUnsupported }
func (unsupportedBackend) DeleteRegistryValue(string, string) (bool, error) {
	return false, ErrSwitchingUnsupported
}
func (unsupportedBackend) DefaultRoot() string                       { return "" }
func (unsupportedBackend) OpenURL(string) error                      { return ErrSwitchingUnsupported }
func (unsupportedBackend) Launch(string, []string, LaunchOpts) error { return ErrSwitchingUnsupported }

// runningProcessNames is the shared implementation behind sessionkit.Lifecycle.RunningProcesses.
//
// Service entries are skipped: they hold no user configuration and cannot be polled by name.
func runningProcessNames() []string {
	var out []string
	for _, name := range append(backend.ProcessNames(), backend.GameProcessNames()...) {
		if strings.HasPrefix(name, "SERVICE:") {
			continue
		}
		if backend.IsRunning(name) {
			out = append(out, name)
		}
	}
	return out
}

// steamClientRunning reports whether the Steam client itself is up.
//
// The first entry in ProcessNames is the client; the helpers behind it can outlive it briefly
// and are not what "is Steam running" means to a user. A false answer here can also mean
// "this build cannot see processes", so callers that must not write while Steam is up have to
// gate on requireProcessInspection separately — this is a warning signal, not a guard.
func steamClientRunning() bool {
	names := backend.ProcessNames()
	if len(names) == 0 {
		return false
	}
	return backend.IsRunning(names[0])
}

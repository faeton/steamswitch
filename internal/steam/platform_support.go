package steam

import (
	"errors"

	"steamswitch/internal/winutil"
)

// Fail-closed gate for builds that cannot inspect running processes.
//
// Every operation in this package that mutates Steam's on-disk state is only safe while
// Steam is closed. On Windows that is enforced by killing the processes and then checking
// they are gone. On other platforms `winutil.IsExeRunning` is a stub that always returns
// false, so those same checks silently conclude "Steam is not running" and the writes go
// ahead into a live install.
//
// The dangerous case is not the classic switch — `winutil.KillByName` returns ErrUnsupported
// there, so it fails loudly enough. It is `dotaSteamRunningGuard`: that guard exists solely
// to stop a write to the cloud-synced `remote/` tree while Steam is up, because Steam Cloud
// would overwrite it on next launch. With a constant-false answer the guard never fires and
// the user loses the config with no error at all.
//
// So the rule is: if we cannot tell whether Steam is running, refuse. Gating the UI is not
// enough — the tray, the CLI (`internal/app/dispatch.go`) and the bound services all reach
// these functions directly.

// ErrSwitchingUnsupported is returned when this build cannot verify that Steam is closed.
//
// The message is an i18n key; the frontend resolves it via the toast pipeline.
var ErrSwitchingUnsupported = errors.New("Toast_Steam_SwitchingUnsupportedOnThisOS")

// requireProcessInspection refuses when the build cannot tell whether Steam is running.
func requireProcessInspection() error {
	if !winutil.ProcessInspectionSupported() {
		return ErrSwitchingUnsupported
	}
	return nil
}

// SwitchingSupported reports whether this build can switch accounts at all.
//
// Exposed so the frontend can disable the affected controls and explain why, rather than
// letting the user click a button that always fails.
func SwitchingSupported() bool { return winutil.ProcessInspectionSupported() }

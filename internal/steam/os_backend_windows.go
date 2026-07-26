//go:build windows

package steam

import (
	"path/filepath"
	"strings"

	"steamswitch/internal/winutil"
)

// The Windows backend. Every method is a thin adapter onto `internal/winutil`; the behaviour
// is exactly what this package did before the seam existed.

func newOSBackend() osBackend { return windowsBackend{} }

type windowsBackend struct{}

// steamRegKey is the hive a swap owns.
const steamRegKey = `HKCU\Software\Valve\Steam`

func (windowsBackend) ProcessInspectionSupported() bool { return true }

func (windowsBackend) ProcessNames() []string {
	return []string{
		"steam.exe",
		"SERVICE:Steam Client Service",
		"steamwebhelper.exe",
		"GameOverlayUI.exe",
	}
}

func (windowsBackend) GameProcessNames() []string { return dotaProcessNames }

func (windowsBackend) IsRunning(name string) bool { return winutil.IsExeRunning(name) }

func (windowsBackend) CanClose(names []string, closingMethod string) error {
	return winutil.ErrIfCannotKill(names, winutil.ClosingMethod(closingMethod))
}

func (windowsBackend) Close(names []string, closingMethod string) error {
	return winutil.KillByName(names, winutil.ClosingMethod(closingMethod), nil)
}

// SetAutoLogin writes both values the client consults. Writing only AutoLoginUser would leave
// Steam with two contradictory answers about whether to stay signed in.
func (windowsBackend) SetAutoLogin(_, accountName string, rememberPassword bool) error {
	if err := winutil.RegistryWrite(steamRegKey+":AutoLoginUser", accountName); err != nil {
		return err
	}
	remember := uint32(0)
	if rememberPassword {
		remember = 1
	}
	return winutil.RegistryWrite(steamRegKey+":RememberPassword", remember)
}

func (windowsBackend) AutoLoginUser(string) (string, error) {
	v, _, err := winutil.RegistryRead(steamRegKey + ":AutoLoginUser")
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return strings.TrimSpace(s), nil
}

func (windowsBackend) DeleteRegistryValue(_, valueName string) (bool, error) {
	err := winutil.RegistryDelete(steamRegKey + ":" + valueName)
	if err == nil {
		return true, nil
	}
	if winutil.RegistryDeleteIsNotExist(err) {
		return false, nil
	}
	return false, err
}

// DefaultRoot is left to Platforms.json, whose ExeLocationDefault already carries the Windows
// path and is user-overridable.
func (windowsBackend) DefaultRoot() string { return "" }

func (windowsBackend) Launch(steamRoot string, args []string, opts LaunchOpts) error {
	return winutil.Start(filepath.Join(steamRoot, "steam.exe"), args, winutil.StartOpts{
		Admin:         opts.Admin,
		Method:        winutil.StartingMethod(strings.TrimSpace(opts.StartingMethod)),
		HideWindow:    false,
		WorkingDir:    steamRoot,
		AsDesktopUser: winutil.IsProcessElevated() && !opts.Admin,
	})
}

// OpenURL goes through `cmd /c start`, which resolves the protocol handler from the registry.
// The empty second argument is the window title `start` would otherwise take the URL to be.
func (windowsBackend) OpenURL(url string) error {
	return winutil.Start("cmd.exe", []string{"/c", "start", "", url}, winutil.StartOpts{})
}

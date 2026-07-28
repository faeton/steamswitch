//go:build windows

package steam

import (
	"os"
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

// steamInstallPathValues are where the client records its own install directory, most
// authoritative first.
//
// HKCU is written by the running client for the current user and tracks a move; the HKLM
// values are written by the installer. WOW6432Node is listed before the plain key because
// Steam ships a 32-bit installer, so on a 64-bit Windows the redirected key is the one that
// exists — but a 32-bit or ARM host has it under the plain path instead.
var steamInstallPathValues = []string{
	`HKCU\Software\Valve\Steam:SteamPath`,
	`HKLM\SOFTWARE\WOW6432Node\Valve\Steam:InstallPath`,
	`HKLM\SOFTWARE\Valve\Steam:InstallPath`,
}

// DefaultRoot asks Steam where it is rather than guessing.
//
// This used to return "" and defer to Platforms.json's `%ProgramFiles(x86)%\Steam\steam.exe`.
// That was a guess dressed as a default: it is wrong for everyone who installed Steam on
// another drive, and it was never even consulted, because the hardcoded `FolderPath` in
// settings won first. The registry values below are the client's own record of where it
// lives, so they are right for a custom install and for one that has been moved.
//
// Platforms.json remains the fallback for the case where none of them can be read.
func (windowsBackend) DefaultRoot() string {
	for _, encoded := range steamInstallPathValues {
		v, _, err := winutil.RegistryRead(encoded)
		if err != nil {
			continue
		}
		s, _ := v.(string)
		// Steam writes HKCU\SteamPath with forward slashes ("c:/program files (x86)/steam"),
		// which every path join downstream would carry through unchanged.
		s = filepath.FromSlash(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if st, err := os.Stat(s); err == nil && st.IsDir() {
			return filepath.Clean(s)
		}
	}
	return ""
}

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

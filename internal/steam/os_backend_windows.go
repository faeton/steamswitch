//go:build windows

package steam

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"steamswitch/internal/winutil"
)

// The Windows backend. Most methods are thin adapters onto `internal/winutil`; Close adds the
// polite request in front of them.

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

// Close asks Steam to quit, then falls back to KillByName.
//
// Steam ignored the WM_CLOSE and plain taskkill that KillByName tries first (measured under
// CrossOver, not yet on real Windows), so it was force-killed every time: no logoff, no
// "Shutdown" in bootstrap_log, and ActiveUser and pid left set in the registry. `steam.exe
// -shutdown` exited cleanly in about 4 s against about 8 s for the kill.
func (b windowsBackend) Close(names []string, closingMethod string) error {
	method := winutil.ClosingMethod(closingMethod)
	root := b.DefaultRoot()
	client, args, canAsk := windowsExitRequest(root, fileExists)
	if !canAsk || !closingMethodAsksFirst(method) {
		return winutil.KillByName(names, method, nil)
	}

	processes, services := splitServices(names)
	started := time.Now()
	outcome, err := closeSequence{
		clientRunning: func() bool { return len(processes) > 0 && winutil.IsExeRunning(processes[0]) },
		anyRunning:    func() bool { return anyExeRunning(processes) },
		askToExit: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), windowsAskTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, client, args...)
			cmd.Dir = root
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			return cmd.Run()
		},
		// KillByName already goes from polite to forceful, so there is no separate middle step.
		terminate:   func() {},
		kill:        func() { _ = winutil.KillByName(names, method, nil) },
		exitGrace:   closeExitGrace(windowsExitGrace, root),
		askSettle:   windowsAskTimeout,
		helperGrace: windowsHelperGrace,
		poll:        windowsClosePoll,
	}.run()
	steamLog.Info("steam close", slog.String("via", string(outcome)), slog.Duration("took", time.Since(started)))

	// A client that quit by itself may leave its service behind. The kill path has already
	// handled it, because `names` includes the service.
	if outcome != closeByForce && len(services) > 0 {
		if serr := winutil.KillByName(services, method, nil); serr != nil && err == nil {
			err = serr
		}
	}
	return err
}

// How long a running Steam gets to act on `-shutdown`. The ask is bounded below the grace, so a
// stuck request is dead before a kill could leave it to start Steam again.
const (
	windowsExitGrace   = 10 * time.Second
	windowsHelperGrace = 3 * time.Second
	windowsAskTimeout  = 5 * time.Second
	windowsClosePoll   = 250 * time.Millisecond
)

// windowsExitRequest is the command that asks a running client to quit. It is only for a client
// known to be running: on macOS, with none, `-shutdown` starts Steam (measured), and closeSequence
// checks first. ok is false when there is no client to run.
func windowsExitRequest(root string, exists func(string) bool) (name string, args []string, ok bool) {
	if strings.TrimSpace(root) == "" {
		return "", nil, false
	}
	client := filepath.Join(root, "steam.exe")
	if !exists(client) {
		return "", nil, false
	}
	return client, []string{"-shutdown"}, true
}

// closingMethodAsksFirst is false for TaskKill, the setting for an immediate kill.
func closingMethodAsksFirst(method winutil.ClosingMethod) bool {
	return method != winutil.ClosingTaskKill
}

// splitServices separates services from processes. A SERVICE: entry cannot be looked up as a
// process, so left in the list it would simply read as never running.
func splitServices(names []string) (processes, services []string) {
	for _, name := range names {
		if strings.HasPrefix(name, "SERVICE:") {
			services = append(services, name)
		} else {
			processes = append(processes, name)
		}
	}
	return processes, services
}

func anyExeRunning(names []string) bool {
	for _, name := range names {
		if winutil.IsExeRunning(name) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
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

// ActiveUserID32 reads HKCU\Software\Valve\Steam\ActiveProcess\ActiveUser, which the client
// sets to the signed-in account's id32 and to 0 while it has none.
func (windowsBackend) ActiveUserID32(string) string {
	v, _, err := winutil.RegistryRead(steamRegKey + `\ActiveProcess:ActiveUser`)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "0" || s == "<nil>" {
		return ""
	}
	return s
}

// LocalVDFPath is under %LOCALAPPDATA%, not the install folder.
func (windowsBackend) LocalVDFPath(string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Steam", "local.vdf")
}

// AppMarkedRunning reads the per-app Running flag, then the client's own RunningAppID.
func (windowsBackend) AppMarkedRunning(_, appID string) bool {
	if v, _, err := winutil.RegistryRead(steamRegKey + `\Apps\` + appID + `:Running`); err == nil {
		if strings.TrimSpace(fmt.Sprint(v)) == "1" {
			return true
		}
	}
	if v, _, err := winutil.RegistryRead(steamRegKey + `:RunningAppID`); err == nil {
		return strings.TrimSpace(fmt.Sprint(v)) == appID
	}
	return false
}

func (windowsBackend) RunningAppID(string) string {
	v, _, err := winutil.RegistryRead(steamRegKey + `:RunningAppID`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// ProcessRunningFrom catches a game that outlived Steam, where the Running flag says nothing.
func (windowsBackend) ProcessRunningFrom(dir string) bool { return winutil.AnyProcessRunningFrom(dir) }

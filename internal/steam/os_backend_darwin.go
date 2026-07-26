//go:build darwin

package steam

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The macOS backend.
//
// Steam on macOS keeps the same data layout as on Windows — `config/loginusers.vdf`,
// `userdata/<id32>/570/{local,remote}`, `local.vdf` — under
// `~/Library/Application Support/Steam`. The two things that differ are where the
// Windows-registry values live (`registry.vdf` in that same directory, see registryvdf.go)
// and how processes are named and stopped.
//
// The install root and the data root are separate here, unlike on Windows where
// `C:\Program Files (x86)\Steam` holds both `steam.exe` and `config/`. Everything in this
// package means the *data* root when it says `steamRoot`; only Launch needs the bundle, and
// it finds that itself.
//
// What a switch does is the same as on Windows and no better: it selects among accounts the
// machine has already remembered. It does not create a session. An account listed in
// loginusers.vdf with no corresponding entry in `local.vdf`'s ConnectCache will land on the
// password prompt — on either OS.

func newOSBackend() osBackend { return darwinBackend{} }

type darwinBackend struct{}

// Process names as they appear in the kernel's process table, which is what pgrep matches.
//
// `steam_osx` is the client itself. `steamwebhelper` is the CEF host and is the one that
// actually holds config file handles open after the main window closes. There is no
// equivalent of the Windows "Steam Client Service"; on macOS the client runs entirely as the
// logged-in user.
var darwinSteamProcesses = []string{
	"steam_osx",
	"steamwebhelper",
}

func (darwinBackend) ProcessInspectionSupported() bool { return true }

func (darwinBackend) ProcessNames() []string {
	return append([]string(nil), darwinSteamProcesses...)
}

// GameProcessNames covers the game binaries that keep their own handles on `userdata` and
// survive Steam exiting. macOS drops the `.exe` suffix the Windows list carries.
func (darwinBackend) GameProcessNames() []string {
	out := make([]string, 0, len(dotaProcessNames))
	for _, name := range dotaProcessNames {
		out = append(out, strings.TrimSuffix(name, ".exe"))
	}
	return out
}

// IsRunning matches on the executable name exactly.
//
// `pgrep -x` rather than a substring match: `dota2` must not match `dota2launcher`, and a
// false positive here blocks every switch with an error the user cannot act on. It is part of
// the macOS base system, so there is no dependency to install.
func (darwinBackend) IsRunning(name string) bool {
	pids, err := pgrepExact(name)
	return err == nil && len(pids) > 0
}

func pgrepExact(name string) ([]int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	out, err := exec.Command("/usr/bin/pgrep", "-x", name).Output()
	if err != nil {
		var ee *exec.ExitError
		// pgrep exits 1 for "no match", which is an answer, not a failure.
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var pids []int
	for _, line := range strings.Fields(string(out)) {
		if pid, convErr := strconv.Atoi(line); convErr == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// CanClose reports whether the processes could be signalled.
//
// On macOS a user can always signal their own processes, and Steam runs as the logged-in user,
// so the only failure worth pre-detecting is a process owned by somebody else — which means
// Steam was started by another account and this app cannot manage it. Signal 0 asks the kernel
// exactly that question without delivering anything.
func (darwinBackend) CanClose(names []string, _ string) error {
	for _, name := range names {
		pids, err := pgrepExact(name)
		if err != nil {
			return fmt.Errorf("listing %s: %w", name, err)
		}
		for _, pid := range pids {
			if err := syscall.Kill(pid, 0); err != nil {
				if errors.Is(err, syscall.EPERM) {
					return fmt.Errorf("%s (pid %d) belongs to another user", name, pid)
				}
				// ESRCH just means it exited between the listing and the check.
				if !errors.Is(err, syscall.ESRCH) {
					return fmt.Errorf("%s (pid %d): %w", name, pid, err)
				}
			}
		}
	}
	return nil
}

// darwinCloseGrace is how long a SIGTERM'd Steam gets to shut down cleanly.
//
// Steam flushes `loginusers.vdf`, `registry.vdf` and the cloud-sync state on exit. Killing it
// before that finishes is precisely how a switch corrupts the file it is about to rewrite, so
// the grace period is generous and SIGKILL is a last resort.
const darwinCloseGrace = 20 * time.Second

// darwinClosePoll is how often the process table is re-checked while waiting.
const darwinClosePoll = 250 * time.Millisecond

// Close asks politely, then insists.
//
// The Windows ClosingMethod ("Combined", "Close", "TaskKill") has no macOS analogue and is
// ignored: there is exactly one sensible sequence here, and offering the user a choice between
// signals they cannot reason about would be a setting that only creates support questions.
func (b darwinBackend) Close(names []string, _ string) error {
	signalAll(names, syscall.SIGTERM)

	deadline := time.Now().Add(darwinCloseGrace)
	for time.Now().Before(deadline) {
		if !b.anyRunning(names) {
			return nil
		}
		time.Sleep(darwinClosePoll)
	}

	signalAll(names, syscall.SIGKILL)
	// Give the kernel a moment to reap, then report honestly. The caller treats a non-nil
	// error as advisory and re-checks with IsRunning, so a stubborn process surfaces either
	// way — but saying so here makes the log useful.
	time.Sleep(darwinClosePoll)
	if b.anyRunning(names) {
		return fmt.Errorf("steam did not exit after SIGKILL")
	}
	return nil
}

func (b darwinBackend) anyRunning(names []string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, "SERVICE:") {
			continue
		}
		if b.IsRunning(name) {
			return true
		}
	}
	return false
}

func signalAll(names []string, sig syscall.Signal) {
	for _, name := range names {
		if strings.HasPrefix(name, "SERVICE:") {
			continue
		}
		pids, err := pgrepExact(name)
		if err != nil {
			continue
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, sig)
		}
	}
}

// SetAutoLogin writes into registry.vdf, the file Steam reads on macOS in place of the
// Windows registry.
func (darwinBackend) SetAutoLogin(steamRoot, accountName string, rememberPassword bool) error {
	if strings.TrimSpace(steamRoot) == "" {
		return errors.New("steam data folder not found")
	}
	reg, err := readRegistryVDF(steamRoot)
	if err != nil {
		return err
	}
	reg.Set("AutoLoginUser", accountName)
	// Kept in step with loginusers.vdf for the same reason as on Windows: two stores
	// disagreeing about whether to stay signed in is how an account ends up at a password
	// prompt it was never meant to see.
	if rememberPassword {
		reg.Set("RememberPassword", "1")
	} else {
		reg.Set("RememberPassword", "0")
	}
	return reg.Write(steamRoot)
}

func (darwinBackend) AutoLoginUser(steamRoot string) (string, error) {
	reg, err := readRegistryVDF(steamRoot)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reg.Get("AutoLoginUser")), nil
}

func (darwinBackend) DeleteRegistryValue(steamRoot, valueName string) (bool, error) {
	reg, err := readRegistryVDF(steamRoot)
	if err != nil {
		return false, err
	}
	if !reg.Delete(valueName) {
		return false, nil
	}
	return true, reg.Write(steamRoot)
}

// DefaultRoot is Steam's data directory, which is what the rest of this package means by
// steamRoot. Returned even when it does not exist: an empty answer would send the caller to
// Platforms.json's Windows-only ExeLocationDefault, which resolves to nothing useful here.
func (darwinBackend) DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Steam")
}

// darwinBundleCandidates are where the Steam application bundle may live, most authoritative
// first. `Steam.AppBundle/Steam` is the copy the client keeps inside its own data directory
// and self-updates; it is a real bundle but lacks the `.app` extension, so it is the fallback
// rather than the first choice.
func darwinBundleCandidates(steamRoot string) []string {
	return []string{
		"/Applications/Steam.app",
		filepath.Join(steamRoot, "Steam.AppBundle", "Steam"),
	}
}

// Launch starts Steam through LaunchServices.
//
// `open` rather than exec'ing `steam_osx` directly: it attaches the process to the user's GUI
// session, gives it the bundle identity Steam expects, and — the part that matters — does not
// make Steam a child of SteamSwitch, so quitting the switcher does not take the client with
// it.
//
// LaunchOpts.Admin is ignored. macOS has no "run this GUI app elevated" that is not a
// privilege-escalation prompt, Steam does not need one, and failing the launch over a Windows
// setting the user cannot see would be worse than quietly doing the sensible thing.
func (darwinBackend) Launch(steamRoot string, args []string, _ LaunchOpts) error {
	bundle := ""
	for _, candidate := range darwinBundleCandidates(steamRoot) {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			bundle = candidate
			break
		}
	}
	if bundle == "" {
		return fmt.Errorf("Steam.app not found (looked in %s)",
			strings.Join(darwinBundleCandidates(steamRoot), ", "))
	}

	argv := []string{"-a", bundle}
	if len(args) > 0 {
		argv = append(argv, "--args")
		argv = append(argv, args...)
	}
	return exec.Command("/usr/bin/open", argv...).Run()
}

// OpenURL hands the URL to LaunchServices, which routes `steam://` to whichever Steam
// installation is registered for it.
func (darwinBackend) OpenURL(url string) error {
	return exec.Command("/usr/bin/open", url).Run()
}

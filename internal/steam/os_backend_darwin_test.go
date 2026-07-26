//go:build darwin

package steam

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"steamswitch/internal/platform"
)

// macOS is a Tier 1 target: switching is expected to work, so these assert capability rather
// than refusal. What they cannot assert is that a real switch succeeds — that needs a Steam
// install and a second account, and lives in TESTING.md.

func TestDarwin_SwitchingIsSupported(t *testing.T) {
	if !SwitchingSupported() {
		t.Fatal("macOS is Tier 1; the backend must report that it can see processes")
	}
	if err := requireProcessInspection(); err != nil {
		t.Fatalf("requireProcessInspection = %v, want nil", err)
	}
}

// TestDarwin_ProcessNamesHaveNoExeSuffix guards the most likely porting mistake: reusing the
// Windows list. "steam.exe" matches nothing on macOS, so every guard would report Steam closed
// while it is running — a false all-clear, which is the failure this whole seam exists to
// prevent.
func TestDarwin_ProcessNamesHaveNoExeSuffix(t *testing.T) {
	for _, name := range append(backend.ProcessNames(), backend.GameProcessNames()...) {
		if strings.HasSuffix(name, ".exe") {
			t.Fatalf("%q is a Windows process name; it can never match on macOS", name)
		}
		if strings.HasPrefix(name, "SERVICE:") {
			t.Fatalf("%q is a Windows service; macOS Steam runs entirely as the user", name)
		}
	}
	if len(backend.ProcessNames()) == 0 {
		t.Fatal("no process names, so IsRunning can never report Steam as running")
	}
	if backend.ProcessNames()[0] != "steam_osx" {
		t.Fatalf("first process name is %q; steamClientRunning takes it to be the client itself",
			backend.ProcessNames()[0])
	}
	// dota2, not dota2.exe — the Dota part of the kit depends on this matching.
	found := false
	for _, name := range backend.GameProcessNames() {
		if name == "dota2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("game process names = %v, want dota2", backend.GameProcessNames())
	}
}

// TestDarwin_IsRunningMatchesExactly pins `pgrep -x` semantics against a process this test
// actually starts. A substring match would make "dota2" match "dota2launcher" and block every
// switch with an error the user cannot act on.
//
// A child of our own is the only reliable control: `pgrep` without -U lists just the invoking
// user's processes, so pid 1 is invisible, and a hardcoded GUI app is not present on a build
// machine.
func TestDarwin_IsRunningMatchesExactly(t *testing.T) {
	cmd := exec.Command("/bin/cat") // reads stdin forever, costs nothing
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// pgrep reads a table the kernel updates asynchronously with exec.
	deadline := time.Now().Add(5 * time.Second)
	for !backend.IsRunning("cat") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !backend.IsRunning("cat") {
		t.Skip("the process table is not visible in this environment")
	}

	if backend.IsRunning("ca") {
		t.Fatal("a prefix matched; pgrep is not being run with -x")
	}
	if backend.IsRunning("cat-definitely-not-running") {
		t.Fatal("a longer name matched; pgrep is not being run with -x")
	}
	if backend.IsRunning("") {
		t.Fatal("an empty name matched something")
	}
}

// TestDarwin_ProcessNamesFitInTheKernelCommField is a trap worth pinning before the CS2 module
// lands. macOS truncates a process's `p_comm` to MAXCOMLEN (16) characters, and `pgrep -x`
// compares against that truncated value — so a longer name silently never matches, and the
// guard that depends on it silently never fires.
func TestDarwin_ProcessNamesFitInTheKernelCommField(t *testing.T) {
	const maxComLen = 16
	for _, name := range append(backend.ProcessNames(), backend.GameProcessNames()...) {
		if len(name) > maxComLen {
			t.Fatalf("%q is %d characters; pgrep -x compares against a %d-character field, so it can never match",
				name, len(name), maxComLen)
		}
	}
}

func TestDarwin_PgrepIsPresent(t *testing.T) {
	// The backend shells out to it, so its absence would silently turn every "is Steam
	// running" answer into "no" — the fail-open case.
	if _, err := os.Stat("/usr/bin/pgrep"); err != nil {
		t.Fatalf("/usr/bin/pgrep is missing: %v", err)
	}
	if _, err := exec.LookPath("/usr/bin/open"); err != nil {
		t.Fatalf("/usr/bin/open is missing, so Steam cannot be launched: %v", err)
	}
}

func TestDarwin_DefaultRootIsTheDataDirectory(t *testing.T) {
	root := backend.DefaultRoot()
	if root == "" {
		t.Fatal("no default root; ResolveInstallFolder would fall back to a Windows path")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Library", "Application Support", "Steam")
	if root != want {
		t.Fatalf("DefaultRoot = %q, want %q", root, want)
	}
	// It must be the folder holding config/loginusers.vdf, not the application bundle: every
	// other path in this package is built from it.
	if strings.HasSuffix(root, ".app") {
		t.Fatalf("DefaultRoot = %q is an application bundle, not the data root", root)
	}
}

func TestDarwin_SetAutoLoginRoundTrips(t *testing.T) {
	root := t.TempDir()
	if err := backend.SetAutoLogin(root, "someone", true); err != nil {
		t.Fatal(err)
	}
	got, err := backend.AutoLoginUser(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "someone" {
		t.Fatalf("AutoLoginUser = %q, want %q", got, "someone")
	}

	reg, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	if v := reg.Get("RememberPassword"); v != "1" {
		t.Fatalf("RememberPassword = %q, want 1", v)
	}

	// The public-computer case: the two stores must not disagree.
	if err := backend.SetAutoLogin(root, "someone", false); err != nil {
		t.Fatal(err)
	}
	reg, _ = readRegistryVDF(root)
	if v := reg.Get("RememberPassword"); v != "0" {
		t.Fatalf("RememberPassword = %q after opting out, want 0", v)
	}
}

func TestDarwin_SetAutoLoginRefusesAnEmptyRoot(t *testing.T) {
	// Otherwise it would create ./registry.vdf next to the running binary.
	if err := backend.SetAutoLogin("", "someone", true); err == nil {
		t.Fatal("expected a refusal for an unresolved Steam root")
	}
}

func TestDarwin_DeleteRegistryValue(t *testing.T) {
	root := t.TempDir()
	if err := backend.SetAutoLogin(root, "someone", true); err != nil {
		t.Fatal(err)
	}
	existed, err := backend.DeleteRegistryValue(root, "AutoLoginUser")
	if err != nil || !existed {
		t.Fatalf("existed = %v, err = %v", existed, err)
	}
	existed, err = backend.DeleteRegistryValue(root, "AutoLoginUser")
	if err != nil || existed {
		t.Fatalf("second delete: existed = %v, err = %v", existed, err)
	}
}

func TestDarwin_LaunchReportsAMissingBundleRatherThanSucceeding(t *testing.T) {
	if _, err := os.Stat("/Applications/Steam.app"); err == nil {
		t.Skip("Steam is installed here, so the not-found path cannot be exercised")
	}
	err := backend.Launch(t.TempDir(), nil, LaunchOpts{})
	if err == nil {
		t.Fatal("Launch reported success with no Steam.app anywhere")
	}
	if !strings.Contains(err.Error(), "Steam.app") {
		t.Fatalf("err = %v, want it to name what was missing", err)
	}
}

// TestDarwin_ResolveInstallFolderPrefersTheOSDefault pins that Platforms.json's Windows-only
// ExeLocationDefault does not win here.
func TestDarwin_ResolveInstallFolderPrefersTheOSDefault(t *testing.T) {
	app := platform.AppSettings{PlatformExePaths: map[string]string{}}
	descriptor := []byte(`{"Platforms":{"Steam":{"ExeLocationDefault":"C:\\Steam\\steam.exe"}}}`)
	root, err := ResolveInstallFolder(t.TempDir(), Settings{}, app, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if root != backend.DefaultRoot() {
		t.Fatalf("ResolveInstallFolder = %q, want the macOS data root %q", root, backend.DefaultRoot())
	}
}

package steam

import (
	"os"
	"path/filepath"
	"testing"

	"steamswitch/internal/platform"
)

// winDescriptor is the real shape of the Steam entry in Platforms.json: a single Windows path
// that expands to nothing on any other OS, which is exactly why it cannot be the only source
// of truth about where Steam lives.
const winDescriptor = `{"Platforms":{"Steam":{"ExeLocationDefault":"%ProgramFiles(x86)%\\Steam\\steam.exe"}}}`

func steamRootDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "loginusers.vdf"), []byte(`"users"{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func noExePaths() platform.AppSettings {
	return platform.AppSettings{PlatformExePaths: map[string]string{}}
}

// TestResolveInstallFolder_ConfiguredPathWins pins that an explicit, real setting still beats
// detection. The fix below must not take the choice away from someone who made one.
func TestResolveInstallFolder_ConfiguredPathWins(t *testing.T) {
	dir := steamRootDir(t)
	got, err := ResolveInstallFolder(t.TempDir(), Settings{FolderPath: dir}, noExePaths(), []byte(winDescriptor))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("ResolveInstallFolder = %q, want the configured %q", got, dir)
	}
}

// TestResolveInstallFolder_NonexistentConfiguredPathDoesNotWin is the regression this whole
// change exists for.
//
// The old implementation returned a non-empty FolderPath unconditionally, and defaultSettings
// pre-filled it with `C:\Program Files (x86)\Steam\`. So on a machine where Steam is anywhere
// else — another drive, another OS — the guess short-circuited every other candidate and the
// app confidently looked in a directory that does not exist.
func TestResolveInstallFolder_NonexistentConfiguredPathDoesNotWin(t *testing.T) {
	real := steamRootDir(t)
	stale := filepath.Join(t.TempDir(), "not-here")
	app := platform.AppSettings{PlatformExePaths: map[string]string{
		platformName: filepath.Join(real, "steam.exe"),
	}}

	got, err := ResolveInstallFolder(t.TempDir(), Settings{FolderPath: stale}, app, []byte(winDescriptor))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(real) {
		t.Fatalf("ResolveInstallFolder = %q, want the real root %q", got, real)
	}
}

// TestResolveInstallFolder_FallsBackToANameWhenNothingExists keeps the Settings screen usable
// on a machine with no Steam at all: it needs a path to show the user so they can correct it,
// not an empty string or an error.
func TestResolveInstallFolder_FallsBackToANameWhenNothingExists(t *testing.T) {
	if root := backend.DefaultRoot(); root != "" {
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			// Steam is installed on this machine, so detection legitimately wins and the
			// "nothing exists anywhere" branch cannot be reached.
			t.Skip("Steam is installed here; the no-Steam-anywhere branch is unreachable")
		}
	}
	stale := filepath.Join(t.TempDir(), "nowhere")
	got, err := ResolveInstallFolder(t.TempDir(), Settings{FolderPath: stale}, noExePaths(), []byte(winDescriptor))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(stale) {
		t.Fatalf("ResolveInstallFolder = %q, want the configured name %q back", got, stale)
	}
}

// TestDefaultSettings_DoesNotGuessAnInstallPath pins the other half of the fix. A default that
// is a guess is worse than no default, because ResolveInstallFolder trusts a configured path
// first and therefore never gets to detect anything.
func TestDefaultSettings_DoesNotGuessAnInstallPath(t *testing.T) {
	if got := defaultSettings().FolderPath; got != "" {
		t.Fatalf("defaultSettings().FolderPath = %q, want empty so detection runs", got)
	}
}

func TestDropForeignFolderPath(t *testing.T) {
	// The value every pre-fix install wrote, on the OS where it cannot mean anything. On
	// Windows it is absolute and kept; on Unix it is not absolute and is dropped, which is
	// what makes one rule enough for both.
	windowsLiteral := `C:\Program Files (x86)\Steam\`

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"a path for this OS is kept", filepath.Join(string(filepath.Separator), "opt", "steam"), filepath.Join(string(filepath.Separator), "opt", "steam")},
		{"a relative path is dropped", filepath.Join("relative", "steam"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dropForeignFolderPath(tc.in); got != tc.want {
				t.Fatalf("dropForeignFolderPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	got := dropForeignFolderPath(NormalizeFolderPath(windowsLiteral))
	if filepath.IsAbs(windowsLiteral) {
		if got == "" {
			t.Fatal("a Windows path on Windows was dropped; it is the legitimate default there")
		}
		return
	}
	if got != "" {
		t.Fatalf("dropForeignFolderPath(%q) = %q, want it dropped off Windows", windowsLiteral, got)
	}
}

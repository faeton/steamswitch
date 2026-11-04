package settingsfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscover_prefersPortableOverAppData(t *testing.T) {
	dir := t.TempDir()
	exeDir := filepath.Join(dir, "bin")
	portable := PortableUserDataDir(exeDir)
	appData := filepath.Join(dir, "appdata", UserDataDirName)
	for _, d := range []string{portable, appData} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(portable, FileName), []byte(`{"language":"portable"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appData, FileName), []byte(`{"language":"appdata"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	redirectUserConfigDir(t, filepath.Join(dir, "appdata"))

	got, ok := Discover(exeDir)
	if !ok {
		t.Fatal("expected settings file")
	}
	if got != filepath.Join(portable, FileName) {
		t.Fatalf("got %q, want portable settings", got)
	}
}

func TestDiscover_fallsBackToExeRoot(t *testing.T) {
	tmpAppData := filepath.Join(t.TempDir(), "appdata")
	if err := os.MkdirAll(tmpAppData, 0o755); err != nil {
		t.Fatal(err)
	}
	redirectUserConfigDir(t, tmpAppData)

	exeDir := t.TempDir()
	legacy := filepath.Join(exeDir, FileName)
	if err := os.WriteFile(legacy, []byte(`{"language":"legacy"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := Discover(exeDir)
	if !ok || got != legacy {
		t.Fatalf("got %q ok=%v, want %q", got, ok, legacy)
	}
}

func TestIsDefaultUserDataDir(t *testing.T) {
	exeDir := filepath.Join(t.TempDir(), "bin")
	portable := PortableUserDataDir(exeDir)
	custom := filepath.Join(t.TempDir(), "custom", UserDataDirName)
	if !IsDefaultUserDataDir(portable, exeDir) {
		t.Fatal("portable should be default")
	}
	if IsDefaultUserDataDir(custom, exeDir) {
		t.Fatal("custom should not be default")
	}
}

// redirectUserConfigDir points DefaultUserDataDir at `dir` for the duration of the test.
//
// Setting %APPDATA% only works on Windows: `os.UserConfigDir` reads $HOME/Library/Application
// Support on darwin and $XDG_CONFIG_HOME (else $HOME/.config) elsewhere. Without the other two
// the AppData half of these tests passes for the wrong reason off Windows — the real user
// directory has no settings file in it, so "portable wins" is not actually being demonstrated.
func redirectUserConfigDir(t *testing.T, dir string) {
	t.Helper()
	set := func(key, value string) {
		orig, had := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, orig)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
	set("APPDATA", dir)
	set("XDG_CONFIG_HOME", dir)
	// darwin appends Library/Application Support to $HOME rather than reading a variable, so
	// the only way to aim it at an arbitrary directory is to build that suffix and link the
	// leaf back to `dir`. Then all three platforms agree on where UserConfigDir lands.
	home := filepath.Join(dir, "home")
	if runtime.GOOS == "darwin" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		macParent := filepath.Join(home, "Library")
		if err := os.MkdirAll(macParent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir, filepath.Join(macParent, "Application Support")); err != nil {
			t.Fatal(err)
		}
	}
	set("HOME", home)

	got, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		if want, err := filepath.EvalSymlinks(dir); err == nil && resolved != want {
			t.Fatalf("user config dir resolved to %q, want %q — the redirect did not take", resolved, want)
		}
	}
}

// The Mac and Linux binary is called SteamSwitch and has no suffix, so {exeDir}/SteamSwitch is a
// file. Looking inside it answers ENOTDIR, which is not "not found", and the app refused to start.
func TestDefaultSearchDirs_SkipsABinaryNamedLikeTheDataFolder(t *testing.T) {
	exeDir := t.TempDir()
	if err := os.WriteFile(PortableUserDataDir(exeDir), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	redirectUserConfigDir(t, filepath.Join(t.TempDir(), "appdata"))

	for _, dir := range DefaultSearchDirs(exeDir) {
		if dir == PortableUserDataDir(exeDir) {
			t.Fatalf("the executable %q was offered as a data folder", dir)
		}
	}
	if _, ok := Discover(exeDir); ok {
		t.Fatal("found a settings file in an empty install")
	}
}

func TestDefaultSearchDirs_KeepsARealPortableFolder(t *testing.T) {
	exeDir := t.TempDir()
	if err := os.MkdirAll(PortableUserDataDir(exeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	redirectUserConfigDir(t, filepath.Join(t.TempDir(), "appdata"))
	if got := DefaultSearchDirs(exeDir)[0]; got != PortableUserDataDir(exeDir) {
		t.Fatalf("first search dir = %q, want the portable folder", got)
	}
}

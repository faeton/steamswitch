package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setTestAppData redirects the OS user-config directory to a temporary one for the duration of
// the test, so nothing here can see — or write — the host machine's real SteamSwitch data.
//
// Setting %APPDATA% alone is not enough, and quietly did nothing outside Windows.
// `os.UserConfigDir` reads a different variable per platform:
//
//	windows  %AppData%
//	darwin   $HOME/Library/Application Support
//	other    $XDG_CONFIG_HOME, else $HOME/.config
//
// so on macOS every test using this helper was writing into the developer's real
// ~/Library/Application Support/SteamSwitch. That is bad on its own, and it also made
// TestLegacyWindowSettingsMigratesPlatformsAndStats intermittently fail: the migration searches
// the AppData directory as well as the portable one, so a WindowSettings.json left behind by an
// earlier run was found in preference to the fixture the test had just written.
func setTestAppData(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	tmp := filepath.Join(base, "appdata")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatalf("create temp appdata: %v", err)
	}

	// HOME is set on every platform: on darwin and linux it is what UserConfigDir derives
	// from, and setting it on Windows is harmless.
	setEnvForTest(t, "APPDATA", tmp)
	setEnvForTest(t, "HOME", base)
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		setEnvForTest(t, "XDG_CONFIG_HOME", tmp)
	}

	// Fail loudly rather than silently polluting the real directory if this ever stops
	// working — a redirect that does nothing is exactly the failure being fixed here.
	got, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("resolve user config dir: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(got)+string(filepath.Separator), filepath.Clean(base)+string(filepath.Separator)) {
		t.Fatalf("user config dir is %q, which is outside the test temp tree %q — tests would write to the real one", got, base)
	}
}

func setEnvForTest(t *testing.T, key, value string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, orig)
			return
		}
		_ = os.Unsetenv(key)
	})
}

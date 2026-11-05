package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStatsOptOutRoundTrips(t *testing.T) {
	setTestAppData(t)
	exeDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ResetPathSingletonsForTest(exeDir)
	settings := defaultSettings()
	settings.StatsEnabled = false
	if err := SaveAppSettings(exeDir, settings); err != nil {
		t.Fatal(err)
	}

	path, ok := settingsFilePathForTest(exeDir)
	if !ok {
		t.Fatal("saved settings file was not found")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"statsEnabled": false`)) {
		t.Fatalf("explicit stats opt-out was omitted: %s", raw)
	}

	ResetPathSingletonsForTest(exeDir)
	loaded, err := LoadAppSettings(exeDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StatsEnabled {
		t.Fatalf("stats opt-out did not survive reload: enabled=%v", loaded.StatsEnabled)
	}
}

func TestInstallerStatsPreferenceIsConsumed(t *testing.T) {
	setTestAppData(t)
	exeDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userDataDir, err := DefaultUserDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(userDataDir, "SendAnonymousStats.no")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ResetPathSingletonsForTest(exeDir)
	if err := SaveAppSettings(exeDir, defaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := InitDataPaths(exeDir); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadAppSettings(exeDir)
	if err != nil {
		t.Fatal(err)
	}
	if settings.StatsEnabled {
		t.Fatalf("installer send preference applied incorrectly: enabled=%v", settings.StatsEnabled)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("installer marker was not consumed: %v", err)
	}
}

func settingsFilePathForTest(exeDir string) (string, bool) {
	for _, candidate := range append(settingsSearchDirsForTest(exeDir), exeDir) {
		path := filepath.Join(candidate, settingsFileName)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, true
		}
	}
	return "", false
}

func settingsSearchDirsForTest(exeDir string) []string {
	dirs := []string{PortableUserDataDir(exeDir)}
	if dir, err := DefaultUserDataDir(); err == nil {
		dirs = append(dirs, dir)
	}
	return dirs
}

package steam

import (
	"os"
	"path/filepath"
	"strings"

	"steamswitch/internal/platform"
)

const platformName = "Steam"

// ResolveInstallFolder returns the Steam installation root (folder containing steam.exe).
// Order: SteamSettings.FolderPath → PlatformExePaths["Steam"] (exe dir) → ExeLocationDefault from Platforms.json.
func ResolveInstallFolder(exeDir string, s Settings, app platform.AppSettings, platformsJSON []byte) (string, error) {
	if fp := strings.TrimSpace(s.FolderPath); fp != "" {
		if st, err := os.Stat(filepath.Join(fp, "config", "loginusers.vdf")); err == nil && !st.IsDir() {
			return filepath.Clean(fp), nil
		}
		// path set but loginusers missing — still return folder for user to fix
		return filepath.Clean(fp), nil
	}

	if exe := strings.TrimSpace(app.PlatformExePaths[platformName]); exe != "" {
		dir := filepath.Dir(exe)
		if dir != "" && dir != "." {
			return filepath.Clean(dir), nil
		}
	}

	// The OS default comes before Platforms.json because that file's ExeLocationDefault is a
	// Windows path (`%ProgramFiles(x86)%\Steam\steam.exe`) and expanding it anywhere else
	// yields a directory that does not exist. On Windows the backend returns "" and the
	// descriptor — which the user can override — stays authoritative.
	//
	// Note this is the *data* root, the folder holding `config/loginusers.vdf` and
	// `userdata/`. On Windows that happens to be the same folder as `steam.exe`; on macOS it
	// is not, and the backend's Launch finds the application bundle for itself.
	if def := strings.TrimSpace(backend.DefaultRoot()); def != "" {
		return filepath.Clean(def), nil
	}

	entry, err := platform.ParsePlatformEntry(platformsJSON, platformName)
	if err != nil {
		return "", err
	}
	if found := entry.ExeLocationDefault.FirstExistingExe(); found != "" {
		return filepath.Clean(filepath.Dir(found)), nil
	}
	if exp := entry.ExeLocationDefault.FirstExpanded(); exp != "" {
		return filepath.Clean(filepath.Dir(exp)), nil
	}
	return "", nil
}

// LoginUsersPath returns .../config/loginusers.vdf under the Steam root.
func LoginUsersPath(steamRoot string) string {
	return filepath.Join(steamRoot, "config", "loginusers.vdf")
}

package steam

import (
	"os"
	"path/filepath"
	"strings"

	"steamswitch/internal/platform"
)

const platformName = "Steam"

// ResolveInstallFolder returns the Steam data root — the folder holding `config/loginusers.vdf`
// and `userdata/`. On Windows that is also the folder holding `steam.exe`; on macOS it is not,
// and the backend's Launch finds the application bundle for itself.
//
// Order: SteamSettings.FolderPath → PlatformExePaths["Steam"] (exe dir) → the OS backend's
// detected root → ExeLocationDefault from Platforms.json. Each candidate must actually exist
// to win, which is the difference between this and the version that shipped before it.
//
// That existence check is the whole point. `FolderPath` used to short-circuit the entire chain
// on a non-empty value, and it defaulted to a hardcoded `C:\Program Files (x86)\Steam\` — so
// the guess was always the answer, detection never ran, and anyone who installed Steam
// anywhere else (or is not on Windows at all) got a path that cannot exist. A configured path
// still wins when it is real; it no longer wins when it is a leftover guess.
func ResolveInstallFolder(exeDir string, s Settings, app platform.AppSettings, platformsJSON []byte) (string, error) {
	configured := strings.TrimSpace(s.FolderPath)
	exeDirCandidate := ""
	if exe := strings.TrimSpace(app.PlatformExePaths[platformName]); exe != "" {
		if dir := filepath.Dir(exe); dir != "" && dir != "." {
			exeDirCandidate = dir
		}
	}
	// The OS default comes before Platforms.json because that file's ExeLocationDefault is a
	// Windows path (`%ProgramFiles(x86)%\Steam\steam.exe`) and expanding it anywhere else
	// yields a directory that does not exist.
	detected := strings.TrimSpace(backend.DefaultRoot())

	for _, candidate := range []string{configured, exeDirCandidate, detected} {
		if candidate == "" {
			continue
		}
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}

	entry, err := platform.ParsePlatformEntry(platformsJSON, platformName)
	if err != nil {
		return "", err
	}
	if found := entry.ExeLocationDefault.FirstExistingExe(); found != "" {
		return filepath.Clean(filepath.Dir(found)), nil
	}

	// Nothing on this machine exists. Return the best-known *name* anyway rather than an
	// error, so the Settings screen shows the user a path to correct instead of a blank.
	for _, candidate := range []string{configured, detected, exeDirCandidate} {
		if candidate != "" {
			return filepath.Clean(candidate), nil
		}
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

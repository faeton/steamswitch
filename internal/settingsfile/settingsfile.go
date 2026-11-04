// Package settingsfile resolves SteamSwitch.settings.json locations without importing platform.
package settingsfile

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// FileName is the app-wide settings JSON filename.
	FileName = "SteamSwitch.settings.json"
	// UserDataDirName is the folder that holds per-user app data.
	UserDataDirName = "SteamSwitch"
)

// PortableUserDataDir returns {exeDir}/SteamSwitch/.
func PortableUserDataDir(exeDir string) string {
	return filepath.Join(filepath.Clean(exeDir), UserDataDirName)
}

// DefaultUserDataDir returns the default install location (%AppData%/SteamSwitch).
func DefaultUserDataDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, UserDataDirName), nil
}

// DefaultSearchDirs returns the two default user data folders checked on launch (portable first).
//
// On macOS and Linux the binary has no `.exe` suffix, so `{exeDir}/SteamSwitch` is usually the
// executable itself. Looking inside it fails with ENOTDIR rather than "not found", which every
// caller rightly treats as a real error — so a file at that path is simply not a candidate.
func DefaultSearchDirs(exeDir string) []string {
	var dirs []string
	portable := PortableUserDataDir(exeDir)
	if st, err := os.Stat(portable); err != nil || st.IsDir() {
		dirs = append(dirs, portable)
	}
	if appData, err := DefaultUserDataDir(); err == nil {
		dirs = append(dirs, appData)
	}
	return dirs
}

// Discover finds an existing settings file. Portable user data is checked first, then AppData, then the exe dir (legacy/custom).
func Discover(exeDir string) (path string, ok bool) {
	exeDir = filepath.Clean(exeDir)
	for _, dir := range DefaultSearchDirs(exeDir) {
		if p, found := fileInDir(dir); found {
			return p, true
		}
	}
	return fileInDir(exeDir)
}

func fileInDir(dir string) (string, bool) {
	p := filepath.Join(filepath.Clean(dir), FileName)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return "", false
	}
	return p, true
}

// IsDefaultUserDataDir reports whether dir is the portable or default AppData user data folder.
func IsDefaultUserDataDir(dir, exeDir string) bool {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" {
		return false
	}
	if strings.EqualFold(dir, PortableUserDataDir(exeDir)) {
		return true
	}
	if appData, err := DefaultUserDataDir(); err == nil {
		if strings.EqualFold(dir, filepath.Clean(appData)) {
			return true
		}
	}
	return false
}

// IsExeRootPath reports whether path is {exeDir}/SteamSwitch.settings.json.
func IsExeRootPath(exeDir, path string) bool {
	return strings.EqualFold(filepath.Clean(path), filepath.Join(filepath.Clean(exeDir), FileName))
}

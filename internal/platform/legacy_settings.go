package platform

import (
	"errors"
	"os"
	"path/filepath"
)

func applyStatsInstallerPreference(settings *AppSettings) (bool, error) {
	dir, err := DefaultUserDataDir()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(dir, "SendAnonymousStats.no")); err == nil {
		settings.StatsEnabled = false
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(dir, "SendAnonymousStats.yes")); err == nil {
		settings.StatsEnabled = true
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return false, nil
}

func clearStatsInstallerPreference() {
	dir, err := DefaultUserDataDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, "SendAnonymousStats.no"))
	_ = os.Remove(filepath.Join(dir, "SendAnonymousStats.yes"))
}

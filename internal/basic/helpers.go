package basic

import (
	"path/filepath"

	"steamswitch/internal/paths"
)

func accountCacheDir(platformKey, accountName string) (string, error) {
	base, err := loginCacheRoot(platformKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, safeFileName(accountName)), nil
}

func safeFileName(s string) string {
	out := paths.WindowsFileName(s, 200)
	if out == "" {
		return "_"
	}
	return out
}

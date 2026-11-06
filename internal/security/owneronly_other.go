//go:build !windows

package security

import "steamswitch/internal/fsutil"

func writeOwnerOnlyFile(path string, data []byte) error {
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

// TightenOwnerOnlyDir has nothing to do here: mode 0600 was already enforced when these were written.
func TightenOwnerOnlyDir(string) error { return nil }

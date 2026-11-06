package security

import (
	"os"
	"path/filepath"

	"steamswitch/internal/fsutil"
)

func writeFileAtomicDurable(path string, data []byte, perm os.FileMode) error {
	write := func() error { return fsutil.WriteFileAtomic(path, data, perm) }
	// A mode that shuts out group and others means owner-only, however the OS has to express that.
	if perm != 0 && perm.Perm()&0o077 == 0 {
		write = func() error { return writeOwnerOnlyFile(path, data) }
	}
	if err := write(); err != nil {
		return err
	}
	syncDirBestEffort(filepath.Dir(path))
	return nil
}

func syncDirBestEffort(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

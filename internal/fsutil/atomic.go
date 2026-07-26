package fsutil

import (
	"os"
	"path/filepath"

	"steamswitch/internal/actionlog"
)

// WriteFileAtomic writes data to path using a temp file in the same directory.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if perm != 0 {
		if err := os.Chmod(tmpPath, perm); err != nil {
			cleanup()
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		actionlog.Record("file:write", path, "", err)
		return err
	}
	actionlog.Record("file:write", path, "", nil)
	return nil
}

// WriteFileAtomicDurable is WriteFileAtomic plus a sync of the containing directory, so the
// rename that publishes the file is itself durable and not just atomic.
//
// Use it where a crash immediately afterwards must not be able to lose the write — the
// session-kit journal and its active pointer, whose entire purpose is to be on disk before
// the mutation they describe. Ordinary settings writes do not need it and should not pay
// for it: an fsync of the directory is a real seek on spinning media and a flush on SSDs.
func WriteFileAtomicDurable(path string, data []byte, perm os.FileMode) error {
	if err := WriteFileAtomic(path, data, perm); err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}

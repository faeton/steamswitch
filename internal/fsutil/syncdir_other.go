//go:build !windows

package fsutil

import "os"

// SyncDir flushes a directory's own metadata — the entries added, removed or renamed inside
// it — to stable storage.
//
// fsync on a file does not cover the directory entry that names it. Renaming a synced temp
// file into place is atomic with respect to *readers*, but until the parent directory is
// synced the rename itself can be lost in a power cut, so a crash can leave the old contents
// visible even though a later write appeared to succeed. For the session-kit journal that
// inverts the whole ordering guarantee: the rule is that the journal entry is durable before
// the mutation it describes, and an unsynced directory can reverse exactly that pair.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

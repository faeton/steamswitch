package security

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
)

// ShredFile overwrites a file's bytes once and then unlinks it, reporting only whether the
// unlink succeeded.
//
// **This does not guarantee erasure, and no caller may tell a user that it does.** On the
// machines this app runs on, the data very likely survives the overwrite:
//
//   - SSDs and NVMe drives do not rewrite in place. Wear-levelling and TRIM leave the original
//     block mapped out but intact until the controller decides otherwise.
//   - Volume Shadow Copies, File History, and OneDrive/Dropbox/Drive version history keep
//     prior revisions of anything in a synced or protected folder.
//   - The Windows Search index and Defender's scan history retain extracted content.
//   - Whatever produced the file — an agent transcript, a spreadsheet, a chat message — still
//     has its own copy, which is usually the copy that matters.
//
// It is still worth doing: it defeats trivial undelete on a spinning disk, and it costs one
// write. It exists so the legacy plaintext-import path is not a no-op, not so the UI can print
// a tick. The honest line, and the one the import summary uses, is "we removed the file we
// could see — check for other copies yourself".
func ShredFile(path string) error {
	// Lstat, not Stat: following a symlink here would overwrite whatever it points at, which
	// for a path the user picked in a file dialog is a way to destroy an unrelated file.
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("refusing to shred a non-regular file")
	}

	if size := info.Size(); size > 0 {
		if f, openErr := os.OpenFile(path, os.O_WRONLY, 0); openErr == nil {
			_, _ = io.CopyN(f, rand.Reader, size)
			_ = f.Sync()
			_ = f.Close()
		}
		// An overwrite that fails is not a reason to keep the file: unlinking is the part
		// that actually removes it from the directory, and it is the part callers report.
	}
	return os.Remove(path)
}

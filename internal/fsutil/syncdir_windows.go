//go:build windows

package fsutil

// SyncDir is a no-op on Windows.
//
// There is no Win32 equivalent of fsync-on-a-directory: opening a directory handle requires
// FILE_FLAG_BACKUP_SEMANTICS, and FlushFileBuffers on such a handle is not documented to
// flush the directory's metadata. NTFS instead journals metadata operations itself, so a
// rename that has returned is recorded in $LogFile and is replayed on mount after a crash.
//
// That is weaker than an explicit fsync — the guarantee is metadata ordering, not a
// commit point the caller can wait on — but it is what the platform offers. Callers must
// not read a nil return here as "the directory entry is on the platter".
func SyncDir(string) error { return nil }

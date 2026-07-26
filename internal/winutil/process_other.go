//go:build !windows

package winutil

import "time"

func KillByName(names []string, method ClosingMethod, _ func() error) error {
	return ErrUnsupported
}

// WaitForegroundForExe is a Windows-only helper; stub always returns false.
func WaitForegroundForExe(_ string, _ time.Duration) bool {
	return false
}

func Start(exe string, args []string, opts StartOpts) error {
	return ErrUnsupported
}

func IsProcessElevated() bool {
	return false
}

func StartAsDesktopUser(exe string, args []string, opts StartOpts) error {
	return ErrUnsupported
}

// SnapshotRunningExeBasenames is Windows-only.
//
// This reports failure rather than an empty set. An empty set is indistinguishable from
// "nothing is running", and callers cache it as an authoritative answer.
func SnapshotRunningExeBasenames() (map[string]struct{}, error) {
	return nil, ErrUnsupported
}

// IsExeRunning is Windows-only and always reports false here.
//
// The constant false is why [ProcessInspectionSupported] exists: every caller of this
// function is deciding whether it is safe to write to a file Steam might have open, and a
// false answer reads as "safe" at exactly the moment it is least true. Do not call this on a
// path that mutates Steam state without gating on ProcessInspectionSupported first.
func IsExeRunning(_ string) bool {
	return false
}

// ProcessInspectionSupported reports whether this build can tell if a process is running.
func ProcessInspectionSupported() bool { return false }

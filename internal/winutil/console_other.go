//go:build !windows

package winutil

// AttachParentConsole is a no-op outside Windows.
//
// True because a process here always has its stdio, so nothing needs a dialog instead.
func AttachParentConsole() bool { return true }

// ShowErrorBox is a no-op outside Windows.
func ShowErrorBox(string, string) {}

// AllocConsole is a no-op outside Windows.
func AllocConsole() error { return nil }

// FreeConsole is a no-op outside Windows.
func FreeConsole() error { return nil }

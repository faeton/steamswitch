//go:build windows

package winutil

import (
	"syscall"
	"unsafe"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
	procFreeConsole   = kernel32.NewProc("FreeConsole")
)

// AttachParentConsole attaches to the parent process console (CLI from cmd).
//
// It reports whether there is a console. False means the process was started by a double-click,
// a link or a shortcut, and anything written to stderr goes nowhere.
func AttachParentConsole() bool {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	return r != 0
}

// ShowErrorBox shows a blocking warning dialog, for a process with no window or console of its own.
func ShowErrorBox(title, text string) {
	const mbOK, mbIconWarning, mbSetForeground = 0x0, 0x30, 0x10000
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	m, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(
		0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbOK|mbIconWarning|mbSetForeground)
}

// AllocConsole allocates a new console if none exists (optional for headless).
func AllocConsole() error {
	r, _, err := procAllocConsole.Call()
	if r == 0 {
		return err
	}
	return nil
}

// FreeConsole frees the console for this process.
func FreeConsole() error {
	r, _, err := procFreeConsole.Call()
	if r == 0 {
		return err
	}
	return nil
}

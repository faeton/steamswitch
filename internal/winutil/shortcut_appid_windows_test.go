//go:build windows

package winutil

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A lazy proc only looks itself up when called, and a missing export panics there rather than
// returning an error, so a wrong DLL or name stays invisible until someone clicks the feature.
func TestShortcutProcsAreRealExports(t *testing.T) {
	for _, p := range []*windows.LazyProc{procCoTaskMemAlloc, procPropVariantClear, procSHGetPropertyStoreFromParsingName} {
		if err := p.Find(); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
	}
}

func TestNewLPWSTRPropVariant_HasTheWindowsLayout(t *testing.T) {
	pv, err := newLPWSTRPropVariant("Steam.Ключ 1")
	if err != nil {
		t.Fatal(err)
	}
	defer clearPropVariant(&pv)

	if vt := *(*uint16)(unsafe.Pointer(&pv.data[0])); vt != vtLPWSTR {
		t.Fatalf("vt = %d, want %d", vt, vtLPWSTR)
	}
	p := *(**uint16)(unsafe.Pointer(&pv.data[8]))
	if got := windows.UTF16PtrToString(p); got != "Steam.Ключ 1" {
		t.Fatalf("string = %q", got)
	}
}

// What the panic prevented: a real .lnk, in a path with spaces and non-ASCII letters, written
// twice over and read back through the property store.
func TestWriteShortcutLnk_SetsTheAppUserModelID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Папка с пробелами")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(dir, "Тест.lnk")
	for _, want := range []string{"SteamSwitch.Test.One", "SteamSwitch.Test.Two"} {
		if err := WriteShortcutLnk(lnk, self, "+s:1", dir, "test", "", want); err != nil {
			t.Fatalf("WriteShortcutLnk: %v", err)
		}
		got, err := readShortcutAppUserModelID(lnk)
		if err != nil {
			// Wine has no property store, so only real Windows can answer this.
			if strings.Contains(err.Error(), "Not implemented") {
				t.Skipf("property store unavailable here: %v", err)
			}
			t.Fatalf("read back: %v", err)
		}
		if got != want {
			t.Fatalf("AppUserModelID = %q, want %q", got, want)
		}
	}
}

func readShortcutAppUserModelID(lnk string) (string, error) {
	var out string
	err := withShortcutPropertyStore(lnk, gpsDefault, func(ps *iPropertyStore) error {
		var pv propVariant
		hr, _, _ := syscall.SyscallN(ps.vtbl.getValue, uintptr(unsafe.Pointer(ps)),
			uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&pv)))
		if err := hresultErr(hr); err != nil {
			return err
		}
		defer clearPropVariant(&pv)
		if *(*uint16)(unsafe.Pointer(&pv.data[0])) == vtLPWSTR {
			out = windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&pv.data[8])))
		}
		return nil
	})
	return out, err
}

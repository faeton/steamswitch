//go:build windows

package winutil

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

const (
	gpsDefault   = 0x0
	gpsReadWrite = 0x2
	// vtLPWSTR is VT_LPWSTR: the PROPVARIANT owns a CoTaskMemAlloc'd UTF-16 string.
	vtLPWSTR = 31
)

var (
	modOle32                              = windows.NewLazySystemDLL("ole32.dll")
	modShell32                            = windows.NewLazySystemDLL("shell32.dll")
	procCoTaskMemAlloc                    = modOle32.NewProc("CoTaskMemAlloc")
	procPropVariantClear                  = modOle32.NewProc("PropVariantClear")
	procSHGetPropertyStoreFromParsingName = modShell32.NewProc("SHGetPropertyStoreFromParsingName")
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

var pkeyAppUserModelID = propertyKey{
	fmtid: windows.GUID{
		Data1: 0x9f4c2855, Data2: 0x9f79, Data3: 0x4f39,
		Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3},
	},
	pid: 5,
}

type propVariant struct {
	data [propVariantSize]byte
}

type iPropertyStoreVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	getCount       uintptr
	getAt          uintptr
	getValue       uintptr
	setValue       uintptr
	commit         uintptr
}

type iPropertyStore struct {
	vtbl *iPropertyStoreVtbl
}

func (ps *iPropertyStore) release() {
	syscall.SyscallN(ps.vtbl.release, uintptr(unsafe.Pointer(ps)))
}

func (ps *iPropertyStore) setValue(key *propertyKey, val *propVariant) error {
	hr, _, _ := syscall.SyscallN(
		ps.vtbl.setValue,
		uintptr(unsafe.Pointer(ps)),
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(val)),
	)
	return hresultErr(hr)
}

func (ps *iPropertyStore) commit() error {
	hr, _, _ := syscall.SyscallN(ps.vtbl.commit, uintptr(unsafe.Pointer(ps)))
	return hresultErr(hr)
}

func hresultErr(hr uintptr) error {
	if hr == 0 {
		return nil
	}
	return ole.NewError(hr)
}

// newLPWSTRPropVariant builds what InitPropVariantFromString would. That function is an inline
// helper in propvarutil.h and no DLL exports it, so looking it up panics. The string lives in COM
// memory because PropVariantClear frees it from there.
//
// PROPVARIANT is a 16-bit vt and three reserved words, with the union at offset 8 on both the
// 32-bit and the 64-bit layout.
func newLPWSTRPropVariant(s string) (propVariant, error) {
	var pv propVariant
	units, err := windows.UTF16FromString(s)
	if err != nil {
		return pv, err
	}
	size := uintptr(len(units)) * unsafe.Sizeof(units[0])
	mem, _, _ := procCoTaskMemAlloc.Call(size)
	if mem == 0 {
		return pv, fmt.Errorf("CoTaskMemAlloc: out of memory")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(mem)), len(units)), units)
	*(*uint16)(unsafe.Pointer(&pv.data[0])) = vtLPWSTR
	*(*uintptr)(unsafe.Pointer(&pv.data[8])) = mem
	return pv, nil
}

func clearPropVariant(pv *propVariant) {
	if pv == nil {
		return
	}
	procPropVariantClear.Call(uintptr(unsafe.Pointer(pv)))
}

func setShortcutAppUserModelID(lnkPath, appID string) error {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil
	}
	if len(appID) > 128 {
		appID = appID[:128]
	}
	return withShortcutPropertyStore(lnkPath, gpsReadWrite, func(ps *iPropertyStore) error {
		pv, err := newLPWSTRPropVariant(appID)
		if err != nil {
			return err
		}
		defer clearPropVariant(&pv)

		if err := ps.setValue(&pkeyAppUserModelID, &pv); err != nil {
			return fmt.Errorf("SetValue AppUserModelID: %w", err)
		}
		if err := ps.commit(); err != nil {
			return fmt.Errorf("Commit: %w", err)
		}
		return nil
	})
}

// withShortcutPropertyStore opens a shortcut's property store on a COM-initialised thread.
func withShortcutPropertyStore(lnkPath string, flags uintptr, use func(*iPropertyStore) error) error {
	// LazyProc.Call panics on a missing export, and through a Wails binding that reaches the user
	// as a stack trace. Metadata is not worth that.
	for _, p := range []*windows.LazyProc{procCoTaskMemAlloc, procPropVariantClear, procSHGetPropertyStoreFromParsingName} {
		if err := p.Find(); err != nil {
			return err
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const rpcEChangedMode = uintptr(0x80010106)
	var needUninit bool
	if err := ole.CoInitialize(0); err != nil {
		oe, ok := err.(*ole.OleError)
		if !ok {
			return fmt.Errorf("com init: %w", err)
		}
		switch oe.Code() {
		case 1:
			needUninit = true
		case rpcEChangedMode:
			needUninit = false
		default:
			return fmt.Errorf("com init: %w", err)
		}
	} else {
		needUninit = true
	}
	if needUninit {
		defer ole.CoUninitialize()
	}

	pathPtr, err := windows.UTF16PtrFromString(lnkPath)
	if err != nil {
		return err
	}

	iid := ole.NewGUID("{886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}")
	var punk uintptr
	hr, _, _ := procSHGetPropertyStoreFromParsingName.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		flags,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&punk)),
	)
	if e := hresultErr(hr); e != nil {
		return fmt.Errorf("SHGetPropertyStoreFromParsingName: %w", e)
	}
	if punk == 0 {
		return fmt.Errorf("SHGetPropertyStoreFromParsingName: nil store")
	}
	ps := (*iPropertyStore)(unsafe.Pointer(punk))
	defer ps.release()
	return use(ps)
}
